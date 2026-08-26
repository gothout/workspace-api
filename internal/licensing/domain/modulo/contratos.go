// Contratos com o mundo externo (ACL — regras 3 e 4 de agents/01):
//
//   - VerificadorLicencas é o lado do subdomínio irmão licenca: a remoção de
//     um módulo só é permitida sem licenças vivas. A dependência entra por
//     interface declarada AQUI, implementada pelo licenca e ligada no
//     cmd/bootstrap (adaptador resolve o singleton NA CHAMADA). Nil = boot
//     sem a peça → remoção recusa fechada (fail-closed).
package modulo

import (
	"context"

	"github.com/google/uuid"
)

// VerificadorLicencas pergunta ao agregado licença se o módulo ainda tem
// concessões vivas em qualquer organization.
type VerificadorLicencas interface {
	// ExisteParaModulo devolve true quando há AO MENOS UMA licença viva
	// (deleted_at IS NULL) para o módulo — consulta GLOBAL por natureza:
	// a regra de remoção é da plataforma, não de um tenant.
	ExisteParaModulo(ctx context.Context, moduloUUID uuid.UUID) (bool, error)
}

// InvalidadorAcessos observa escritas do CATÁLOGO para invalidar o cache de
// módulos liberados (app:*). Declaração do consumidor; implementação Redis
// ligada no bootstrap. Nil = sem cache — operação normal, só mais cara.
type InvalidadorAcessos interface {
	// InvalidarTudo derruba o namespace inteiro: desativar um módulo afeta
	// TODO par (organization, workspace) que o usa.
	InvalidarTudo(ctx context.Context)
}
