package audit_log

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDetalhesNormalizaPares(t *testing.T) {
	detalhes := Detalhes("email", "a@b.c", "tentativa", 3)
	require.Equal(t, map[string]string{"email": "a@b.c", "tentativa": "3"}, detalhes)
}

func TestDetalhesVazioENilSeguro(t *testing.T) {
	require.Nil(t, Detalhes())
	require.Nil(t, Detalhes(nil...))
}

func TestDetalhesChaveSoltaViraValorVazio(t *testing.T) {
	detalhes := Detalhes("so_chave")
	require.Equal(t, map[string]string{"so_chave": ""}, detalhes)
}

func TestSlogPadraoEscreveLinhaDeterministica(t *testing.T) {
	var saida bytes.Buffer
	anterior := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&saida, nil)))
	t.Cleanup(func() { slog.SetDefault(anterior) })

	SlogPadrao().Registrar(Evento{
		Instante:         time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC),
		Dominio:          "identidade",
		Subdominio:       "organization",
		Acao:             "criar",
		Sucesso:          true,
		OrganizationUUID: "org-1",
		UserUUID:         "user-1",
		RayTrace:         "ray-1",
		Detalhes:         Detalhes("zz", 1, "aa", 2),
	})

	linha := saida.String()
	// Falha/sucesso manda o nível; detalhes saem em ordem de chave (determinístico).
	require.Contains(t, linha, "level=INFO")
	require.Contains(t, linha, "msg=auditoria.identidade.criar")
	require.Contains(t, linha, "dominio=identidade")
	require.Contains(t, linha, "organization_uuid=org-1")
	require.Contains(t, linha, "ray_trace=ray-1")
	require.Less(t, strings.Index(linha, "aa=2"), strings.Index(linha, "zz=1"))
}

func TestSlogPadraoFalhaViraWarn(t *testing.T) {
	var saida bytes.Buffer
	anterior := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&saida, nil)))
	t.Cleanup(func() { slog.SetDefault(anterior) })

	SlogPadrao().Registrar(Evento{Dominio: "identidade", Subdominio: "auth", Acao: "login"})
	require.Contains(t, saida.String(), "level=WARN")
}
