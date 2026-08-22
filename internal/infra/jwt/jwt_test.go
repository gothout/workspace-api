package jwt

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConnectValidaParametros(t *testing.T) {
	manager, err := Connect("segredo-suficientemente-longo", 60, 168)
	require.NoError(t, err)
	assert.Equal(t, 60*time.Minute, manager.TTLAccess())
	assert.Equal(t, 168*time.Hour, manager.TTLRefresh())
}

func TestConnectReprovaParametrosInvalidos(t *testing.T) {
	casos := []struct {
		nome    string
		segredo string
		ttl     int
		refresh int
	}{
		{"segredo vazio", "", 60, 168},
		{"segredo curto", "curto", 60, 168},
		{"ttl zero", "segredo-suficientemente-longo", 0, 168},
		{"refresh zero", "segredo-suficientemente-longo", 60, 0},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			_, err := Connect(caso.segredo, caso.ttl, caso.refresh)
			assert.Error(t, err)
		})
	}
}

func TestGetAntesDoInitDevolveErro(t *testing.T) {
	ResetarParaTeste()
	_, err := Get()
	assert.ErrorIs(t, err, ErrNaoInicializado)
}

func TestCicloDoSingletonUmaUnicaVez(t *testing.T) {
	ResetarParaTeste()
	primeiro, err := Connect("segredo-do-teste-de-ciclo-unico", 30, 24)
	require.NoError(t, err)

	// Simula o boot: monta o singleton sem passar por config (injeção direta
	// no estado do pacote — o sync.Once real é exercido no bootstrap).
	instance = primeiro

	obtido, err := Get()
	require.NoError(t, err)
	assert.Same(t, primeiro, obtido, "Get devolve sempre a mesma instância")

	Close()
	_, err = Get()
	assert.ErrorIs(t, err, ErrNaoInicializado, "após Close o singleton não responde mais")
}
