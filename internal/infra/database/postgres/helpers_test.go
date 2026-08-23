package postgres

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"workspace-api/internal/pkg/config"
)

// dockerDisponivel confere o daemon antes de tentar conexão real — sem
// ambiente o teste pula, nunca reprova.
func dockerDisponivel(t *testing.T) bool {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "docker", "info", "--format", "{{.ServerVersion}}").Run() == nil
}

// subirBancoEfemero sobe um Postgres descartável e devolve a config de
// conexão já com host/porta mapeados pelo docker.
func subirBancoEfemero(t *testing.T) (context.Context, config.PostgresConfig) {
	t.Helper()
	ctx := context.Background()
	container, err := postgres.Run(ctx, "postgres:16",
		postgres.WithDatabase("workspace"),
		postgres.WithUsername("workspace"),
		postgres.WithPassword("workspace"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(90*time.Second)),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	host, err := container.Host(ctx)
	require.NoError(t, err)
	porta, err := container.MappedPort(ctx, "5432")
	require.NoError(t, err)
	return ctx, config.PostgresConfig{
		Host:    host,
		Port:    porta.Int(),
		User:    "workspace",
		Pass:    "workspace",
		Name:    "workspace",
		SslMode: "disable",
		Pool: config.PoolConfig{
			MaxOpenConns:       5,
			MaxIdleConns:       2,
			ConnMaxLifetimeMin: 5,
			ConnMaxIdleTimeMin: 2,
		},
	}
}

// configInitParaTeste grava um configs.json apontando para o banco informado
// e roda o config.Init real — é assim que o singleton do postgres enxerga a
// configuração sem estado global extra.
func configInitParaTeste(t *testing.T, pg config.PostgresConfig) {
	t.Helper()
	exemplo := map[string]any{
		"app":      map[string]any{"name": "workspace-api", "env": "teste", "version": "0.0.0", "base_domain": "localhost"},
		"server":   map[string]any{"http": map[string]any{"port": 18080, "read_timeout_sec": 15, "write_timeout_sec": 30, "idle_timeout_sec": 60, "shutdown_timeout_sec": 10, "trusted_proxy": []string{}, "cors": map[string]any{"allowed_origins": []string{}}}},
		"security": map[string]any{"jwt_secret": "segredo-de-teste-suficiente-do-postgres", "jwt_ttl_min": 60, "jwt_refresh_ttl_hours": 168},
		"databases": map[string]any{
			"postgres": map[string]any{"host": pg.Host, "port": pg.Port, "user": pg.User, "pass": pg.Pass, "name": pg.Name, "ssl_mode": "disable",
				"pool": map[string]any{"max_open_conns": pg.Pool.MaxOpenConns, "max_idle_conns": pg.Pool.MaxIdleConns, "conn_max_lifetime_min": 5, "conn_max_idle_time_min": 2}},
			"migrations": map[string]any{"path": "db/migrations", "auto_run": false, "lock_timeout_sec": 5, "statement_timeout_min": 10},
		},
	}
	conteudo, err := json.Marshal(exemplo)
	require.NoError(t, err)
	caminho := filepath.Join(t.TempDir(), "configs.json")
	require.NoError(t, os.WriteFile(caminho, conteudo, 0o600))
	require.NoError(t, config.Init(caminho))
}
