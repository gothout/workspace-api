package clickhouse

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"workspace-api/internal/pkg/config"
	"workspace-api/internal/pkg/errobserve"
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
	erros      []errobserve.Evento
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

func (g *gravadorFake) contagens() (int, int, int) {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	return len(g.acessos), len(g.auditorias), len(g.erros)
}

func (g *gravadorFake) esperarAte(t *testing.T, acessos, auditorias int) {
	t.Helper()
	require.Eventually(t, func() bool {
		a, au, _ := g.contagens()
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

func (g *gravadorFake) InserirErros(_ context.Context, lote []errobserve.Evento) error {
	if err := g.registrarEntrada(); err != nil {
		return err
	}
	g.mutex.Lock()
	defer g.mutex.Unlock()
	g.erros = append(g.erros, lote...)
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

func eventoErro(codigo string) errobserve.Evento {
	return errobserve.Evento{
		Dominio: "identidade", Subdominio: "workspace", Codigo: codigo,
		Severidade: errobserve.SeveridadeWarn,
	}
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
	descartesAcesso, descartesAuditoria, descartesErro := escritor.Descartes()
	require.Zero(t, descartesAcesso+descartesAuditoria+descartesErro)
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

	_, descartesAuditoria, _ := escritor.Descartes()
	require.Equal(t, uint64(2), descartesAuditoria)

	// Liberando o banco, tudo que estava em voo/fila é entregue no Fechar.
	fake.liberar()
	escritor.Fechar()
	acessos, auditoriasEntregues, errosEntregues := fake.contagens()
	require.Equal(t, 0, acessos)
	require.Equal(t, fila+1, auditoriasEntregues) // 1 em voo + 4 drenados
	require.Equal(t, 0, errosEntregues)
	_, descartesAuditoria, _ = escritor.Descartes()
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

	acessos, auditorias, erros := fake.contagens()
	require.Equal(t, 7, acessos)
	require.Equal(t, 7, auditorias)
	require.Equal(t, 0, erros)
}

// TestTrilhaDeErrosFlushEDrain: a trilha de ERROS (errobserve) segue as
// mesmas regras das outras — flush por tamanho de lote SEM esperar a janela
// e dreno completo no Fechar.
func TestTrilhaDeErrosFlushEDrain(t *testing.T) {
	fake := novoGravadorFake()
	escritor := NovaEscritor(fake, config.LogsConfig{LoteTamanho: 3, LoteJanelaMs: 60_000, FilaTamanho: 100})

	for i := 0; i < 3; i++ { // flush por tamanho: sai sem esperar a janela
		escritor.EnfileirarErros(eventoErro("identidade.workspace.slug_em_uso"))
	}
	require.Eventually(t, func() bool {
		_, _, erros := fake.contagens()
		return erros >= 3
	}, 3*time.Second, 5*time.Millisecond)

	escritor.EnfileirarErros(eventoErro("desconhecido")) // abaixo do lote — drena no Fechar
	escritor.Fechar()
	_, _, erros := fake.contagens()
	require.Equal(t, 4, erros)
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
		escritor.EnfileirarErros(eventoErro("x.y"))
	})
	descartesAcesso, descartesAuditoria, descartesErro := escritor.Descartes()
	require.Equal(t, uint64(1), descartesAcesso)
	require.Equal(t, uint64(1), descartesAuditoria)
	require.Equal(t, uint64(1), descartesErro)
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

// TestWhereComumMetodoEClasse (UX3): os filtros novos viram SQL na trilha de
// acesso — método normalizado e faixa [n00,(n+1)00); classe fora do conjunto
// 1–5 nunca entra no WHERE.
func TestWhereComumMetodoEClasse(t *testing.T) {
	where, args := whereComum(FiltroTrilha{Metodo: "get", ClasseStatus: 5}, "")
	assert.Contains(t, where, "metodo = ?")
	assert.Contains(t, where, "status >= ? AND status < ?")
	require.Len(t, args, 3)
	assert.Equal(t, "GET", args[0])
	assert.EqualValues(t, 500, args[1])
	assert.EqualValues(t, 600, args[2])

	where, args = whereComum(FiltroTrilha{ClasseStatus: 9}, "")
	assert.NotContains(t, where, "status")
	assert.Empty(t, args)
}
