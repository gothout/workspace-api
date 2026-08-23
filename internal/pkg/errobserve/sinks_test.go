package errobserve

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// capturaSlog troca o logger padrão por um buffer durante o teste.
func capturaSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buffer bytes.Buffer
	anterior := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buffer, nil)))
	t.Cleanup(func() { slog.SetDefault(anterior) })
	return &buffer
}

func TestSlogPadraoNiveisEOrdem(t *testing.T) {
	saida := capturaSlog(t)
	ResetarParaTeste()

	obs := For("identidade", "workspace", entradasDeExemplo())
	_ = obs.Observe(context.Background(), errSlugEmUso)

	linha := saida.String()
	if !strings.Contains(linha, "level=WARN") || !strings.Contains(linha, "msg=erro.identidade.workspace.slug_em_uso") {
		t.Fatalf("warn não saiu como esperado: %s", linha)
	}
	if !strings.Contains(linha, "severidade=warn") || !strings.Contains(linha, "codigo=identidade.workspace.slug_em_uso") {
		t.Fatalf("campos do evento ausentes: %s", linha)
	}

	// Desconhecido sobe como ERROR e agrupa em erro.desconhecido.
	saida.Reset()
	_ = obs.Observe(context.Background(), errors.New("falha sem nome"))
	linha = saida.String()
	if !strings.Contains(linha, "level=ERROR") || !strings.Contains(linha, `msg=erro.desconhecido`) ||
		!strings.Contains(linha, "codigo=desconhecido") {
		t.Fatalf("desconhecido não saiu como esperado: %s", linha)
	}
}

func TestAlertaAgregaCriticalPorJanela(t *testing.T) {
	saida := capturaSlog(t)
	alerta := Alerta(50 * time.Millisecond)

	evCritical := func(codigo string) Evento {
		return Evento{
			Instante: time.Now().UTC(), Dominio: "identidade", Subdominio: "workspace",
			Codigo: codigo, Severidade: SeveridadeCritical,
		}
	}

	alerta.Registrar(evCritical("a.b"))
	alerta.Registrar(evCritical("a.b"))
	alerta.Registrar(evCritical("c.d")) // código diferente — janela própria

	if alerta.(*alertaSink).AlertasEmitidos() != 2 {
		t.Fatalf("esperava 2 alertas (um por código), recebi %d", alerta.(*alertaSink).AlertasEmitidos())
	}
	if alerta.(*alertaSink).SuprimidosNaJanela("a.b") != 1 {
		t.Fatalf("agregação na janela falhou: %d", alerta.(*alertaSink).SuprimidosNaJanela("a.b"))
	}
	if strings.Count(saida.String(), "[ALERTA]") != 2 {
		t.Fatalf("linhas [ALERTA] inesperadas: %s", saida.String())
	}

	// Warn passa direto pelo sink de alerta (no-op).
	alerta.Registrar(Evento{Codigo: "x.y", Severidade: SeveridadeWarn})
	if alerta.(*alertaSink).AlertasEmitidos() != 2 {
		t.Fatal("warn não deveria disparar alerta")
	}

	// Janela estourada: o próximo critical do mesmo código grita de novo,
	// carregando a contagem agregada da janela anterior.
	time.Sleep(60 * time.Millisecond)
	alerta.Registrar(evCritical("a.b"))
	if alerta.(*alertaSink).AlertasEmitidos() != 3 {
		t.Fatalf("janela não renovou: %d", alerta.(*alertaSink).AlertasEmitidos())
	}
	if !strings.Contains(saida.String(), "agregados_na_janela_anterior=1") {
		t.Fatalf("contagem agregada ausente no novo alerta: %s", saida.String())
	}
}

// Disputa real: N goroutines observando enquanto sinks são redefinidos e o
// registro cresce — nenhum pânico, todo erro devolvido intacto, contagem
// consistente (roda também sob -race).
func TestDisputaObserveDefinirRegistro(t *testing.T) {
	capturaSlog(t) // silencia a saída da disputa
	ResetarParaTeste()
	obs := For("identidade", "workspace", entradasDeExemplo())

	const goroutines = 24
	porGoroutine := 40
	var barreira sync.WaitGroup
	largada := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		barreira.Add(1)
		go func(i int) {
			defer barreira.Done()
			<-largada
			for j := 0; j < porGoroutine; j++ {
				_ = obs.Observe(context.Background(), errSlugEmUso)
				switch (i + j) % 3 {
				case 0:
					DefinirSinks(&sinkColetor{})
				case 1:
					_ = CatalogoGlobal()
				case 2:
					_ = SeveridadeDoCodigo("identidade.workspace.slug_em_uso")
				}
			}
		}(i)
	}
	close(largada)
	barreira.Wait()

	if FalhasSink() != 0 {
		t.Fatalf("pânicos de sink durante a disputa: %d", FalhasSink())
	}
}
