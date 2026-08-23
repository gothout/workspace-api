// Ligação da evolução Redis (issue #8) com o processo: TODOS os adaptadores
// que transformam o cliente degradável em contratos dos pacotes consumidores
// moram aqui, resolvendo o singleton NA CHAMADA (regra do AGENTS.md do
// bootstrap).
//
//   - revogadorComCache compõe denylist Redis + fonte Postgres para o
//     contrato RevogadorDeRefresh declarado em internal/infra/jwt;
//   - cacheResolucaoRedis implementa o CacheResolucao do subdomínio
//     workspace (estrutura EntradaResolucao é dele — por isso o adaptador é
//     daqui: infra não importa domain);
//   - resolvedorPermissoesComCache decora o resolvedor do user com o cache
//     perm:{org}:{user}:{wks}, invalidado pelo observador ligado ao
//     service do user;
//   - limitadorLoginAuth expõe o lockout por (e-mail, IP) à aplicação auth.
//
// Sem Redis (desabilitado/inacessível) todos viram no-op honesto: consulta
// direta à fonte, sem lockout — nada quebra, só fica mais caro/aberto.
package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	aplicacaoauth "workspace-api/internal/identidade/application/auth"
	dominioUsuario "workspace-api/internal/identidade/domain/user"
	dominioWorkspace "workspace-api/internal/identidade/domain/workspace"
	rediscache "workspace-api/internal/infra/redis"
	"workspace-api/internal/infra/jwt"
	"workspace-api/internal/middleware"
	"workspace-api/internal/pkg/config"
	"workspace-api/internal/pkg/orgctx"
)

// chaveWorkspaceNulo representa uuid.Nil dentro das chaves de cache (sem
// workspace ativo — console master/acesso direto).
const chaveWorkspaceNulo = "nil"

// --- Contrato RevogadorDeRefresh (denylist Redis + fonte Postgres) ---------

// revogadorComCache fecha o ciclo da denylist (issue #8): hit positivo no
// Redis responde na hora; miss cai à tabela persistida e, se lá constar
// revogação, grava o positivo (best-effort). NEGATIVO nunca é cacheado —
// logout e rotação valem NA HORA sem invalidação, e um Redis vazio nunca
// libera token. Falha de Redis = segue à fonte (degradar, não bloquear).
//
// A FONTE é injetável pelo construtor (nil = revogadorRefresh real, que
// resolve o pool NA CHAMADA) — mesmo padrão dos outros adaptadores: os
// testes de composição usam funções puras sobre o banco efêmero.
type revogadorComCache struct {
	fonte jwt.RevogadorDeRefresh
}

// novoRevogadorComCache monta o revogador composto do processo.
func novoRevogadorComCache(fonte jwt.RevogadorDeRefresh) revogadorComCache {
	if fonte == nil {
		fonte = revogadorRefresh{}
	}
	return revogadorComCache{fonte: fonte}
}

func (c revogadorComCache) Revogado(jti string) (bool, error) {
	cliente, err := rediscache.Get()
	if err != nil || cliente == nil {
		return c.fonte.Revogado(jti) // sem Redis: comportamento pré-evolução
	}
	denylist := rediscache.NovaDenylist(cliente, ttlDenylist())
	revogado, err := denylist.Revogado(jti)
	if err != nil {
		slog.Warn("[DEGRADADO] denylist redis falhou — conferindo a fonte persistida", "erro", err.Error())
		return c.fonte.Revogado(jti)
	}
	if revogado {
		return true, nil
	}
	revogadoFonte, err := c.fonte.Revogado(jti)
	if err != nil {
		return false, err
	}
	if revogadoFonte {
		// Cache best-effort do positivo: revogação é permanente, então pode
		// viver até o fim do TTL do refresh.
		if err := denylist.MarcarRevogado(jti); err != nil {
			slog.Warn("[DEGRADADO] denylist redis não gravou revogação — fonte permanece a verdade", "erro", err.Error())
		}
	}
	return revogadoFonte, nil
}

// ttlDenylist acompanha a vida útil do refresh: depois dela o jti não é mais
// apresentável, então não há que lembrar por mais tempo.
func ttlDenylist() time.Duration {
	m, err := jwt.Get()
	if err != nil || m == nil {
		return time.Hour
	}
	return m.TTLRefresh()
}

// --- Contrato CacheResolucao (subdomínio workspace) -------------------------

// cacheResolucaoRedis guarda workspace:slug:{slug} com JSON mínimo. TTL vem
// de cache.ttl_resolucao_seg (curto por desenho); a invalidação ATIVA nas
// escritas é quem garante "filho nunca mais vivo que o pai" sem depender de
// TTL expirar sozinho.
type cacheResolucaoRedis struct{}

type entradaResolucaoJSON struct {
	WorkspaceUUID    string `json:"w"`
	OrganizationUUID string `json:"o"`
	Status           string `json:"s"`
}

func (cacheResolucaoRedis) Buscar(ctx context.Context, slug string) (*dominioWorkspace.EntradaResolucao, bool) {
	cliente, err := rediscache.Get()
	if err != nil || cliente == nil {
		return nil, false
	}
	bruto, err := cliente.Get(ctx, rediscache.ChaveResolucaoSlug(slug)).Bytes()
	if err != nil {
		if !errors.Is(err, context.DeadlineExceeded) && err.Error() != "redis: nil" {
			slog.Warn("[DEGRADADO] leitura do cache de resolução falhou", "erro", err.Error())
		}
		return nil, false // ausente E falha são o mesmo caminho: consulta real
	}
	var entrada entradaResolucaoJSON
	if err := json.Unmarshal(bruto, &entrada); err != nil {
		return nil, false // lixo corrompido: trata como ausente, nunca propaga
	}
	return &dominioWorkspace.EntradaResolucao{
		WorkspaceUUID:    entrada.WorkspaceUUID,
		OrganizationUUID: entrada.OrganizationUUID,
		Status:           entrada.Status,
	}, true
}

func (cacheResolucaoRedis) Guardar(ctx context.Context, slug string, entrada dominioWorkspace.EntradaResolucao) {
	cliente, err := rediscache.Get()
	if err != nil || cliente == nil {
		return
	}
	corpo, err := json.Marshal(entradaResolucaoJSON{
		WorkspaceUUID:    entrada.WorkspaceUUID,
		OrganizationUUID: entrada.OrganizationUUID,
		Status:           entrada.Status,
	})
	if err != nil {
		return
	}
	ttl := time.Duration(config.MustUse().Cache.TtlResolucaoSeg) * time.Second
	if err := cliente.Set(ctx, rediscache.ChaveResolucaoSlug(slug), corpo, ttl).Err(); err != nil {
		slog.Warn("[DEGRADADO] escrita no cache de resolução falhou", "erro", err.Error())
	}
}

func (cacheResolucaoRedis) Invalidar(ctx context.Context, slug string) {
	cliente, err := rediscache.Get()
	if err != nil || cliente == nil {
		return
	}
	if err := cliente.Del(ctx, rediscache.ChaveResolucaoSlug(slug)).Err(); err != nil {
		slog.Warn("[DEGRADADO] invalidação do cache de resolução falhou", "erro", err.Error())
	}
}

// InvalidarOrganization é a invalidação GROSSEIRA da cascata: varre o
// namespace de slugs e descarta as entradas derivadas da organization. O
// espaço de chaves é proporcional aos workspaces — varredura pontual em
// evento raro (inativação), não no caminho quente.
func (cacheResolucaoRedis) InvalidarOrganization(ctx context.Context, organizationUUID string) {
	cliente, err := rediscache.Get()
	if err != nil || cliente == nil {
		return
	}
	var cursor uint64
	for {
		pagina, proximo, err := cliente.Scan(ctx, cursor, rediscache.PrefixoWorkspaceSlug+"*", 100).Result()
		if err != nil {
			slog.Warn("[DEGRADADO] varredura do cache de resolução falhou", "erro", err.Error())
			return
		}
		for _, chave := range pagina {
			bruto, err := cliente.Get(ctx, chave).Bytes()
			if err != nil {
				continue
			}
			var entrada entradaResolucaoJSON
			if err := json.Unmarshal(bruto, &entrada); err != nil || entrada.OrganizationUUID != organizationUUID {
				continue
			}
			_ = cliente.Del(ctx, chave).Err()
		}
		cursor = proximo
		if cursor == 0 {
			return
		}
	}
}

// --- Contrato ResolvedorPermissoes (decorado com cache perm:) ---------------

// resolvedorPermissoesComCache decora o delegado ao subdomínio user: a
// consulta de PermissoesEfetivas roda em toda requisição autorizada, então
// é candidata natural a cache. TemVinculo NÃO é cacheado — caminho de
// concessão de acesso com auditoria própria ([SUPORTE]) mora lá.
//
// A BASE é injetável pelo construtor (nil = resolvedorPermissoesUser real,
// que resolve o singleton do user NA CHAMADA) — os testes de composição
// usam dublês puros.
type resolvedorPermissoesComCache struct {
	base middleware.ResolvedorPermissoes
}

func novoResolvedorPermissoesComCache(base middleware.ResolvedorPermissoes) resolvedorPermissoesComCache {
	if base == nil {
		base = resolvedorPermissoesUser{}
	}
	return resolvedorPermissoesComCache{base: base}
}

func (c resolvedorPermissoesComCache) TemVinculo(ctx context.Context, usuarioUUID, organizationUUID, workspaceUUID uuid.UUID) (bool, error) {
	return c.base.TemVinculo(ctx, usuarioUUID, organizationUUID, workspaceUUID)
}

func (c resolvedorPermissoesComCache) PermissoesEfetivas(ctx context.Context, usuarioUUID, organizationUUID, workspaceUUID uuid.UUID) ([]string, error) {
	cliente, err := rediscache.Get()
	if err != nil || cliente == nil {
		return c.base.PermissoesEfetivas(ctx, usuarioUUID, organizationUUID, workspaceUUID)
	}
	chave := rediscache.ChavePermissoes(organizationUUID.String(), usuarioUUID.String(), rotuloWorkspace(workspaceUUID))
	if bruto, err := cliente.Get(ctx, chave).Bytes(); err == nil {
		var permissoes []string
		if json.Unmarshal(bruto, &permissoes) == nil && permissoes != nil {
			return permissoes, nil
		} // lixo corrompido: recarrega da fonte
	}
	permissoes, err := c.base.PermissoesEfetivas(ctx, usuarioUUID, organizationUUID, workspaceUUID)
	if err != nil {
		return nil, err
	}
	if corpo, err := json.Marshal(permissoes); err == nil {
		ttl := time.Duration(config.MustUse().Cache.TtlPermissoesSeg) * time.Second
		if err := cliente.Set(ctx, chave, corpo, ttl).Err(); err != nil {
			slog.Warn("[DEGRADADO] escrita no cache de permissões falhou", "erro", err.Error())
		}
	}
	return permissoes, nil
}

func rotuloWorkspace(w uuid.UUID) string {
	if w == uuid.Nil {
		return chaveWorkspaceNulo
	}
	return w.String()
}

// invalidadorPermissoesRedis é o lado Redis do contrato ObservadorAtribuicoes
// do user: escrita de atribuição derruba as entradas afetadas. Roda DEPOIS
// da persistência — falha aqui nunca desfaz escrita (o TTL é o cinto de
// segurança).
type invalidadorPermissoesRedis struct{}

func (invalidadorPermissoesRedis) AtribuicaoAlterada(ctx context.Context, usuarioUUID, workspaceUUID uuid.UUID) {
	cliente, err := rediscache.Get()
	if err != nil || cliente == nil {
		return
	}
	organizationUUID := orgctx.OrganizationUUID(ctx)
	if workspaceUUID == uuid.Nil {
		// Remoção não carrega o par: invalidação grosseira de todas as
		// entradas do usuário na organization (evento raro, espaço pequeno).
		var cursor uint64
		padrao := rediscache.ChavePermissoesUsuario(organizationUUID.String(), usuarioUUID.String())
		for {
			pagina, proximo, err := cliente.Scan(ctx, cursor, padrao, 100).Result()
			if err != nil {
				slog.Warn("[DEGRADADO] invalidação do cache de permissões falhou", "erro", err.Error())
				return
			}
			for _, chave := range pagina {
				_ = cliente.Del(ctx, chave).Err()
			}
			cursor = proximo
			if cursor == 0 {
				return
			}
		}
	}
	if err := cliente.Del(ctx, rediscache.ChavePermissoes(
		organizationUUID.String(), usuarioUUID.String(), rotuloWorkspace(workspaceUUID))).Err(); err != nil {
		slog.Warn("[DEGRADADO] invalidação do cache de permissões falhou", "erro", err.Error())
	}
}

// --- Contrato LimitadorLogin (aplicação auth) --------------------------------

// limitadorLoginAuth monta o lockout por (e-mail, IP) NA CHAMADA: parâmetros
// vêm da config corrente e cliente ausente devolve limitador no-op (sem
// Redis = sem lockout, com log uma única vez no boot).
type limitadorLoginAuth struct{}

func (limitadorLoginAuth) limitador() *rediscache.LimitadorLogin {
	cliente, err := rediscache.Get()
	if err != nil || cliente == nil {
		return nil
	}
	cfg := config.MustUse().Cache.LoginLockout
	return rediscache.NovoLimitadorLogin(cliente, cfg.MaxTentativas,
		time.Duration(cfg.JanelaSeg)*time.Second,
		time.Duration(cfg.BloqueioSeg)*time.Second)
}

func (l limitadorLoginAuth) Autorizado(ctx context.Context, email, ip string) (bool, time.Duration, error) {
	limitador := l.limitador()
	if limitador == nil {
		return false, 0, nil
	}
	return limitador.Bloqueado(ctx, email, ip)
}

func (l limitadorLoginAuth) RegistrarFalha(ctx context.Context, email, ip string) error {
	limitador := l.limitador()
	if limitador == nil {
		return nil
	}
	return limitador.RegistrarFalha(ctx, email, ip)
}

func (l limitadorLoginAuth) RegistrarSucesso(ctx context.Context, email, ip string) error {
	limitador := l.limitador()
	if limitador == nil {
		return nil
	}
	return limitador.RegistrarSucesso(ctx, email, ip)
}

var _ aplicacaoauth.LimitadorLogin = limitadorLoginAuth{}
var _ dominioUsuario.ObservadorAtribuicoes = invalidadorPermissoesRedis{}
var _ dominioWorkspace.CacheResolucao = cacheResolucaoRedis{}
var _ middleware.ResolvedorPermissoes = resolvedorPermissoesComCache{}
