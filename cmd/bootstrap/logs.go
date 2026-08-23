// Frente das TRILHAS DE LOG assíncronas (evolução #9) e dos SINKS do
// observador de erros (evolução errobserve): liga o writer em lote do
// ClickHouse às três trilhas (auditoria das escritas, acesso HTTP e erros
// observados) ou, com a dependência degradada, aos destinos stdout. Também
// monta o sink de ALERTA ([ALERTA] para críticos, janela agregada) e define
// a lista de sinks global — o slog padrão é sempre mantido pelo pacote.
// Toda ligação entre infra e pkg mora AQUI.
package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"workspace-api/internal/infra/clickhouse"
	"workspace-api/internal/pkg/config"
	"workspace-api/internal/pkg/errobserve"
	"workspace-api/internal/pkg/log/access_log"
	"workspace-api/internal/pkg/log/audit_log"

	aplicacaologs "workspace-api/internal/identidade/application/logs"
)

// trilhas carrega os destinos prontos para consumo pelo middleware de acesso
// (routes.Opcoes.AcessoLog), pelos auditar() dos subdomínios (ComTrilha) e
// pelo observador de erros (sink ClickHouse da terceira trilha).
type trilhas struct {
	escritor  *clickhouse.Escritor // nil = degradado (stdout)
	auditoria audit_log.Destino
	acesso    access_log.Destino
}

// iniciarTrilhas monta o singleton ClickHouse (DEGRADÁVEL — nunca derruba o
// boot), escolhe os destinos: writer assíncrono quando existe; SlogPadrao
// (stdout, mesmo formato legado) quando não — o log [DEGRADADO] correspondente
// já saiu do Connect. Liga também os sinks do errobserve: slog sempre ativo,
// alerta agregado sempre presente e a trilha de ERROS no ClickHouse quando ele
// existe. Degradado = evento de plataforma sistema.degradacao_dependencia.
func iniciarTrilhas() (*trilhas, error) {
	cfg := config.MustUse()
	obsPlataforma := observadorPlataforma()

	escritor, err := clickhouse.InitClickhouse()
	if err != nil {
		return nil, err
	}
	if escritor == nil {
		slog.Info("[BOOTSTRAP] trilhas de log no STDOUT (clickhouse ausente)",
			"lote_tamanho", cfg.Logs.LoteTamanho)
		obsPlataforma.Observe(context.Background(), fmt.Errorf("%w: clickhouse indisponível no boot", errobserve.ErrDegradacao))
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

	// Sinks do errobserve: [slog (sempre, garantido pelo pacote)] + alerta +
	// writer ClickHouse da trilha de erros. Sem ClickHouse, o slog segue
	// sozinho — telemetria degradada nunca fica sem destino.
	alerta := errobserve.Alerta(time.Duration(cfg.Logs.AlertaJanelaSeg) * time.Second)
	errobserve.DefinirSinks(alerta, destinoErros{escritor})
	return &trilhas{
		escritor:  escritor,
		auditoria: destinoAuditoria{escritor},
		acesso:    destinoAcesso{escritor},
	}, nil
}

// Go NÃO sobrecarrega métodos: as três interfaces Destino/Sink declaram o
// mesmo nome de método com tipos de evento diferentes — um adaptador fino por
// trilha implementa cada contrato estruturalmente, delegando ao escritor.

type destinoAuditoria struct{ escritor *clickhouse.Escritor }

func (d destinoAuditoria) Registrar(ev audit_log.Evento) {
	d.escritor.EnfileirarAuditoria(ev)
}

type destinoAcesso struct{ escritor *clickhouse.Escritor }

func (d destinoAcesso) Registrar(ev access_log.Evento) {
	d.escritor.EnfileirarAcesso(ev)
}

type destinoErros struct{ escritor *clickhouse.Escritor }

func (d destinoErros) Registrar(ev errobserve.Evento) {
	d.escritor.EnfileirarErros(ev)
}

// --- Leitura das trilhas (E5) ----------------------------------------------

// consultorLogs liga o contrato ConsultaTrilhas da aplicação logs ao
// consultor do ClickHouse, resolvendo o singleton NA CHAMADA (mesma
// disciplina dos demais adaptadores): degradado/ausente devolve
// aplicacaologs.ErrIndisponivel — que vira o 503 padronizado, nunca lista
// vazia silenciosa.
type consultorLogs struct{}

func (consultorLogs) Auditoria(ctx context.Context, f clickhouse.FiltroTrilha) ([]audit_log.Evento, int64, error) {
	c, err := consultadorAtivo()
	if err != nil {
		return nil, 0, err
	}
	return c.Auditoria(ctx, f)
}

func (consultorLogs) Acesso(ctx context.Context, f clickhouse.FiltroTrilha) ([]access_log.Evento, int64, error) {
	c, err := consultadorAtivo()
	if err != nil {
		return nil, 0, err
	}
	return c.Acesso(ctx, f)
}

func (consultorLogs) Erros(ctx context.Context, f clickhouse.FiltroTrilha) ([]errobserve.Evento, int64, error) {
	c, err := consultadorAtivo()
	if err != nil {
		return nil, 0, err
	}
	return c.Erros(ctx, f)
}

// consultadorAtivo resolve o consultor do processo: não inicializado OU
// degradado = ErrIndisponivel (wrapping com %w — errors.Is tem que casar).
func consultadorAtivo() (*clickhouse.Consultor, error) {
	consultor, err := clickhouse.UseConsultor()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", aplicacaologs.ErrIndisponivel, err)
	}
	if consultor == nil {
		return nil, aplicacaologs.ErrIndisponivel
	}
	return consultor, nil
}

var _ aplicacaologs.ConsultaTrilhas = consultorLogs{}
