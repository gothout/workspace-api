// Contrato com o mundo externo (regra 5 de agents/01): a aplicação logs NÃO
// importa subdomínio de domain e não persiste nada próprio — lê as trilhas
// gravadas pela evolução #9 no ClickHouse, por uma interface estreita
// declarada AQUI (o consumidor dita o contrato) e ligada no cmd/bootstrap.
//
// O recorte de escopo em 3 níveis (plataforma / organization / workspace) é
// regra desta aplicação: o consultor só aplica filtros, nunca decide acesso.
package logs

import (
	"context"

	"workspace-api/internal/infra/clickhouse"
	"workspace-api/internal/pkg/errobserve"
	"workspace-api/internal/pkg/log/access_log"
	"workspace-api/internal/pkg/log/audit_log"
)

// Dependencias carrega o que a aplicação precisa, ligado UMA vez no boot
// pelo cmd/bootstrap (adaptador que resolve o singleton NA CHAMADA).
type Dependencias struct {
	Trilhas ConsultaTrilhas
}

// ConsultaTrilhas é a face de leitura das trilhas — implementada pelo
// Consultor do infra/clickhouse (conformidade estrutural; os tipos de evento
// são os das folhas pkg/log e errobserve, alcançáveis por application).
// Degradado = adaptador do bootstrap devolve ErrIndisponivel, que vira 503
// padronizado — nunca lista vazia silenciosa.
type ConsultaTrilhas interface {
	Auditoria(ctx context.Context, filtro clickhouse.FiltroTrilha) ([]audit_log.Evento, int64, error)
	Acesso(ctx context.Context, filtro clickhouse.FiltroTrilha) ([]access_log.Evento, int64, error)
	Erros(ctx context.Context, filtro clickhouse.FiltroTrilha) ([]errobserve.Evento, int64, error)
}
