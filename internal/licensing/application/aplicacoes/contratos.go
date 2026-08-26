// Contratos com o mundo externo (ACL — regra 5 de agents/01): a aplicação
// orquestra SÓ o subdomínio ativação — as aplicações liberadas do par
// (organization, workspace) resolvido na requisição.
package aplicacoes

import (
	"context"

	"github.com/google/uuid"

	modelativacao "workspace-api/internal/licensing/model/ativacao"
)

// ProvedorAcessos responde quais módulos o par (organization, workspace) tem
// liberados — implementado no cmd/bootstrap pelo adaptador que resolve o
// singleton da ativação NA CHAMADA.
type ProvedorAcessos interface {
	Liberadas(ctx context.Context, organizationUUID, workspaceUUID uuid.UUID) ([]modelativacao.AplicacaoDisponivelDto, error)
}
