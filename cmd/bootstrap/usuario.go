// Ligação do subdomínio user com a cadeia de middleware e com o infra/jwt:
// adaptadores que resolvem os singletons NA CHAMADA (regra do AGENTS.md do
// bootstrap) e traduzem o vocabulário de erro entre os contratos.
//
// Desde a F4 o ResolvedorPermissoes NÃO é mais o provisório da F1 (que
// consultava o SQL das tabelas direto): delega ao service do user — regra de
// vínculo, suporte auditado e união de permissões moram no subdomínio.
package bootstrap

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"

	aplicacaoauth "workspace-api/internal/identidade/application/auth"
	dominioOrganizacao "workspace-api/internal/identidade/domain/organization"
	dominioUsuario "workspace-api/internal/identidade/domain/user"
	dominioWorkspace "workspace-api/internal/identidade/domain/workspace"
	orgmodel "workspace-api/internal/identidade/model/organization"
	modeluser "workspace-api/internal/identidade/model/user"
	"workspace-api/internal/infra/database/postgres"
	"workspace-api/internal/infra/jwt"
	"workspace-api/internal/pkg/config"
	"workspace-api/internal/pkg/orgctx"
)

// --- Contrato ResolvedorPermissoes (delega ao service do user) -----------------

// resolvedorPermissoesUser injeta organization+workspace no ctx e pergunta ao
// subdomínio user — vínculo direto, suporte auditado ([SUPORTE]) e curingas
// são regra DE LÁ desde a F4.
type resolvedorPermissoesUser struct{}

func (resolvedorPermissoesUser) TemVinculo(ctx context.Context, usuarioUUID, organizationUUID, workspaceUUID uuid.UUID) (bool, error) {
	return dominioUsuario.MustUse().Service.TemVinculo(
		ctxEscopado(ctx, organizationUUID, workspaceUUID), usuarioUUID, workspaceUUID)
}

func (resolvedorPermissoesUser) PermissoesEfetivas(ctx context.Context, usuarioUUID, organizationUUID, workspaceUUID uuid.UUID) ([]string, error) {
	return dominioUsuario.MustUse().Service.PermissoesEfetivas(
		ctxEscopado(ctx, organizationUUID, workspaceUUID), usuarioUUID, workspaceUUID)
}

// ctxEscopado monta o contexto que os contratos do middleware não trazem: o
// middleware passa os identificadores como parâmetros; os repositories do
// negócio leem escopo do ctx (fail-closed).
func ctxEscopado(ctx context.Context, organizationUUID, workspaceUUID uuid.UUID) context.Context {
	if workspaceUUID == uuid.Nil {
		return orgctx.WithOrganization(ctx, organizationUUID)
	}
	return orgctx.WithWorkspace(orgctx.WithOrganization(ctx, organizationUUID), workspaceUUID)
}

// --- Contrato RevogadorDeRefresh (infra/jwt → tabela do subdomínio) ------------

// revogadorRefresh fecha o ciclo da revogação persistida: o validador do JWT
// confere o jti contra identidade_user_refresh_token na hora de aceitar um
// refresh. Consulta GLOBAL pelo jti (único TOTAL) — exceção documentada no
// AGENTS.md do subdomínio: acontece antes de existir escopo.
type revogadorRefresh struct{}

func (revogadorRefresh) Revogado(jti string) (bool, error) {
	db, err := postgres.GetDB()
	if err != nil {
		return false, err
	}
	repo := dominioUsuario.NewRepository(db)
	return repo.RefreshTokenRevogado(jti)
}

var _ jwt.RevogadorDeRefresh = revogadorRefresh{}

// --- Contrato ValidadorWorkspaces (subdomínio user → irmão workspace) ----------

// validadorWorkspaces responde se o workspace alvo de uma atribuição existe,
// está ativo e pertence à organization do contexto — resolve o singleton do
// workspace NA CHAMADA e traduz "não encontrado" para recusa (não vaza
// existência entre tenants).
type validadorWorkspaces struct{}

func (validadorWorkspaces) Pertence(ctx context.Context, organizationUUID, workspaceUUID uuid.UUID) (bool, error) {
	ws, err := dominioWorkspace.MustUse().Service.ResolverPorUUID(ctx, workspaceUUID)
	if err != nil {
		if err == dominioWorkspace.ErrNotFound {
			return false, nil // inexistente/inativo/alheio: mesma recusa
		}
		return false, err
	}
	return ws.Ativo() && ws.OrganizationUUID == organizationUUID, nil
}

var _ dominioUsuario.ValidadorWorkspaces = validadorWorkspaces{}

// --- Contratos da aplicação auth -------------------------------------------------

// usuariosAuth expõe ao login só a face do subdomínio user de que ele
// precisa: o ctx chega JÁ ESCOPADO na organization resolvida pelo Host.
type usuariosAuth struct{}

func (usuariosAuth) Autenticar(ctx context.Context, email, senha string) (*modeluser.User, error) {
	u, err := dominioUsuario.MustUse().Service.Autenticar(ctx, email, senha)
	if err != nil {
		// Tradução ACL: qualquer recusa do subdomínio vira o sentinela do
		// contrato — corpo indistinguível garantido na aplicação.
		return nil, aplicacaoauth.ErrCredenciaisInvalidas
	}
	return u, nil
}

func (usuariosAuth) PorUUID(ctx context.Context, id uuid.UUID) (*modeluser.User, error) {
	u, err := dominioUsuario.MustUse().Service.Read(ctx, id)
	if err != nil {
		return nil, aplicacaoauth.ErrSessaoInvalida
	}
	return u, nil
}

func (usuariosAuth) RegistrarRefreshToken(ctx context.Context, usuarioUUID uuid.UUID, jti string, expiraEm time.Time) error {
	return dominioUsuario.MustUse().Service.RegistrarSessao(ctx, usuarioUUID, jti, expiraEm)
}

func (usuariosAuth) RefreshTokenAtivo(ctx context.Context, usuarioUUID uuid.UUID, jti string) (bool, error) {
	return dominioUsuario.MustUse().Service.SessaoAtiva(ctx, usuarioUUID, jti)
}

func (usuariosAuth) EncerrarSessao(ctx context.Context, usuarioUUID uuid.UUID, jti string) error {
	return dominioUsuario.MustUse().Service.EncerrarSessao(ctx, usuarioUUID, jti)
}

// --- Contrato VitalidadeOrganization (R4: sessão não sobrevive à dona) ---------

// vitalidadeOrganizacao fecha o fail-closed do refresh/logout: pergunta ao
// subdomínio organization se a dona do contrato segue viva — resolve o
// singleton NA CHAMADA. Removida/inativa = false sem erro (não vaza
// existência); falha de infra sobe para a aplicação decidir.
type vitalidadeOrganizacao struct{}

func (vitalidadeOrganizacao) Ativa(ctx context.Context, organizationUUID uuid.UUID) (bool, error) {
	ctxEscopo := orgctx.WithOrganization(ctx, organizationUUID)
	o, err := dominioOrganizacao.MustUse().Service.Read(ctxEscopo, organizationUUID)
	if err != nil {
		if errors.Is(err, dominioOrganizacao.ErrNotFound) {
			return false, nil // removida = morta — mesma recusa de inativa
		}
		return false, err
	}
	return o.Status == orgmodel.StatusAtivo, nil
}

var _ aplicacaoauth.VitalidadeOrganization = vitalidadeOrganizacao{}

// --- Contrato EncerradorSessoesUsuarios (cascata organization → user) ----------

// encerradorSessoesUsuario executa o lado user da cascata da organization
// (R4): resolve o singleton do user NA CHAMADA. A organization alvo é A DO
// CTX (o service da organization já conferiu o pertencimento) e a query é
// fail-closed por ele. Idempotente.
type encerradorSessoesUsuario struct{}

func (encerradorSessoesUsuario) RevogarTokensDaOrganization(ctx context.Context) (int64, error) {
	return dominioUsuario.MustUse().Service.RevogarSessoesDaOrganization(ctx)
}

var _ dominioOrganizacao.EncerradorSessoesUsuarios = encerradorSessoesUsuario{}

// emissorToken resolve o singleton do JWT NA CHAMADA — o adaptador nunca
// guarda o manager congelado.
type emissorToken struct{}

func (emissorToken) EmitirPar(in jwt.EntradaToken) (string, string, string, time.Time, error) {
	m, err := jwt.Get()
	if err != nil {
		return "", "", "", time.Time{}, err
	}
	acesso, err := m.EmitirAcesso(in)
	if err != nil {
		return "", "", "", time.Time{}, err
	}
	refresh, jti, expira, err := m.EmitirRefresh(in)
	if err != nil {
		return "", "", "", time.Time{}, err
	}
	return acesso, refresh, jti, expira, nil
}

func (emissorToken) Validar(tokenTexto string) (*jwt.Claims, error) {
	m, err := jwt.Get()
	if err != nil {
		return nil, err
	}
	return m.Validar(tokenTexto)
}

// ValidarSemRevogacao expõe a porta do logout idempotente (R5): sem denylist,
// o token já revogado tem as claims lidas e o EncerrarSessao confirma a
// revogação em vez de recusar.
func (emissorToken) ValidarSemRevogacao(tokenTexto string) (*jwt.Claims, error) {
	m, err := jwt.Get()
	if err != nil {
		return nil, err
	}
	return m.ValidarAssinatura(tokenTexto)
}

// resolvedorOrganizacao casa o Host com a MESMA fonte do ResolveWorkspace:
// rótulo de workspace no domínio-base da plataforma OU domínio custom
// white-label (o dono do domínio resolve mesmo sem rótulo — portal raiz do
// parceiro). Host fixo da plataforma (base nu, api., painel.) e host
// desconhecido NÃO resolvem organization: a aplicação devolve o 401 genérico.
type resolvedorOrganizacao struct{}

func (resolvedorOrganizacao) Resolver(ctx context.Context, hostBruto string) (uuid.UUID, bool, error) {
	host := hostSemPorta(hostBruto)
	baseDomain := normalizarDominio(config.MustUse().App.BaseDomain)

	if baseDomain != "" {
		if slug, casou := rotuloDeSufixo(host, baseDomain); casou && slug != "" &&
			!contemRotuloFixo(dominioWorkspace.MustUse().Service.SlugsFixos(), slug) {
			resolvido, err := dominioWorkspace.MustUse().Service.ResolverPorSlug(ctx, slug)
			if err != nil {
				if err == dominioWorkspace.ErrNotFound {
					return uuid.Nil, false, nil // slug não resolve organization nenhuma
				}
				return uuid.Nil, false, err
			}
			if resolvido.Ativo() {
				return resolvido.OrganizationUUID, true, nil
			}
			return uuid.Nil, false, nil // workspace inativo não abre sessão
		}
	}

	// White-label: {qualquer-coisa}.dominio-custom → organization dona.
	listados, err := provedorDominiosCustom{}.Listar(ctx)
	if err != nil {
		return uuid.Nil, false, err
	}
	for _, d := range listados {
		if _, casou := rotuloDeSufixo(host, normalizarDominio(d.Dominio)); casou {
			// O login resolve ORGANIZATION, não workspace: o rótulo é
			// irrelevante aqui — qualquer host sob o domínio do parceiro
			// endereça a organization dele.
			return d.OrganizationUUID, true, nil
		}
	}
	return uuid.Nil, false, nil
}

// --- Helpers de Host (espelhos puros dos do middleware — cmd não importa
// os internos de lá; mesma gramática: case-insensitive, sem porta) ----------

func hostSemPorta(bruto string) string {
	host := strings.ToLower(strings.TrimSpace(bruto))
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}

func normalizarDominio(d string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(d), "."))
}

// rotuloDeSufixo devolve o rótulo à esquerdo do sufixo casado; host IGUAL ao
// domínio casou mas não tem rótulo ("").
func rotuloDeSufixo(host, dominio string) (string, bool) {
	if host == "" || dominio == "" {
		return "", false
	}
	switch {
	case host == dominio:
		return "", true
	case strings.HasSuffix(host, "."+dominio):
		return strings.TrimSuffix(host, "."+dominio), true
	default:
		return "", false
	}
}

func contemRotuloFixo(fixos []string, slug string) bool {
	for _, f := range fixos {
		if strings.EqualFold(strings.TrimSpace(f), slug) {
			return true
		}
	}
	return false
}
