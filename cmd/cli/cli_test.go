package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"

	"github.com/stretchr/testify/require"
	"workspace-api/internal/pkg/config"
)

func TestNovoRootCommandMontaArvoreSemExecutar(t *testing.T) {
	raiz := NovoRootCommand()
	require.NotNil(t, raiz)
	assert.Equal(t, "workspace-api", raiz.Name())

	subcomandos := map[string]bool{}
	for _, sub := range raiz.Commands() {
		if sub.Name() != "" {
			subcomandos[sub.Name()] = true
		}
	}
	assert.True(t, subcomandos["serve"], "serve deve estar montado")
	assert.True(t, subcomandos["migrate"], "migrate deve estar montado")
	assert.True(t, subcomandos["seed"], "seed deve estar montado")

	bandeira := raiz.PersistentFlags().Lookup("config")
	require.NotNil(t, bandeira)
	assert.Equal(t, "configs.json", bandeira.DefValue)

	migrate := subComando(raiz, "migrate")
	require.NotNil(t, migrate)
	nomes := map[string]bool{}
	for _, sub := range migrate.Commands() {
		nomes[sub.Name()] = true
	}
	for _, esperado := range []string{"up", "down", "goto", "force", "status", "validate", "create"} {
		assert.True(t, nomes[esperado], "migrate %s deve existir", esperado)
	}
}

func subComando(raiz *cobra.Command, nome string) *cobra.Command {
	for _, sub := range raiz.Commands() {
		if sub.Name() == nome {
			return sub
		}
	}
	return nil
}

func TestMigrateValidateSemConfigFalhaComMensagemClara(t *testing.T) {
	config.ResetarParaTeste()
	t.Cleanup(config.ResetarParaTeste)
	raiz := NovoRootCommand()
	bufferSaida := &bufferTeste{}
	bufferErro := &bufferTeste{}
	raiz.SetOut(bufferSaida)
	raiz.SetErr(bufferErro)
	raiz.SetArgs([]string{"--config", filepath.Join(t.TempDir(), "nao-existe.json"), "migrate", "validate"})

	err := raiz.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "falha ao ler configuração")
}

func TestMigrateValidateVerdeSemConexao(t *testing.T) {
	config.ResetarParaTeste()
	t.Cleanup(config.ResetarParaTeste)
	configs := escreverConfigsValidas(t)
	raiz := NovoRootCommand()
	saida := &bufferTeste{}
	raiz.SetOut(saida)
	raiz.SetErr(&bufferTeste{})
	raiz.SetArgs([]string{"--config", configs, "migrate", "validate"})

	require.NoError(t, raiz.Execute())
	assert.Contains(t, saida.String(), "migrate validate ok")
}

// O provisionamento incompleto (e-mail sem senha, ou o contrário) recusa CEDO
// — antes de ler config ou abrir banco: a config aponta para arquivo que não
// existe e mesmo assim o erro é o do provisionamento.
func TestSeedProvisionamentoIncompletoRecusaAntesDoBanco(t *testing.T) {
	config.ResetarParaTeste()
	t.Cleanup(config.ResetarParaTeste)
	casos := [][]string{
		{"seed", "--super-admin-email", "admin@plataforma.teste"},
		{"seed", "--super-admin-senha", "senha-forte-123"},
	}
	for _, args := range casos {
		raiz := NovoRootCommand()
		raiz.SetOut(&bufferTeste{})
		raiz.SetErr(&bufferTeste{})
		raiz.SetArgs(append([]string{"--config", filepath.Join(t.TempDir(), "nao-existe.json")}, args...))

		err := raiz.Execute()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--super-admin-email E --super-admin-senha juntos")
	}
}

// Flags do provisionamento montadas com padrões documentados.
func TestSeedFlagsDeProvisionamentoMontadas(t *testing.T) {
	seed := subComando(NovoRootCommand(), "seed")
	require.NotNil(t, seed)

	email := seed.Flags().Lookup("super-admin-email")
	require.NotNil(t, email)
	senha := seed.Flags().Lookup("super-admin-senha")
	require.NotNil(t, senha)
	slug := seed.Flags().Lookup("workspace-slug")
	require.NotNil(t, slug)
	assert.Equal(t, "principal", slug.DefValue)
	assert.NotEmpty(t, email.Usage)
	assert.NotEmpty(t, senha.Usage)
}

type bufferTeste struct{ conteudo string }

func (b *bufferTeste) Write(p []byte) (int, error) {
	b.conteudo += string(p)
	return len(p), nil
}

func (b *bufferTeste) String() string { return b.conteudo }

func escreverConfigsValidas(t *testing.T) string {
	t.Helper()
	conteudo := `{
	  "app": {"name": "workspace-api", "env": "teste", "version": "0.0.0", "base_domain": "localhost"},
	  "server": {"http": {"port": 18081, "read_timeout_sec": 15, "write_timeout_sec": 30, "idle_timeout_sec": 60, "shutdown_timeout_sec": 10, "trusted_proxy": [], "cors": {"allowed_origins": []}}},
	  "security": {"jwt_secret": "segredo-de-teste-do-cli-validate", "jwt_ttl_min": 60, "jwt_refresh_ttl_hours": 168},
	  "databases": {
	    "postgres": {"host": "localhost", "port": 5432, "user": "x", "pass": "x", "name": "x", "ssl_mode": "disable",
	      "pool": {"max_open_conns": 25, "max_idle_conns": 10, "conn_max_lifetime_min": 5, "conn_max_idle_time_min": 2}},
	    "migrations": {"path": "` + filepath.ToSlash(caminhoMigrationsVazio(t)) + `", "auto_run": false, "lock_timeout_sec": 5, "statement_timeout_min": 10}
	  }
	}`
	caminho := filepath.Join(t.TempDir(), "configs.json")
	require.NoError(t, os.WriteFile(caminho, []byte(conteudo), 0o600))
	return caminho
}

func caminhoMigrationsVazio(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "migrations-vazias")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	return dir
}
