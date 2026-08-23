// Contrato com o mundo externo (ACL — regras 3 e 4 de agents/01):
//
//   - ValidadorWorkspaces confere se o workspace alvo da atribuição pertence
//     à organization do contexto e está ativo. A dependência entra por esta
//     interface declarada no consumidor, ligada no cmd/bootstrap com um
//     adaptador que resolve o singleton do subdomínio irmão NA CHAMADA —
//     nunca import direto de domain/workspace.
//
//   - ObservadorAtribuicoes é o gancho OPCIONAL de INVALIDAÇÃO de caches de
//     leitura (evolução Redis, issue #8): disparado DEPOIS de toda escrita
//     bem-sucedida em identidade_user_atribuicao. Implementação Redis vive
//     no cmd/bootstrap; nil = operação normal sem cache a invalidar — nenhuma
//     regra de negócio depende dele e falha dele NUNCA desfaz a escrita.
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

// ObservadorAtribuicoes é notificado quando as permissões efetivas de um
// usuário mudam por escrita de atribuição. A organization vem do ctx (a
// mesma escopada da escrita). workspaceUUID identifica o par afetado;
// uuid.Nil = "todas as entradas do usuário na organization" (invalidação
// grosseira e segura, ex.: remoção que não carrega o par).
type ObservadorAtribuicoes interface {
	AtribuicaoAlterada(ctx context.Context, usuarioUUID, workspaceUUID uuid.UUID)
}

// OpcaoServico compõe peças opcionais no NewService sem mudar assinaturas
// dos chamadores existentes (seed, testes) — hoje, só o observador.
type OpcaoServico func(*serviceImpl)

// ComObservadorAtribuicoes liga o gancho de invalidação ao service.
func ComObservadorAtribuicoes(o ObservadorAtribuicoes) OpcaoServico {
	return func(s *serviceImpl) { s.observador = o }
}
