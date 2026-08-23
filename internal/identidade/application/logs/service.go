package logs

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"workspace-api/internal/infra/clickhouse"
	"workspace-api/internal/middleware"
	"workspace-api/internal/pkg/errobserve"
	"workspace-api/internal/pkg/log/access_log"
	"workspace-api/internal/pkg/log/audit_log"
	"workspace-api/internal/pkg/orgctx"
	"workspace-api/internal/pkg/pagination"
)

// Service é a regra da aplicação: leitura das trilhas COM o recorte de
// escopo em 3 níveis imposto a partir do ctx — nunca do query param.
type Service interface {
	Auditoria(ctx context.Context, filtro clickhouse.FiltroTrilha, p pagination.Pagination) (pagination.Response[AuditoriaItemDto], error)
	Acesso(ctx context.Context, filtro clickhouse.FiltroTrilha, p pagination.Pagination) (pagination.Response[AcessoItemDto], error)
	Erros(ctx context.Context, filtro clickhouse.FiltroTrilha, p pagination.Pagination) (pagination.Response[ErroItemDto], error)
}

type serviceImpl struct {
	trilhas  ConsultaTrilhas
	usuarios ResolvedorUsuarios // opcional (UX2): nil = linhas sem nome/e-mail
}

// OpcaoServico adiciona peças opcionais ao service sem mudar a assinatura
// para os chamadores existentes (mesmo padrão dos subdomínios).
type OpcaoServico func(*serviceImpl)

// ComResolvedorUsuarios liga o enriquecimento user_nome/user_email das
// linhas de log (issue #29).
func ComResolvedorUsuarios(u ResolvedorUsuarios) OpcaoServico {
	return func(s *serviceImpl) { s.usuarios = u }
}

// NewService devolve o service DECORADO (service_observado.go): todo erro
// que sobe ao chamador é observado — singleton e testes pelo mesmo caminho.
func NewService(trilhas ConsultaTrilhas, opcoes ...OpcaoServico) Service {
	s := &serviceImpl{trilhas: trilhas}
	for _, aplicar := range opcoes {
		aplicar(s)
	}
	return serviceObservado{Service: s, obs: observadorErros}
}

// recorte é o recorte de leitura derivado do ctx — o chamador NUNCA escolhe:
//
//   - PLATAFORMA: possui o curinga global *:* — lê qualquer organization,
//     filtros opcionais honrados como vieram;
//   - ORGANIZATION: atende identidade:logs:ler_organization — preso à
//     própria organization (todos os workspaces dela);
//   - WORKSPACE: demais chamadores com identidade:logs:ler — presos ao par
//     (organization, workspace) resolvido no ctx.
type recorte struct {
	plataforma  bool
	organizacao uuid.UUID // Nil na plataforma
	workspace   uuid.UUID // Nil quando o alcance é a organization inteira
}

// recorteDoCtx deriva o recorte. Fail-closed: chamador sem organization
// resolvida — ou sem o workspace exigido pelo recorte básico — não lê nada.
func recorteDoCtx(ctx context.Context) recorte {
	efetivas := orgctx.Permissoes(ctx)
	if contemGlobal(efetivas) {
		return recorte{plataforma: true}
	}
	org := orgctx.OrganizationUUID(ctx)
	if middleware.Atende(efetivas, PermLerOrganization) {
		return recorte{organizacao: org}
	}
	// Recorte básico EXIGE o par resolvido: org sem workspace no ctx não vira
	// alcance de organization inteira (defesa em profundidade — pela cadeia
	// HTTP os dois chegam sempre juntos).
	if org == uuid.Nil || orgctx.WorkspaceUUID(ctx) == uuid.Nil {
		return recorte{}
	}
	return recorte{organizacao: org, workspace: orgctx.WorkspaceUUID(ctx)}
}

// aplicar impõe o recorte sobre os filtros informados. Filtro apontando fora
// do recorte = ErrForaDoEscopo (404 — não confirma existência de dados
// alheios; uuid alheio que exista ou não recebe a MESMA resposta).
func (r recorte) aplicar(filtro clickhouse.FiltroTrilha) (clickhouse.FiltroTrilha, error) {
	if r.plataforma {
		return filtro, nil
	}
	if r.organizacao == uuid.Nil {
		return clickhouse.FiltroTrilha{}, ErrForaDoEscopo
	}
	if filtro.OrganizationUUID != "" {
		pedida, err := uuid.Parse(filtro.OrganizationUUID)
		if err != nil || pedida != r.organizacao {
			return clickhouse.FiltroTrilha{}, ErrForaDoEscopo
		}
	}
	filtro.OrganizationUUID = r.organizacao.String()
	if r.workspace != uuid.Nil {
		if filtro.WorkspaceUUID != "" {
			pedido, err := uuid.Parse(filtro.WorkspaceUUID)
			if err != nil || pedido != r.workspace {
				return clickhouse.FiltroTrilha{}, ErrForaDoEscopo
			}
		}
		filtro.WorkspaceUUID = r.workspace.String()
	}
	return filtro, nil
}

// contemGlobal confere a posse EXATA do curinga global — forma de 2 segmentos
// fora da gramática do Atende (mesma exceção da validação de posse de R1).
func contemGlobal(efetivas []string) bool {
	for _, p := range efetivas {
		if p == permGlobal {
			return true
		}
	}
	return false
}

const permGlobal = "*:*"

// coletarUserUUIDs devolve os uuids DISTINTOS e não vazios das linhas — a
// resolução é em lote, uma consulta por resposta (issue #29).
func coletarUserUUIDs[T any](itens []T, campo func(T) string) []string {
	vistos := map[string]bool{}
	uuids := make([]string, 0, len(itens))
	for _, item := range itens {
		u := campo(item)
		if u == "" || vistos[u] {
			continue
		}
		vistos[u] = true
		uuids = append(uuids, u)
	}
	return uuids
}

// usuariosDasLinhas resolve nome/e-mail EM LOTE para as linhas da página.
// Enriquecimento é leitura COMPLEMENTAR: falha do resolvedor nunca derruba a
// consulta — as linhas seguem com os campos vazios e o motivo sai no log
// (mesma filosofia degradável das dependências opcionais do template).
func (s *serviceImpl) usuariosDasLinhas(ctx context.Context, uuids []string) map[string]UsuarioLog {
	if s.usuarios == nil || len(uuids) == 0 {
		return map[string]UsuarioLog{}
	}
	mapa, err := s.usuarios.Resolver(ctx, uuids)
	if err != nil || mapa == nil {
		slog.WarnContext(ctx, "logs.enriquecimento_usuarios_degradado",
			"dominio", Dominio, "subdominio", Subdominio,
			"motivo", motivoDe(err), "linhas", len(uuids))
		return map[string]UsuarioLog{}
	}
	return mapa
}

func motivoDe(err error) string {
	if err == nil {
		return "resposta vazia"
	}
	return err.Error()
}

// Auditoria consulta a trilha de auditoria no recorte do ctx. Leitura pura —
// ler NÃO audita (doc 04).
func (s *serviceImpl) Auditoria(ctx context.Context, filtro clickhouse.FiltroTrilha, p pagination.Pagination) (pagination.Response[AuditoriaItemDto], error) {
	aplicado, err := recorteDoCtx(ctx).aplicar(filtro)
	if err != nil {
		return pagination.Response[AuditoriaItemDto]{}, err
	}
	aplicado.Offset, aplicado.Limite = p.Offset(), p.Limit()
	itens, total, err := s.trilhas.Auditoria(ctx, aplicado)
	if err != nil {
		return pagination.Response[AuditoriaItemDto]{}, err
	}
	usuarios := s.usuariosDasLinhas(ctx, coletarUserUUIDs(itens, func(ev audit_log.Evento) string { return ev.UserUUID }))
	return NovoAuditoriaResponseDto(itens, total, p, usuarios), nil
}

// Acesso consulta a trilha de acesso HTTP no recorte do ctx.
func (s *serviceImpl) Acesso(ctx context.Context, filtro clickhouse.FiltroTrilha, p pagination.Pagination) (pagination.Response[AcessoItemDto], error) {
	aplicado, err := recorteDoCtx(ctx).aplicar(filtro)
	if err != nil {
		return pagination.Response[AcessoItemDto]{}, err
	}
	aplicado.Offset, aplicado.Limite = p.Offset(), p.Limit()
	itens, total, err := s.trilhas.Acesso(ctx, aplicado)
	if err != nil {
		return pagination.Response[AcessoItemDto]{}, err
	}
	usuarios := s.usuariosDasLinhas(ctx, coletarUserUUIDs(itens, func(ev access_log.Evento) string { return ev.UserUUID }))
	return NovoAcessoResponseDto(itens, total, p, usuarios), nil
}

// Erros consulta a trilha de erros observados no recorte do ctx.
func (s *serviceImpl) Erros(ctx context.Context, filtro clickhouse.FiltroTrilha, p pagination.Pagination) (pagination.Response[ErroItemDto], error) {
	aplicado, err := recorteDoCtx(ctx).aplicar(filtro)
	if err != nil {
		return pagination.Response[ErroItemDto]{}, err
	}
	aplicado.Offset, aplicado.Limite = p.Offset(), p.Limit()
	itens, total, err := s.trilhas.Erros(ctx, aplicado)
	if err != nil {
		return pagination.Response[ErroItemDto]{}, err
	}
	usuarios := s.usuariosDasLinhas(ctx, coletarUserUUIDs(itens, func(ev errobserve.Evento) string { return ev.UserUUID }))
	return NovoErrosResponseDto(itens, total, p, usuarios), nil
}
