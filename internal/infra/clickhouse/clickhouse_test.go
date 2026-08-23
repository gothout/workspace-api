package clickhouse

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"workspace-api/internal/pkg/config"
)

// TestConnectDesabilitadoDevolveConexaoNula prova o contrato degradável sem
// rede alguma: enabled=false → nil SEM erro e sem pânico.
func TestConnectDesabilitadoDevolveConexaoNula(t *testing.T) {
	require.Nil(t, Connect(config.ClickHouseConfig{Enabled: false}))
}

// TestConnectInacessivelDevolveNulo: servidor configurado mas fora do ar no
// boot = [DEGRADADO] com conexão nula — NUNCA erro que derrube o boot, e a
// degradação é rápida (timeout curto de dial).
func TestConnectInacessivelDevolveNulo(t *testing.T) {
	inicio := time.Now()
	conn := Connect(config.ClickHouseConfig{Enabled: true, Host: "127.0.0.1", Port: portaMorta()})
	duracao := time.Since(inicio)
	require.Nil(t, conn)
	require.Less(t, duracao, 10*time.Second)
}

// TestCicloDoSingleton é O único teste que exercita InitClickhouse/Use/Close
// (sync.Once não se desfaz entre casos): boot sem config erra; boot com
// clickhouse desabilitado devolve writer nil SEM erro (degradado); Use segue
// nil,nil; Close/Reset encerram limpos.
func TestCicloDoSingleton(t *testing.T) {
	ResetarParaTeste()

	_, err := InitClickhouse()
	require.Error(t, err) // config não inicializada

	_, err = Use()
	require.Error(t, err) // ainda não iniciado com sucesso

	iniciarConfigDeTeste(t, config.ClickHouseConfig{Enabled: false})
	ResetarParaTeste() // nova tentativa de boot = Once novo (lição F1)
	escritor, err := InitClickhouse()
	require.NoError(t, err)
	require.Nil(t, escritor) // degradado: consumidor trata a ausência

	deNovo, err := Use()
	require.NoError(t, err)
	require.Nil(t, deNovo)

	Close()
	ResetarParaTeste()
	_, err = Use()
	require.Error(t, err)
}

// iniciarConfigDeTeste grava uma config válida mínima com a seção clickhouse
// informada e monta o singleton da pkg/config para o boot do teste.
func iniciarConfigDeTeste(t *testing.T, chCfg config.ClickHouseConfig) {
	t.Helper()
	conteudo := map[string]any{
		"app":  map[string]any{"name": "teste", "env": "dev", "version": "0", "base_domain": "localhost"},
		"server": map[string]any{
			"http": map[string]any{"port": 8081, "shutdown_timeout_sec": 5},
		},
		"security":  map[string]any{"jwt_secret": "segredo-de-teste-com-mais-de-32-bytes!!", "jwt_ttl_min": 60, "jwt_refresh_ttl_hours": 24},
		"databases": map[string]any{"postgres": map[string]any{"host": "localhost", "port": 5432, "name": "teste"}, "migrations": map[string]any{"path": "db/migrations"}, "clickhouse": chCfg},
	}
	corpo, err := json.Marshal(conteudo)
	require.NoError(t, err)
	arquivo := filepath.Join(t.TempDir(), "configs_teste.json")
	require.NoError(t, os.WriteFile(arquivo, corpo, 0o600))
	config.ResetarParaTeste()
	require.NoError(t, config.Init(arquivo))
}

// portaMorta devolve uma porta de loopback sem listener (dial recusa rápido).
func portaMorta() int {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 1 // porta reservada sem listener conhecido
	}
	porta := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	return porta
}
