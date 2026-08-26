// Contratos com o mundo externo (ACL — regras 3 e 4 de agents/01):
//
//   - BuscadorModulos é o lado do subdomínio irmão modulo: atribuir licença
//     resolve o slug informado no catálogo. A dependência entra por interface
//     declarada AQUI e é ligada no cmd/bootstrap (adaptador resolve o
//     singleton NA CHAMADA).
package licenca

import (
	"context"
	"errors"

	modelmodulo "workspace-api/internal/licensing/model/modulo"
)

// ErrModuloNaoEncontrado é o vocabulário DO CONTRATO: o adaptador traduz o
// ErrNotFound do irmão módulo para ele — o consumidor nunca importa o domínio
// alheio.
var ErrModuloNaoEncontrado = errors.New("módulo não encontrado no catálogo")

// BuscadorModulos pergunta ao catálogo qual módulo corresponde ao slug — a
// licença referencia o agregado módulo por uuid (nunca join de escrita).
type BuscadorModulos interface {
	// BuscarPorSlug devolve o módulo do catálogo ou ErrModuloNaoEncontrado.
	BuscarPorSlug(ctx context.Context, slug string) (*modelmodulo.Modulo, error)
}

// InvalidadorAcessos observa escritas de LICENÇA para invalidar o cache de
// módulos liberados: revogar/conceder afeta todos os workspaces da
// organization. Declaração do consumidor; implementação Redis no bootstrap.
type InvalidadorAcessos interface {
	// InvalidarOrganization derruba app:{org}:* (grosseira e segura).
	InvalidarOrganization(ctx context.Context, organizationUUID string)
}
