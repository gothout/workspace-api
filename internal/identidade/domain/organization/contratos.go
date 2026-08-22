// Contratos com o subdomínio workspace (ACL — regra 4 de agents/01): a
// cascata de inativação NUNCA chama o irmão direto; entra por esta interface
// declarada no consumidor e ligada no cmd/bootstrap por adaptador que resolve
// o singleton do workspace NA CHAMADA.
package organization

import (
	"context"

	"github.com/google/uuid"
)

// SuspendedorWorkspaces é o lado da cascata que mora no subdomínio workspace
// (F3): inativar/remover a organization suspende os workspaces dela — filho
// nunca fica mais vivo que o pai (invariante do domínio identidade).
type SuspendedorWorkspaces interface {
	// SuspenderPorOrganization inativa TODOS os workspaces ativos da
	// organization e devolve quantos foram suspensos. Idempotente: rodar de
	// novo devolve 0 sem erro.
	SuspenderPorOrganization(ctx context.Context, organizationUUID uuid.UUID) (int, error)
}
