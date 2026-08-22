// Contrato com o mundo externo (ACL — regras 3 e 4 de agents/01):
//
//   - ValidadorWorkspaces confere se o workspace alvo da atribuição pertence
//     à organization do contexto e está ativo. A dependência entra por esta
//     interface declarada no consumidor, ligada no cmd/bootstrap com um
//     adaptador que resolve o singleton do subdomínio irmão NA CHAMADA —
//     nunca import direto de domain/workspace.
package user

import (
	"context"

	"github.com/google/uuid"
)

type ValidadorWorkspaces interface {
	// Pertence responde se o workspace existe, está ativo e pertence à
	// organization informada — workspace alheio/inexistente/inativo é a MESMA
	// recusa (não vaza existência entre tenants).
	Pertence(ctx context.Context, organizationUUID, workspaceUUID uuid.UUID) (bool, error)
}
