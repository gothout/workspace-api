// Ligação do subdomínio workspace com a cadeia de middleware (resolução por
// Host e fallback X-Workspace-Id) e com a cascata de inativação da
// organization: adaptadores que resolvem o singleton DO SUBDOMÍNIO NA
// CHAMADA (regra do AGENTS.md do bootstrap) e traduzem o vocabulário de erro
// para os contratos de internal/middleware.
//
// A busca por slug/uuid é a EXCEÇÃO global documentada (agents/03): acontece
// antes de existir escopo; o resultado nunca vaza para rotas de
// administração.
package bootstrap

import (
	"context"
	"errors"

	"github.com/google/uuid"

	dominioOrganizacao "workspace-api/internal/identidade/domain/organization"
	dominioWorkspace "workspace-api/internal/identidade/domain/workspace"
	"workspace-api/internal/middleware"
)

// --- Contrato ResolvedorWorkspaces -------------------------------------------

// resolvedorWorkspaces delega ao service do workspace — desde a F3 não há
// mais SQL direto sobre a tabela; a lista de rótulos fixos também vem dele.
type resolvedorWorkspaces struct{}

func (resolvedorWorkspaces) BuscarPorSlug(ctx context.Context, slug string) (*middleware.WorkspaceResolvido, error) {
	resolvido, err := dominioWorkspace.MustUse().Service.ResolverPorSlug(ctx, slug)
	if err != nil {
		if errors.Is(err, dominioWorkspace.ErrNotFound) {
			return nil, middleware.ErrNaoEncontrado
		}
		return nil, err
	}
	return paraResolvidoMiddleware(resolvido), nil
}

func (resolvedorWorkspaces) BuscarPorUUID(ctx context.Context, id uuid.UUID) (*middleware.WorkspaceResolvido, error) {
	resolvido, err := dominioWorkspace.MustUse().Service.ResolverPorUUID(ctx, id)
	if err != nil {
		if errors.Is(err, dominioWorkspace.ErrNotFound) {
			return nil, middleware.ErrNaoEncontrado
		}
		return nil, err
	}
	return paraResolvidoMiddleware(resolvido), nil
}

// Fixos pergunta ao SUBDOMÍNIO (a lista mora lá — doc 03).
func (resolvedorWorkspaces) Fixos() []string {
	return dominioWorkspace.MustUse().Service.SlugsFixos()
}

func paraResolvidoMiddleware(r *dominioWorkspace.Resolvido) *middleware.WorkspaceResolvido {
	if r == nil {
		return nil
	}
	return &middleware.WorkspaceResolvido{
		UUID:             r.UUID,
		OrganizationUUID: r.OrganizationUUID,
		Slug:             r.Slug,
		Ativo:            r.Ativo(),
	}
}

// --- Contrato SuspendedorWorkspaces (cascata organization → workspace) -------

// suspendedorWorkspaces executa a cascata pelo CONTRATO da organization:
// resolve o singleton do workspace NA CHAMADA — na F2 era provisório
// ([DEGRADADO]); agora suspende de verdade (filho nunca fica mais vivo que o
// pai). Idempotente: sem workspaces ativos devolve 0 sem erro.
type suspendedorWorkspaces struct{}

func (suspendedorWorkspaces) SuspenderPorOrganization(ctx context.Context, organizationUUID uuid.UUID) (int, error) {
	return dominioWorkspace.MustUse().Service.SuspenderPorOrganization(ctx, organizationUUID)
}

var _ dominioOrganizacao.SuspendedorWorkspaces = suspendedorWorkspaces{}
