package clickhouse

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"workspace-api/internal/pkg/errobserve"
	"workspace-api/internal/pkg/log/access_log"
	"workspace-api/internal/pkg/log/audit_log"
)

// timeoutConsulta limita cada leitura (contagem + varredura das linhas): o
// ctx vive até o fim da iteração porque o protocolo nativo transmite as
// linhas de forma lazy.
const timeoutConsulta = 5 * time.Second

// FiltroTrilha carrega os filtros da leitura (E5). Strings vazias e times
// zero = sem filtro. O ESCOPO (organization/workspace) é responsabilidade da
// aplicação chamadora — este pacote só aplica o que vier, sem regra de
// negócio; quem garante fail-closed é a aplicação logs.
//
// Acao vale para auditoria (coluna acao) e para erros (o código estável é a
// "ação" da trilha); na trilha de acesso não existe ação e o campo é
// ignorado. Detalhes extras não são filtráveis (JSON livre por evento).
type FiltroTrilha struct {
	OrganizationUUID string
	WorkspaceUUID    string
	UserUUID         string
	Acao             string
	RayTrace         string
	InstanteInicio   time.Time // zero = sem limite inferior
	InstanteFim      time.Time // zero = sem limite superior
	Offset           int
	Limite           int
}

// Consultor é a face de LEITURA das trilhas gravadas no ClickHouse. O writer
// em lote é assíncrono e descarta sob pressão — a leitura reflete o que
// conseguiu ser gravado, com eventual atraso de janela; é telemetria
// consultável, não fonte transacional.
type Consultor struct {
	conn driver.Conn
}

// NovoConsultor monta o consultor sobre uma conexão já pingada pelo Connect.
func NovoConsultor(conn driver.Conn) *Consultor {
	return &Consultor{conn: conn}
}

// Auditoria devolve uma página da trilha de auditoria em ordem decrescente
// de instante (desempate pelo ray_trace, segunda chave do ORDER BY da tabela)
// e o total SEM paginação — o envelope padrão do doc 04 sai pronto.
func (c *Consultor) Auditoria(ctx context.Context, f FiltroTrilha) ([]audit_log.Evento, int64, error) {
	ctx, cancelar := context.WithTimeout(ctx, timeoutConsulta)
	defer cancelar()

	where, args := whereComum(f, "acao")
	var total int64
	if err := c.contar(ctx, tabelaAuditoria, where, args, &total); err != nil {
		return nil, 0, err
	}
	sql := "SELECT instante, dominio, subdominio, acao, sucesso, organization_uuid, workspace_uuid, user_uuid, ray_trace, detalhes FROM " +
		tabelaAuditoria + where + " ORDER BY instante DESC, ray_trace DESC LIMIT ? OFFSET ?"
	argsConsulta := argumentosConsulta(args, f)
	rows, err := c.conn.Query(ctx, sql, argsConsulta...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	itens := make([]audit_log.Evento, 0)
	for rows.Next() {
		var (
			ev       audit_log.Evento
			sucesso  uint8
			detalhes string
			instante time.Time
		)
		if err := rows.Scan(&instante, &ev.Dominio, &ev.Subdominio, &ev.Acao,
			&sucesso, &ev.OrganizationUUID, &ev.WorkspaceUUID, &ev.UserUUID,
			&ev.RayTrace, &detalhes); err != nil {
			return nil, 0, err
		}
		ev.Instante = instante
		ev.Sucesso = sucesso == 1
		ev.Detalhes = detalhesDe(detalhes)
		itens = append(itens, ev)
	}
	return itens, total, rows.Err()
}

// Acesso devolve uma página da trilha de acesso HTTP — mesmo contrato da
// Auditoria; filtros de tenancy usam as colunas do E5 (db/logs/0004).
func (c *Consultor) Acesso(ctx context.Context, f FiltroTrilha) ([]access_log.Evento, int64, error) {
	ctx, cancelar := context.WithTimeout(ctx, timeoutConsulta)
	defer cancelar()

	where, args := whereComum(f, "")
	var total int64
	if err := c.contar(ctx, tabelaAcesso, where, args, &total); err != nil {
		return nil, 0, err
	}
	sql := "SELECT instante, metodo, path, rota, status, duracao_ms, ip, user_agent, ray_trace, organization_uuid, workspace_uuid, user_uuid FROM " +
		tabelaAcesso + where + " ORDER BY instante DESC, ray_trace DESC LIMIT ? OFFSET ?"
	rows, qerr := c.conn.Query(ctx, sql, argumentosConsulta(args, f)...)
	if qerr != nil {
		return nil, 0, qerr
	}
	defer rows.Close()
	itens := make([]access_log.Evento, 0)
	for rows.Next() {
		var (
			ev       access_log.Evento
			status   int32
			instante time.Time
		)
		if err := rows.Scan(&instante, &ev.Metodo, &ev.Path, &ev.Rota,
			&status, &ev.DuracaoMS, &ev.IP, &ev.UserAgent, &ev.RayTrace,
			&ev.OrganizationUUID, &ev.WorkspaceUUID, &ev.UserUUID); err != nil {
			return nil, 0, err
		}
		ev.Instante = instante
		ev.Status = int(status)
		itens = append(itens, ev)
	}
	return itens, total, rows.Err()
}

// Erros devolve uma página da trilha de erros observados (errobserve) — o
// código estável filtra pela coluna codigo; causa NÃO sai na leitura (é
// texto interno de log, nunca conteúdo de API).
func (c *Consultor) Erros(ctx context.Context, f FiltroTrilha) ([]errobserve.Evento, int64, error) {
	ctx, cancelar := context.WithTimeout(ctx, timeoutConsulta)
	defer cancelar()

	where, args := whereComum(f, "codigo")
	var total int64
	if err := c.contar(ctx, tabelaErro, where, args, &total); err != nil {
		return nil, 0, err
	}
	sql := "SELECT instante, dominio, subdominio, codigo, mensagem, severidade, desconhecido, organization_uuid, workspace_uuid, user_uuid, ray_trace FROM " +
		tabelaErro + where + " ORDER BY instante DESC, ray_trace DESC LIMIT ? OFFSET ?"
	argsConsulta := argumentosConsulta(args, f)
	rows, err := c.conn.Query(ctx, sql, argsConsulta...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	itens := make([]errobserve.Evento, 0)
	for rows.Next() {
		var (
			ev           errobserve.Evento
			severidade   string
			desconhecido uint8
			instante     time.Time
		)
		if err := rows.Scan(&instante, &ev.Dominio, &ev.Subdominio, &ev.Codigo,
			&ev.Mensagem, &severidade, &desconhecido, &ev.OrganizationUUID,
			&ev.WorkspaceUUID, &ev.UserUUID, &ev.RayTrace); err != nil {
			return nil, 0, err
		}
		ev.Instante = instante
		ev.Severidade = errobserve.Severidade(severidade)
		ev.Desconhecido = desconhecido == 1
		itens = append(itens, ev)
	}
	return itens, total, rows.Err()
}

// argumentosConsulta monta a fatia FINAL de argumentos (filtros + limite +
// offset) numa fatia nova — nunca reaproveita o backing array dos filtros,
// que o driver pode reter durante o consumo lazy das linhas.
func argumentosConsulta(filtros []any, f FiltroTrilha) []any {
	completos := make([]any, 0, len(filtros)+2)
	completos = append(completos, filtros...)
	completos = append(completos, f.Limite, f.Offset)
	return completos
}

// whereComum monta as cláusulas dos filtros presentes. colunaAcao é "acao"
// (auditoria), "codigo" (erros) ou "" (acesso — sem filtro de ação).
func whereComum(f FiltroTrilha, colunaAcao string) (string, []any) {
	var condicoes []string
	var args []any
	adicionar := func(condicao string, valor any) {
		condicoes = append(condicoes, condicao)
		args = append(args, valor)
	}
	if f.OrganizationUUID != "" {
		adicionar("organization_uuid = ?", f.OrganizationUUID)
	}
	if f.WorkspaceUUID != "" {
		adicionar("workspace_uuid = ?", f.WorkspaceUUID)
	}
	if f.UserUUID != "" {
		adicionar("user_uuid = ?", f.UserUUID)
	}
	if colunaAcao != "" && f.Acao != "" {
		adicionar(colunaAcao+" = ?", f.Acao)
	}
	if f.RayTrace != "" {
		adicionar("ray_trace = ?", f.RayTrace)
	}
	if !f.InstanteInicio.IsZero() {
		adicionar("instante >= ?", f.InstanteInicio.UTC())
	}
	if !f.InstanteFim.IsZero() {
		adicionar("instante <= ?", f.InstanteFim.UTC())
	}
	if len(condicoes) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(condicoes, " AND "), args
}

func (c *Consultor) contar(ctx context.Context, tabela, where string, args []any, total *int64) error {
	// count() do ClickHouse é UInt64: scan direto em int64 não é suportado.
	var bruto uint64
	if err := c.conn.QueryRow(ctx, "SELECT count(*) FROM "+tabela+where, args...).Scan(&bruto); err != nil {
		return err
	}
	*total = int64(bruto)
	return nil
}

// detalhesDe converte o JSON determinístico da coluna detalhes de volta ao
// mapa do evento; vazio/corrompido = mapa vazio (leitura nunca falha por
// detalhe malformado).
func detalhesDe(bruto string) map[string]string {
	if strings.TrimSpace(bruto) == "" {
		return nil
	}
	detalhes := map[string]string{}
	if err := json.Unmarshal([]byte(bruto), &detalhes); err != nil {
		return map[string]string{}
	}
	return detalhes
}
