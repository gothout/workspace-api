package clickhouse

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"workspace-api/internal/pkg/config"
	"workspace-api/internal/pkg/log/access_log"
	"workspace-api/internal/pkg/log/audit_log"
)

// gravadorFake captura os lotes entregues; o portão controla se Inserir*
// bloqueia (simula banco lento/caído para provar descarte e drain), e
// pegouLote avisa que o worker ENTROU numa gravação (sincronização
// determinística dos testes de fila cheia).
type gravadorFake struct {
	mutex      sync.Mutex
	acessos    []access_log.Evento
	auditorias []audit_log.Evento
	portao     chan struct{} // fechado = livre; aberto = gravação presa
	pegouLote  chan struct{}
	falhar     bool
}

func novoGravadorFake() *gravadorFake {
	portao := make(chan struct{})
	close(portao)
	return &gravadorFake{portao: portao, pegouLote: make(chan struct{}, 64)}
}

func (g *gravadorFake) travar() {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	g.portao = make(chan struct{})
}

func (g *gravadorFake) liberar() {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	close(g.portao)
}

func (g *gravadorFake) contagens() (int, int) {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	return len(g.acessos), len(g.auditorias)
}

func (g *gravadorFake) esperarAte(t *testing.T, acessos, auditorias int) {
	t.Helper()
	require.Eventually(t, func() bool {
		a, au := g.contagens()
		return a >= acessos && au >= auditorias
	}, 3*time.Second, 5*time.Millisecond)
}

func (g *gravadorFake) registrarEntrada() error {
	g.mutex.Lock()
	portao, falhar := g.portao, g.falhar
	g.mutex.Unlock()
	select {
	case g.pegouLote <- struct{}{}:
	default:
	}
	<-portao // gravação presa enquanto o portão estiver fechado
	if falhar {
		return errors.New("clickhouse fora do ar (dublê)")
	}
	return nil
}

func (g *gravadorFake) InserirAcessos(_ context.Context, lote []access_log.Evento) error {
	if err := g.registrarEntrada(); err != nil {
		return err
	}
	g.mutex.Lock()
	defer g.mutex.Unlock()
	g.acessos = append(g.acessos, lote...)
	return nil
}

func (g *gravadorFake) InserirAuditorias(_ context.Context, lote []audit_log.Evento) error {
	if err := g.registrarEntrada(); err != nil {
		return err
	}
	g.mutex.Lock()
	defer g.mutex.Unlock()
	g.auditorias = append(g.auditorias, lote...)
	return nil
}

func eventoAuditoria(i int) audit_log.Evento {
	return audit_log.Evento{
		Dominio: "identidade", Subdominio: "workspace", Acao: "criar",
		Detalhes: audit_log.Detalhes("i", i),
	}
}

func eventoAcesso() access_log.Evento {
	return access_log.Evento{Metodo: "GET", Path: "/api/status", Status: 200, RayTrace: "ray"}
}

// TestFlushPorTamanho: lote completo descarrega SEM esperar a janela.
func TestFlushPorTamanho(t *testing.T) {
	fake := novoGravadorFake()
	escritor := NovaEscritor(fake, config.LogsConfig{LoteTamanho: 3, LoteJanelaMs: 60_000, FilaTamanho: 100})
	defer escritor.Fechar()

	for i := 0; i < 3; i++ {
		escritor.EnfileirarAuditoria(eventoAuditoria(i))
	}
	fake.esperarAte(t, 0, 3)
	descartesAcesso, descartesAuditoria := escritor.Descartes()
	require.Zero(t, descartesAcesso+descartesAuditoria)
}

// TestFlushPorJanela: trilha abaixo do tamanho do lote sai pela janela de tempo.
func TestFlushPorJanela(t *testing.T) {
	fake := novoGravadorFake()
	escritor := NovaEscritor(fake, config.LogsConfig{LoteTamanho: 500, LoteJanelaMs: 30, FilaTamanho: 100})
	defer escritor.Fechar()

	escritor.EnfileirarAcesso(eventoAcesso())
	fake.esperarAte(t, 1, 0)
}

// TestFilaCheiaDescartaEConta: com a gravação presa no portão (e o worker
// dentro dela, confirmado pelo pegouLote), estourar a fila DESCARTA e CONTA —
// e enfileirar NUNCA bloqueia.
func TestFilaCheiaDescartaEConta(t *testing.T) {
	fake := novoGravadorFake()
	fake.travar()
	const fila = 4
	// LoteTamanho 1: o primeiro evento sai da fila direto para a gravação
	// (que trava no portão) — sem depender do ticker de janela para isso.
	escritor := NovaEscritor(fake, config.LogsConfig{LoteTamanho: 1, LoteJanelaMs: 60_000, FilaTamanho: fila})

	// 1º evento: sai da fila direto para o lote do worker, que trava dentro
	// da gravação — a partir daqui a fila não é mais consumida.
	inicio := time.Now()
	escritor.EnfileirarAuditoria(eventoAuditoria(0))
	<-fake.pegouLote

	for i := 1; i <= fila+2; i++ { // 4 enchem a fila, 2 descartam
		escritor.EnfileirarAuditoria(eventoAuditoria(i))
	}
	require.Less(t, time.Since(inicio), time.Second, "enfileirar nunca bloqueia")

	_, descartesAuditoria := escritor.Descartes()
	require.Equal(t, uint64(2), descartesAuditoria)

	// Liberando o banco, tudo que estava em voo/fila é entregue no Fechar.
	fake.liberar()
	escritor.Fechar()
	acessos, auditoriasEntregues := fake.contagens()
	require.Equal(t, 0, acessos)
	require.Equal(t, fila+1, auditoriasEntregues) // 1 em voo + 4 drenados
	_, descartesAuditoria = escritor.Descartes()
	require.Equal(t, uint64(2), descartesAuditoria)
}

// TestDrainNoShutdown: eventos abaixo do lote e sem janela vencida são TODOS
// gravados quando Fechar retorna — shutdown não deixa linha cair.
func TestDrainNoShutdown(t *testing.T) {
	fake := novoGravadorFake()
	escritor := NovaEscritor(fake, config.LogsConfig{LoteTamanho: 1000, LoteJanelaMs: 60_000, FilaTamanho: 100})

	for i := 0; i < 7; i++ {
		escritor.EnfileirarAcesso(eventoAcesso())
		escritor.EnfileirarAuditoria(eventoAuditoria(i))
	}
	escritor.Fechar()

	acessos, auditorias := fake.contagens()
	require.Equal(t, 7, acessos)
	require.Equal(t, 7, auditorias)
}

// TestFecharIdempotenteENadaDepois: segundo Fechar não pânico; enfileirar
// após o fechamento descarta com contagem — nunca pânico de canal fechado.
func TestFecharIdempotenteENadaDepois(t *testing.T) {
	fake := novoGravadorFake()
	escritor := NovaEscritor(fake, config.LogsConfig{LoteTamanho: 10, LoteJanelaMs: 60_000, FilaTamanho: 10})
	escritor.Fechar()
	require.NotPanics(t, func() {
		escritor.Fechar()
		escritor.EnfileirarAcesso(eventoAcesso())
		escritor.EnfileirarAuditoria(eventoAuditoria(1))
	})
	descartesAcesso, descartesAuditoria := escritor.Descartes()
	require.Equal(t, uint64(1), descartesAcesso)
	require.Equal(t, uint64(1), descartesAuditoria)
}

// TestFalhaGravacaoContaSemTravar: erro do banco perde o lote COM CONTAGEM,
// worker continua vivo e as próximas linhas seguem fluindo.
func TestFalhaGravacaoContaSemTravar(t *testing.T) {
	fake := novoGravadorFake()
	fake.falhar = true
	escritor := NovaEscritor(fake, config.LogsConfig{LoteTamanho: 2, LoteJanelaMs: 20, FilaTamanho: 50})

	for i := 0; i < 4; i++ {
		escritor.EnfileirarAuditoria(eventoAuditoria(i))
	}
	require.Eventually(t, func() bool { return escritor.FalhasGravacao() >= 4 },
		3*time.Second, 5*time.Millisecond)

	fake.mutex.Lock()
	fake.falhar = false
	fake.mutex.Unlock()
	escritor.EnfileirarAuditoria(eventoAuditoria(99))
	fake.esperarAte(t, 0, 1)
	escritor.Fechar()
}
