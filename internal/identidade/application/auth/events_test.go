package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// O auditar() chama validarAcaoCatalogada: ação fora do catálogo PANICA
// (reprova em teste/boot) — evento fora do mapping nunca pode nascer
// silencioso. Toda ação do catálogo, ao contrário, é válida por definição.
func TestValidarAcaoCatalogada(t *testing.T) {
	for _, ev := range catalogoEventos {
		require.NotPanics(t, func() { validarAcaoCatalogada(ev.Acao) },
			"ação catalogada %q deve passar na própria validação", ev.Acao)
	}

	assert.PanicsWithValue(t,
		"evento de auditoria não catalogado: identidade.auth.fantasma — declare-o no events.go do subdomínio (auth/events.go)",
		func() { validarAcaoCatalogada("fantasma") },
		"ação fora do events.go reprova com mensagem acionável")
}
