// Ligação do subdomínio organization com a cadeia de middleware e com o CORS:
// adaptadores que resolvem os singletons DO SUBDOMÍNIO na chamada (regra do
// AGENTS.md do bootstrap) e traduzem o vocabulário de erro para os contratos
// de internal/middleware.
//
// O suspendedorWorkspaces é PROVISÓRIO DOCUMENTADO (issue #3): a cascata de
// inativação depende do subdomínio workspace, que só nasce na F3 — enquanto
// ele não existe, NÃO há workspace algum no banco (a tabela sequer existe),
// então a cascata é vazia por natureza e o adaptador devolve 0 sem erro, com
// log. Na F3 este arquivo passa a resolver workspace.Use() NA CHAMADA; nem o
// contrato (contratos.go da organization) nem o serviço mudam.
package bootstrap

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	dominioOrganizacao "workspace-api/internal/identidade/domain/organization"
	orgmodel "workspace-api/internal/identidade/model/organization"
	"workspace-api/internal/middleware"
)

// --- Contrato SuspendedorWorkspaces (cascata organization → workspace) -------

// suspendedorWorkspaces executa a suspensão dos workspaces da organization.
type suspendedorWorkspaces struct{}

func (suspendedorWorkspaces) SuspenderPorOrganization(ctx context.Context, organizationUUID uuid.UUID) (int, error) {
	// F3 liga aqui: ctrl, err := workspace.Use(); ... ctrl.Service.Suspender...
	slog.WarnContext(ctx,
		"[DEGRADADO] cascata organization→workspace sem subdomínio alvo",
		"dominio", orgmodel.Dominio, "subdominio", "workspace",
		"organization_uuid", organizationUUID.String(),
		"motivo", "subdomínio workspace ainda não inicializado (F3); não há workspace no banco até lá")
	return 0, nil
}

var errOrganizationNaoInicializada = errors.New("subdomínio organization não inicializado")

// --- Contrato ProvedorDominiosCustom (white-label no Host e CORS) ------------

// provedorDominiosCustom lista os domínios custom ATIVOS das organizations —
// inativar/remover a organization desativa a resolução imediatamente porque a
// consulta filtra status = 'ativo' (invariante do doc 03).
type provedorDominiosCustom struct{}

func (provedorDominiosCustom) Listar(ctx context.Context) ([]middleware.DominioCustom, error) {
	registrados, err := dominioOrganizacao.MustUse().Service.ListarDominiosAtivos(ctx)
	if err != nil {
		return nil, err
	}
	lista := make([]middleware.DominioCustom, 0, len(registrados))
	for _, r := range registrados {
		lista = append(lista, middleware.DominioCustom{Dominio: r.Valor, OrganizationUUID: r.OrganizationUUID})
	}
	return lista, nil
}

// --- Contrato ResolvedorApiKeys (X-Api-Key) ----------------------------------

// resolvedorApiKeys valida a chave contra o hash persistido — o token em
// claro nunca sai deste adaptador: ele vira SHA-256 antes da consulta.
type resolvedorApiKeys struct{}

func (resolvedorApiKeys) BuscarPorChave(ctx context.Context, chave string) (*middleware.IdentidadeChave, error) {
	chaves := dominioOrganizacao.MustUse().RepositorioApiKeys
	k, err := chaves.BuscarApiKeyPorHash(ctx, orgmodel.HashDeChave(chave))
	if err != nil {
		if errors.Is(err, dominioOrganizacao.ErrApiKeyNaoEncontrada) {
			return nil, middleware.ErrNaoEncontrado
		}
		return nil, err
	}
	if k.Status != orgmodel.StatusAtivo || expirada(k.ExpiresAt, time.Now().UTC()) {
		// Expirada/inativa não se distingue de inexistente — não vaza existência.
		return nil, middleware.ErrNaoEncontrado
	}
	return &middleware.IdentidadeChave{
		OrganizationUUID:     k.OrganizationUUID,
		WorkspacesPermitidos: []uuid.UUID(k.WorkspacesPermitidos),
		EscopoOrganization:   k.EscopoOrganization,
		Permissoes:           []string(k.Permissoes),
	}, nil
}

func expirada(expiresAt *time.Time, agora time.Time) bool {
	return expiresAt != nil && !expiresAt.After(agora)
}

// --- Domínios custom para o CORS ----------------------------------------------

// dominiosCustomParaCors expõe ao engine os domínios white-label ativos — o
// contrato do CORS não muda; a consulta delega ao subdomínio organization
// (antes da F2 era SQL direto provisório sobre tabela inexistente).
func dominiosCustomParaCors(ctx context.Context) ([]string, error) {
	registrados, err := dominioOrganizacao.MustUse().Service.ListarDominiosAtivos(ctx)
	if err != nil {
		return nil, err
	}
	dominios := make([]string, 0, len(registrados))
	for _, r := range registrados {
		dominios = append(dominios, r.Valor)
	}
	return dominios, nil
}
