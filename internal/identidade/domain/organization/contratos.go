// Contratos com os subdomínios irmãos (ACL — regra 4 de agents/01): a
// cascata de inativação NUNCA chama o irmão direto; entra por estas
// interfaces declaradas no consumidor e ligadas no cmd/bootstrap por
// adaptadores que resolvem o singleton de cada um NA CHAMADA.
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

// EncerradorSessoesUsuarios é o lado da cascata que mora no subdomínio user
// (R4): inativar/remover a organization encerra TODAS as sessões abertas
// (refresh tokens ativos) dos usuários dela — a sessão não sobrevive à dona
// do contrato. A organization alvo é A DO CTX: quem dispara a cascata já
// conferiu o pertencimento (exigirOrganizacaoDoContexto) e a query é
// fail-closed pelo escopo — sem parâmetro que permita errar o alvo.
type EncerradorSessoesUsuarios interface {
	// RevogarTokensDaOrganization encerra as sessões abertas da organization
	// escopada no ctx e devolve quantas foram encerradas. Idempotente:
	// rodar de novo devolve 0 sem erro.
	RevogarTokensDaOrganization(ctx context.Context) (int64, error)
}
