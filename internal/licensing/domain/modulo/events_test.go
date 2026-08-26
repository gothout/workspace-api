package modulo

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// O auditar() chama validarAcaoCatalogada: ação fora do catálogo PANICA —
// evento fora do mapping nunca pode nascer silencioso.
func TestValidarAcaoCatalogada(t *testing.T) {
	for _, ev := range catalogoEventos {
		require.NotPanics(t, func() { validarAcaoCatalogada(ev.Acao) },
			"ação catalogada %q deve passar na própria validação", ev.Acao)
	}

	assert.PanicsWithValue(t,
		"evento de auditoria não catalogado: licensing.modulo.fantasma — declare-o no events.go do subdomínio (modulo/events.go)",
		func() { validarAcaoCatalogada("fantasma") },
		"ação fora do events.go reprova com mensagem acionável")
}
