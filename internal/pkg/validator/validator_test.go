package validator

import (
	"testing"

	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type dtoSlug struct {
	Slug string `binding:"omitempty,slugdns"`
}

func validar(t *testing.T, valor any) error {
	t.Helper()
	engine, ok := binding.Validator.Engine().(*validator.Validate)
	require.True(t, ok)
	return engine.Struct(valor)
}

func TestRegistrarEValidaSlugDNS(t *testing.T) {
	require.NoError(t, Registrar())
	assert.True(t, Registrado())
	// Idempotente: segunda chamada não duplica nem erra.
	require.NoError(t, Registrar())

	casos := []struct {
		slug   string
		valido bool
	}{
		{"filial-sul", true},
		{"a1", false}, // menos de 3 caracteres
		{"abc", true},
		{"-abc", false}, // hífen nas pontas
		{"abc-", false},
		{"Abc", false}, // maiúscula
		{"ab.c", false},
		{string(make([]byte, 64)), false}, // acima de 63
	}
	for _, caso := range casos {
		err := validar(t, dtoSlug{Slug: caso.slug})
		if caso.valido {
			assert.NoError(t, err, "slug %q deveria ser válido", caso.slug)
		} else {
			assert.Error(t, err, "slug %q deveria ser recusado", caso.slug)
		}
	}
}

func TestTagOmitidaNaoDisparaValidacao(t *testing.T) {
	require.NoError(t, Registrar())
	assert.NoError(t, validar(t, dtoSlug{Slug: ""}))
}
