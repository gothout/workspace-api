// Frente das TRILHAS DE LOG assíncronas (evolução #9): liga o writer em lote
// do ClickHouse às duas trilhas (auditoria das escritas e acesso HTTP) ou,
// com a dependência degradada, aos destinos stdout. Toda ligação entre infra
// e pkg/log mora AQUI — nenhum outro pacote conhece o concreto dos dois.
package bootstrap

import (
	"log/slog"

	"workspace-api/internal/infra/clickhouse"
	"workspace-api/internal/pkg/config"
	"workspace-api/internal/pkg/log/access_log"
	"workspace-api/internal/pkg/log/audit_log"
)

// trilhas carrega os destinos prontos para consumo pelo middleware de acesso
// (routes.Opcoes.AcessoLog) e pelos auditar() dos subdomínios (ComTrilha).
type trilhas struct {
	escritor  *clickhouse.Escritor // nil = degradado (stdout)
	auditoria audit_log.Destino
	acesso    access_log.Destino
}

// iniciarTrilhas monta o singleton ClickHouse (DEGRADÁVEL — nunca derruba o
// boot) e escolhe os destinos: writer assíncrono quando existe; SlogPadrao
// (stdout, mesmo formato legado) quando não — o log [DEGRADADO] correspondente
// já saiu do Connect.
func iniciarTrilhas() (*trilhas, error) {
	cfg := config.MustUse()
	escritor, err := clickhouse.InitClickhouse()
	if err != nil {
		return nil, err
	}
	if escritor == nil {
		slog.Info("[BOOTSTRAP] trilhas de log no STDOUT (clickhouse ausente)",
			"lote_tamanho", cfg.Logs.LoteTamanho)
		return &trilhas{
			auditoria: audit_log.SlogPadrao(),
			acesso:    access_log.SlogPadrao(),
		}, nil
	}
	slog.Info("[BOOTSTRAP] trilhas de log no CLICKHOUSE",
		"database", cfg.Databases.ClickHouse.Database,
		"lote_tamanho", cfg.Logs.LoteTamanho,
		"lote_janela_ms", cfg.Logs.LoteJanelaMs,
		"fila_tamanho", cfg.Logs.FilaTamanho)
	return &trilhas{
		escritor:  escritor,
		auditoria: destinoAuditoria{escritor},
		acesso:    destinoAcesso{escritor},
	}, nil
}

// Go NÃO sobrecarrega métodos: as duas interfaces Destino declaram o mesmo
// nome de método com tipos de evento diferentes — um adaptador fino por
// trilha implementa cada contrato estruturalmente, delegando ao escritor.

type destinoAuditoria struct{ escritor *clickhouse.Escritor }

func (d destinoAuditoria) Registrar(ev audit_log.Evento) {
	d.escritor.EnfileirarAuditoria(ev)
}

type destinoAcesso struct{ escritor *clickhouse.Escritor }

func (d destinoAcesso) Registrar(ev access_log.Evento) {
	d.escritor.EnfileirarAcesso(ev)
}
