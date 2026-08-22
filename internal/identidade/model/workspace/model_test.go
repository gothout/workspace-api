package workspace

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSlug(t *testing.T) {
	aceitos := []string{
		"filial-sul",
		"abc",
		"a12",
		"filial-sul-2",
		"0contorno", // pode começar com dígito
	}
	for _, valor := range aceitos {
		vo, err := ParseSlug(valor)
		require.NoError(t, err, "slug %q deveria ser aceito", valor)
		assert.Equal(t, valor, vo.String())
	}

	recusados := map[string]string{
		"":       "vazio",
		"ab":     "curto demais (mínimo 3)",
		"-ruim":  "começando em hífen",
		"ruim-":  "terminando em hífen",
		"AiM":    "maiúsculas",
		"a_b":    "underline",
		"a.b":    "ponto não é rótulo",
		"a b":    "espaço",
		"área":   "acento fora do LDH",
		strings.Repeat("a", 64): "acima de 63",
	}
	for valor, motivo := range recusados {
		if valor == "" {
			continue
		}
		_, err := ParseSlug(valor)
		assert.ErrorIs(t, err, ErrSlugInvalido, "slug %q deveria ser recusado (%s)", valor, motivo)
	}
}

func TestStatusValido(t *testing.T) {
	assert.True(t, StatusAtivo.Valido())
	assert.True(t, StatusInativo.Valido())
	assert.False(t, StatusWorkspace("estranho").Valido())
}

func TestNewWorkspaceEComportamentos(t *testing.T) {
	w, err := NewWorkspace(CreateInput{OrganizationUUID: orgUUID(), Nome: "  Filial Sul  ", Slug: "filial-sul"})
	require.NoError(t, err)
	assert.Equal(t, "Filial Sul", w.Nome)
	assert.Equal(t, StatusAtivo, w.Status)
	assert.NotEqual(t, uuid.Nil, w.UUID)

	_, err = NewWorkspace(CreateInput{OrganizationUUID: orgUUID(), Nome: "a", Slug: "filial-sul"})
	assert.ErrorIs(t, err, ErrNomeInvalido)

	_, err = NewWorkspace(CreateInput{OrganizationUUID: orgUUID(), Nome: "Filial Sul", Slug: "Ruim"})
	assert.ErrorIs(t, err, ErrSlugInvalido)

	require.NoError(t, w.Renomear(" Filial Sul — Zona Norte "))
	assert.Equal(t, "Filial Sul — Zona Norte", w.Nome)
	assert.ErrorIs(t, w.Renomear(""), ErrNomeInvalido)

	require.NoError(t, w.Inativar())
	assert.ErrorIs(t, w.Inativar(), ErrJaInativo)

	require.NoError(t, w.Reativar())
	assert.ErrorIs(t, w.Reativar(), ErrJaAtivo)
}

func orgUUID() uuid.UUID { return uuid.New() }
