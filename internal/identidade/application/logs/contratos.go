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

	"github.com/google/uuid"

	"workspace-api/internal/infra/clickhouse"
	"workspace-api/internal/pkg/errobserve"
	"workspace-api/internal/pkg/log/access_log"
	"workspace-api/internal/pkg/log/audit_log"
)

// Dependencias carrega o que a aplicação precisa, ligado UMA vez no boot
// pelo cmd/bootstrap (adaptador que resolve o singleton NA CHAMADA).
type Dependencias struct {
	Trilhas ConsultaTrilhas
	// Usuarios é o enriquecimento OPCIONAL das linhas com nome/e-mail do
	// usuário (UX2): nil = campos vazios nas respostas, a consulta segue
	// funcionando — degradação honesta.
	Usuarios ResolvedorUsuarios
	// Opcoes alimenta as opções de filtro recortadas pelo escopo (UX3) —
	// exigido no boot: endpoint sem opções seria recurso morto.
	Opcoes ProvedorOpcoes
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

// UsuarioLog é o enriquecimento de UMA identidade referenciada pelas linhas
// de log: o trio que a coluna "Usuário" da tela exibe.
type UsuarioLog struct {
	UUID  string
	Nome  string
	Email string
}

// ResolvedorUsuarios devolve EM LOTE nome/e-mail dos usuários pedidos
// (issue #29) — implementado no bootstrap sobre o repositório do subdomínio
// user (identidade_user_user mora no Postgres; a trilha, no ClickHouse:
// join entre bancos não existe, resolução em lote sim). Só chegam aqui uuids
// de linhas JÁ recortadas pelo service; linhas sem usuário (sistema/
// anônimo/removido) simplesmente não voltam no mapa — campos vazios na tela.
type ResolvedorUsuarios interface {
	Resolver(ctx context.Context, uuids []string) (map[string]UsuarioLog, error)
}

// OpcaoFiltro é UMA opção de Select do painel de logs (UX3): o uuid para
// preencher o filtro e o nome para exibir. Nada além disso — a lista serve
// a quem JÁ pode ler os logs daquele recorte, então uuid+nome bastam.
type OpcaoFiltro struct {
	UUID string
	Nome string
}

// ProvedorOpcoes responde as listas de referência na granularidade pedida
// pelo service (issue #30): ponteiro nil = SEM filtro nessa dimensão (caminho
// da plataforma), ponteiro preenchido = preso ao recorte correspondente. As
// consultas rodam sobre os repositórios dos subdomínios (organization,
// workspace, user) via adaptador ligado no bootstrap — aplicação não toca em
// *gorm.DB.
type ProvedorOpcoes interface {
	Organizacoes(ctx context.Context, organizationUUID *uuid.UUID) ([]OpcaoFiltro, error)
	Workspaces(ctx context.Context, organizationUUID *uuid.UUID) ([]OpcaoFiltro, error)
	Usuarios(ctx context.Context, organizationUUID, workspaceUUID *uuid.UUID) ([]OpcaoFiltro, error)
}
