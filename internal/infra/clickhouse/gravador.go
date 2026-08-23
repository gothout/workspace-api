package clickhouse

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"workspace-api/internal/pkg/errobserve"
	"workspace-api/internal/pkg/log/access_log"
	"workspace-api/internal/pkg/log/audit_log"
)

// Tabelas das trilhas — DDL versionado em db/logs/ (aplicação manual, fora
// do runner de migrations do Postgres). Nomes fixos: mudar aqui exige nova
// versão do DDL.
const (
	tabelaAcesso    = "log_acesso"
	tabelaAuditoria = "log_auditoria"
	tabelaErro      = "log_erro"
)

// gravadorClickhouse implementa gravadorLotes com INSERT em lote nativo do
// ClickHouse (PrepareBatch/Send) — uma ida ao banco por flush, não por linha.
type gravadorClickhouse struct {
	conn driver.Conn
}

// NovoGravador monta o gravador real sobre uma conexão já pingada pelo Connect.
func NovoGravador(conn driver.Conn) *gravadorClickhouse {
	return &gravadorClickhouse{conn: conn}
}

func (g *gravadorClickhouse) InserirAcessos(ctx context.Context, lote []access_log.Evento) error {
	if len(lote) == 0 {
		return nil
	}
	batch, err := g.conn.PrepareBatch(ctx, "INSERT INTO "+tabelaAcesso)
	if err != nil {
		return err
	}
	for _, ev := range lote {
		if err := batch.Append(
			ev.Instante, ev.Metodo, ev.Path, ev.Rota,
			int32(ev.Status), ev.DuracaoMS, ev.IP, ev.UserAgent, ev.RayTrace,
		); err != nil {
			return err
		}
	}
	return batch.Send()
}

func (g *gravadorClickhouse) InserirAuditorias(ctx context.Context, lote []audit_log.Evento) error {
	if len(lote) == 0 {
		return nil
	}
	batch, err := g.conn.PrepareBatch(ctx, "INSERT INTO "+tabelaAuditoria)
	if err != nil {
		return err
	}
	for _, ev := range lote {
		if err := batch.Append(
			ev.Instante, ev.Dominio, ev.Subdominio, ev.Acao,
			uint8(boolByte(ev.Sucesso)),
			ev.OrganizationUUID, ev.WorkspaceUUID, ev.UserUUID, ev.RayTrace,
			jsonDeterministico(ev.Detalhes),
		); err != nil {
			return err
		}
	}
	return batch.Send()
}

func (g *gravadorClickhouse) InserirErros(ctx context.Context, lote []errobserve.Evento) error {
	if len(lote) == 0 {
		return nil
	}
	batch, err := g.conn.PrepareBatch(ctx, "INSERT INTO "+tabelaErro)
	if err != nil {
		return err
	}
	for _, ev := range lote {
		if err := batch.Append(
			ev.Instante, ev.Dominio, ev.Subdominio, ev.Codigo,
			ev.Mensagem, string(ev.Severidade), uint8(boolByte(ev.Desconhecido)),
			ev.OrganizationUUID, ev.WorkspaceUUID, ev.UserUUID, ev.RayTrace,
			ev.Causa,
		); err != nil {
			return err
		}
	}
	return batch.Send()
}

func boolByte(b bool) int {
	if b {
		return 1
	}
	return 0
}

// jsonDeterministico serializa os detalhes do evento em JSON com chaves
// ordenadas (encoding/json ordena map) — coluna String na tabela, consultável
// por LIKE/match sem tipar cada detalhe extra.
func jsonDeterministico(detalhes map[string]string) string {
	if len(detalhes) == 0 {
		return ""
	}
	b, err := json.Marshal(detalhes)
	if err != nil {
		// Map de string nunca falha; defesa honesta mesmo assim.
		chaves := make([]string, 0, len(detalhes))
		for chave := range detalhes {
			chaves = append(chaves, chave)
		}
		sort.Strings(chaves)
		return "{}"
	}
	return string(b)
}
