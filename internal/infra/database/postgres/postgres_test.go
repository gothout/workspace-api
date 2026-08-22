package postgres

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetDBAntesDoInitDevolveErro(t *testing.T) {
	if instance != nil {
		t.Skip("singleton já inicializado por outro teste do pacote")
	}
	_, err := GetDB()
	assert.ErrorIs(t, err, ErrNaoInicializado)
	assert.Error(t, Ping(context.Background()))
}

// TestConnectContraBancoRealECicloDoSingleton cobre, num único teste: o
// Connect puro (pool + ping), e o ciclo InitPostgres → GetDB → Ping → Close.
func TestConnectContraBancoRealECicloDoSingleton(t *testing.T) {
	if !dockerDisponivel(t) {
		t.Skip("docker indisponível: teste de conexão pulado")
	}
	ctx, cfg := subirBancoEfemero(t)

	// Connect puro: sem estado global, testável isoladamente.
	db, err := Connect(ctx, cfg)
	require.NoError(t, err)
	var um int
	require.NoError(t, db.WithContext(ctx).Raw("SELECT 1").Scan(&um).Error)
	assert.Equal(t, 1, um)

	sqlPool, err := db.DB()
	require.NoError(t, err)
	assert.LessOrEqual(t, sqlPool.Stats().MaxOpenConnections, cfg.Pool.MaxOpenConns,
		"pool respeita max_open_conns da config")
	require.NoError(t, sqlPool.Close())

	// Ciclo do singleton: Init → Get → Ping → Close (LIFO do bootstrap).
	configInitParaTeste(t, cfg)
	inicializado, initErr := InitPostgres(ctx)
	require.NoError(t, initErr)

	obtido, err := GetDB()
	require.NoError(t, err)
	assert.Same(t, inicializado, obtido, "GetDB devolve sempre a mesma instância")

	require.NoError(t, Ping(ctx))

	require.NoError(t, Close())
	assert.ErrorIs(t, Ping(ctx), ErrNaoInicializado, "após Close a sonda falha explicitamente")
}
