package logs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFiltrosDeAcessoMetodoEClasse (UX3): parsing dos query params novos e
// propagação — Acesso leva os filtros ao consultor; auditoria/erros os
// ZERAM (as colunas só existem na trilha de acesso).
func TestFiltrosDeAcessoMetodoEClasse(t *testing.T) {
	// Parsing: método normalizado e classe aceita as duas formas.
	filtro, err := LogsFiltroRequestDto{Metodo: "get", StatusClasse: "4xx"}.ParaFiltro()
	require.NoError(t, err)
	assert.Equal(t, "GET", filtro.Metodo)
	assert.Equal(t, 4, filtro.ClasseStatus)

	filtro, err = LogsFiltroRequestDto{StatusClasse: "5xx"}.ParaFiltro()
	require.NoError(t, err)
	assert.Equal(t, 5, filtro.ClasseStatus)
	assert.Empty(t, filtro.Metodo)

	// Inválidos viram ErrFiltroInvalido (400).
	for _, dto := range []LogsFiltroRequestDto{
		{Metodo: "nao-e-metodo"},
		{StatusClasse: "9xx"},
		{StatusClasse: "quatro"},
	} {
		_, err := dto.ParaFiltro()
		require.ErrorIs(t, err, ErrFiltroInvalido, "dto %+v deveria recusar", dto)
	}

	// Propagação: Acesso mantém; Auditoria e Erros zeram.
	filtro, err = LogsFiltroRequestDto{Metodo: "POST", StatusClasse: "2xx"}.ParaFiltro()
	require.NoError(t, err)

	capturado := &consultorFake{}
	svc := NewService(capturado, ComProvedorOpcoes(novoProvedorFake()))

	_, err = svc.Acesso(ctxComRecorte(permsDeOrganization), filtro, paginaPadrao)
	require.NoError(t, err)
	assert.Equal(t, "POST", capturado.filtros[0].Metodo)
	assert.Equal(t, 2, capturado.filtros[0].ClasseStatus)

	_, err = svc.Auditoria(ctxComRecorte(permsDeOrganization), filtro, paginaPadrao)
	require.NoError(t, err)
	assert.Empty(t, capturado.filtros[1].Metodo, "auditoria não filtra por método")
	assert.Zero(t, capturado.filtros[1].ClasseStatus, "auditoria não filtra por classe de status")

	_, err = svc.Erros(ctxComRecorte(permsDeOrganization), filtro, paginaPadrao)
	require.NoError(t, err)
	assert.Empty(t, capturado.filtros[2].Metodo)
	assert.Zero(t, capturado.filtros[2].ClasseStatus)
}
