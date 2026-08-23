package access_log

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSlogPadraoEscreveEventoCompleto(t *testing.T) {
	var saida bytes.Buffer
	anterior := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&saida, nil)))
	t.Cleanup(func() { slog.SetDefault(anterior) })

	SlogPadrao().Registrar(Evento{
		Instante:  time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC),
		Metodo:    "POST",
		Path:      "/api/application/identidade/auth/login",
		Rota:      "/api/application/identidade/auth/login",
		Status:    200,
		DuracaoMS: 7,
		IP:        "10.0.0.1",
		UserAgent: "teste",
		RayTrace:  "ray-42",
	})

	linha := saida.String()
	require.Contains(t, linha, "msg=acesso")
	require.Contains(t, linha, "metodo=POST")
	require.Contains(t, linha, "status=200")
	require.Contains(t, linha, "duracao_ms=7")
	require.Contains(t, linha, "ray_trace=ray-42")
}

func TestDestinoFakePorInterface(t *testing.T) {
	// Prova que qualquer consumidor pode capturar eventos por Destino — é o
	// desenho usado nos testes do escritor e dos services.
	var capturados []Evento
	var destino Destino = destinoFunc(func(ev Evento) { capturados = append(capturados, ev) })
	destino.Registrar(Evento{Metodo: "GET", Status: 404})
	require.Len(t, capturados, 1)
	require.Equal(t, 404, capturados[0].Status)
}

type destinoFunc func(Evento)

func (f destinoFunc) Registrar(ev Evento) { f(ev) }
