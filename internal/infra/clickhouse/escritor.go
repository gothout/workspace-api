package clickhouse

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"workspace-api/internal/pkg/config"
	"workspace-api/internal/pkg/log/access_log"
	"workspace-api/internal/pkg/log/audit_log"
)

// timeoutInsercao limita cada INSERT em lote: banco de log lento atrasa o
// drain, nunca o request (o request só enfileirou).
const timeoutInsercao = 5 * time.Second

// defaults do lote — espelham os defaults da config (validar()); valem
// também quando o escritor é montado direto por teste com config zerada.
const (
	defaultLoteTamanho     = 500
	defaultLoteJanelaMs    = 2000
	defaultFilaTamanho     = 10000
	defaultDrainTimeoutSec = 5
)

// gravadorLotes é a face de persistência que o escritor consome — declarada
// AQUI (consumidor dita o contrato); a implementação real fala com o
// ClickHouse (gravador.go) e os testes injetam dublê.
type gravadorLotes interface {
	InserirAcessos(ctx context.Context, lote []access_log.Evento) error
	InserirAuditorias(ctx context.Context, lote []audit_log.Evento) error
}

// Escritor é o writer assíncrono das duas trilhas: enfileirar NUNCA bloqueia
// (fila cheia descarta e conta), o worker descarrega por tamanho de lote ou
// pela janela de tempo, e Fechar drena tudo que estava pendente.
type Escritor struct {
	gravador gravadorLotes

	filaAcesso    chan access_log.Evento
	filaAuditoria chan audit_log.Evento

	tamanhoLote     int
	janela          time.Duration
	timeoutDrenagem time.Duration

	mutex     sync.Mutex // protege envio nas filas vs. fechamento delas
	fechado   bool
	concluido chan struct{}

	descartesAcesso    atomic.Uint64
	descartesAuditoria atomic.Uint64
	falhasGravacao     atomic.Uint64
}

// NovaEscritor monta o writer PURA: inicia o worker imediatamente. Config
// zerada cai nos defaults (mesmos de config.validar()).
func NovaEscritor(gravador gravadorLotes, cfg config.LogsConfig) *Escritor {
	if gravador == nil {
		panic("clickhouse: escritor sem gravador")
	}
	if cfg.LoteTamanho <= 0 {
		cfg.LoteTamanho = defaultLoteTamanho
	}
	if cfg.LoteJanelaMs <= 0 {
		cfg.LoteJanelaMs = defaultLoteJanelaMs
	}
	if cfg.FilaTamanho <= 0 {
		cfg.FilaTamanho = defaultFilaTamanho
	}
	if cfg.DrainTimeoutSec <= 0 {
		cfg.DrainTimeoutSec = defaultDrainTimeoutSec
	}
	e := &Escritor{
		gravador:        gravador,
		filaAcesso:      make(chan access_log.Evento, cfg.FilaTamanho),
		filaAuditoria:   make(chan audit_log.Evento, cfg.FilaTamanho),
		tamanhoLote:     cfg.LoteTamanho,
		janela:          time.Duration(cfg.LoteJanelaMs) * time.Millisecond,
		timeoutDrenagem: time.Duration(cfg.DrainTimeoutSec) * time.Second,
		concluido:       make(chan struct{}),
	}
	go e.trabalhar()
	return e
}

// EnfileirarAcesso recebe um evento da trilha de acesso — caminho do request:
// O(1), não-bloqueante; fila cheia DESCARTA E CONTA (telemetria perde linha,
// API jamais trava).
func (e *Escritor) EnfileirarAcesso(ev access_log.Evento) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.fechado {
		e.descartesAcesso.Add(1)
		return
	}
	select {
	case e.filaAcesso <- ev:
	default:
		e.descartesAcesso.Add(1)
	}
}

// EnfileirarAuditoria recebe um evento da trilha de auditoria — mesmas regras
// do EnfileirarAcesso.
func (e *Escritor) EnfileirarAuditoria(ev audit_log.Evento) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.fechado {
		e.descartesAuditoria.Add(1)
		return
	}
	select {
	case e.filaAuditoria <- ev:
	default:
		e.descartesAuditoria.Add(1)
	}
}

// Descartes devolve os contadores de descarte por trilha (fila cheia ou
// envio após fechamento) — visíveis para teste e para o /api/status futuro.
func (e *Escritor) Descartes() (acessos, auditorias uint64) {
	return e.descartesAcesso.Load(), e.descartesAuditoria.Load()
}

// FalhasGravacao devolve quantos eventos foram PERDIDOS por erro de gravação
// do lote (contados, nunca silenciados — o log registra a causa).
func (e *Escritor) FalhasGravacao() uint64 { return e.falhasGravacao.Load() }

// Fechar encerra o writer DRENANDO: fecha as filas (sem nova entrada), espera
// o worker esvaziar filas + lotes parciais e gravar o resto. Timeout da
// drenagem vem da config — estourou, o resto é perdido COM LOG (shutdown não
// pode ficar refém de banco caído).
func (e *Escritor) Fechar() {
	e.mutex.Lock()
	if e.fechado {
		e.mutex.Unlock()
		return
	}
	e.fechado = true
	close(e.filaAcesso)
	close(e.filaAuditoria)
	e.mutex.Unlock()

	select {
	case <-e.concluido:
	case <-time.After(e.timeoutDrenagem):
		slog.Warn("clickhouse.escritor.drenagem_timeout",
			"timeout_seg", int(e.timeoutDrenagem.Seconds()),
			"acessos_pendentes", len(e.filaAcesso),
			"auditorias_pendentes", len(e.filaAuditoria))
	}
}

// trabalhar é o loop do worker: acumula as duas trilhas, descarrega quando um
// lote completa OU a janela estoura; no fechamento das filas, drena o resto.
func (e *Escritor) trabalhar() {
	defer close(e.concluido)
	acessos := make([]access_log.Evento, 0, e.tamanhoLote)
	auditorias := make([]audit_log.Evento, 0, e.tamanhoLote)
	canalAcesso := e.filaAcesso
	canalAuditoria := e.filaAuditoria
	ticker := time.NewTicker(e.janela)
	defer ticker.Stop()

	for canalAcesso != nil || canalAuditoria != nil {
		select {
		case ev, aberto := <-canalAcesso:
			if !aberto {
				canalAcesso = nil // fechada: para de selectar nela (nil bloqueia)
				continue
			}
			acessos = append(acessos, ev)
			if len(acessos) >= e.tamanhoLote {
				e.descarregar(&acessos, &auditorias)
			}
		case ev, aberto := <-canalAuditoria:
			if !aberto {
				canalAuditoria = nil
				continue
			}
			auditorias = append(auditorias, ev)
			if len(auditorias) >= e.tamanhoLote {
				e.descarregar(&acessos, &auditorias)
			}
		case <-ticker.C:
			e.descarregar(&acessos, &auditorias)
		}
	}
	// Dreno final: filas fechadas podem ainda ter itens em buffer.
	for ev := range canalRestante(e.filaAcesso) {
		acessos = append(acessos, ev)
	}
	for ev := range canalRestante(e.filaAuditoria) {
		auditorias = append(auditorias, ev)
	}
	e.descarregar(&acessos, &auditorias)
}

// canalRestante converte uma fila FECHADA em iterável dos itens remanescentes.
func canalRestante[T any](fila <-chan T) <-chan T {
	saida := make(chan T, len(fila))
	for {
		select {
		case ev, aberto := <-fila:
			if !aberto {
				close(saida)
				return saida
			}
			saida <- ev
		default:
			close(saida)
			return saida
		}
	}
}

// descarregar grava os lotes parciais e zera os buffers. Erro de gravação
// PERDE o lote com contagem e log — retry aqui atrasaria o worker e a fila
// só cresceria (descarte em cascata); honestidade contábil vale mais.
func (e *Escritor) descarregar(acessos *[]access_log.Evento, auditorias *[]audit_log.Evento) {
	if len(*acessos) > 0 {
		ctx, cancelar := context.WithTimeout(context.Background(), timeoutInsercao)
		if err := e.gravador.InserirAcessos(ctx, *acessos); err != nil {
			e.falhasGravacao.Add(uint64(len(*acessos)))
			slog.Error("clickhouse.escritor.lote_acesso_perdido",
				"tamanho", len(*acessos), "causa", err.Error())
		}
		cancelar()
		*acessos = (*acessos)[:0]
	}
	if len(*auditorias) > 0 {
		ctx, cancelar := context.WithTimeout(context.Background(), timeoutInsercao)
		if err := e.gravador.InserirAuditorias(ctx, *auditorias); err != nil {
			e.falhasGravacao.Add(uint64(len(*auditorias)))
			slog.Error("clickhouse.escritor.lote_auditoria_perdido",
				"tamanho", len(*auditorias), "causa", err.Error())
		}
		cancelar()
		*auditorias = (*auditorias)[:0]
	}
}
