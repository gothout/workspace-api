package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// --- Helpers ----------------------------------------------------------------

type fonteSQL struct {
	db   *sql.DB
	nome string
}

func (f fonteSQL) SQLDB() (*sql.DB, error) { return f.db, nil }
func (f fonteSQL) NomeDatabase() string    { return f.nome }

func escreverPar(t *testing.T, dir string, versao int, nome, up, down string) {
	t.Helper()
	corpo := func(direcao, conteudo string) string {
		return fmt.Sprintf("-- Migration %04d_%s (%s).\n%s\n", versao, nome, direcao, conteudo)
	}
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, fmt.Sprintf("%04d_%s.up.sql", versao, nome)),
		[]byte(corpo("up", up)), 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, fmt.Sprintf("%04d_%s.down.sql", versao, nome)),
		[]byte(corpo("down", down)), 0o644))
}

func diretorioTemporario(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return dir
}

// dockerDisponivel confere daemon antes de tentar container — sem ambiente,
// o teste PULA (nunca reprova por ausência de docker).
func dockerDisponivel(t *testing.T) bool {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "info", "--format", "{{.ServerVersion}}")
	return cmd.Run() == nil
}

// subirBancoEfemero sobe um Postgres descartável (postgres:16) e devolve o pool.
func subirBancoEfemero(t *testing.T) *sql.DB {
	t.Helper()
	if !dockerDisponivel(t) {
		t.Skip("docker indisponível: teste de migrations pulado")
	}
	ctx := context.Background()
	container, err := postgres.Run(ctx, "postgres:16",
		postgres.WithDatabase("workspace"),
		postgres.WithUsername("workspace"),
		postgres.WithPassword("workspace"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(90*time.Second)),
	)
	if err != nil {
		t.Skipf("postgres efêmero indisponível: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.Eventually(t, func() bool { return db.PingContext(ctx) == nil },
		30*time.Second, 500*time.Millisecond, "banco não respondeu ao ping")
	return db
}

func tabelaExiste(t *testing.T, db *sql.DB, nome string) bool {
	t.Helper()
	var existe bool
	err := db.QueryRow(
		"SELECT to_regclass($1) IS NOT NULL", "public."+nome).Scan(&existe)
	require.NoError(t, err)
	return existe
}

const (
	fixtureUpTabela   = "CREATE TABLE identidade_exemplo_tabela (uuid uuid PRIMARY KEY);"
	fixtureDownTabela = "DROP TABLE IF EXISTS identidade_exemplo_tabela;"
	fixtureUpIndice   = "CREATE INDEX identidade_exemplo_idx ON identidade_exemplo_tabela (uuid);"
	fixtureDownIndice = "DROP INDEX IF EXISTS identidade_exemplo_idx;"
)

// --- Unidade: varredura de arquivos ------------------------------------------

func TestListarParesValidosOrdenados(t *testing.T) {
	dir := diretorioTemporario(t)
	escreverPar(t, dir, 2, "identidade_b_segunda", "SELECT 2;", "SELECT -2;")
	escreverPar(t, dir, 1, "identidade_a_primeira", "SELECT 1;", "SELECT -1;")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("docs"), 0o644))

	pares, err := listarPares(dir)
	require.NoError(t, err)
	require.Len(t, pares, 2)
	assert.Equal(t, uint(1), pares[0].versao)
	assert.Equal(t, "identidade_a_primeira", pares[0].nome)
	assert.False(t, pares[0].manual)
	assert.Equal(t, uint(2), pares[1].versao)
}

func TestListarParesReprovaCasosInvalidos(t *testing.T) {
	casos := map[string]func(t *testing.T, dir string){
		"up sem down": func(t *testing.T, dir string) {
			require.NoError(t, os.WriteFile(filepath.Join(dir, "0001_identidade_so_up.up.sql"), []byte("SELECT 1;"), 0o644))
		},
		".sql fora do padrão": func(t *testing.T, dir string) {
			require.NoError(t, os.WriteFile(filepath.Join(dir, "solta.sql"), []byte("SELECT 1;"), 0o644))
		},
		"número duplicado com nomes diferentes": func(t *testing.T, dir string) {
			escreverPar(t, dir, 1, "identidade_um", "SELECT 1;", "SELECT -1;")
			require.NoError(t, os.WriteFile(filepath.Join(dir, "0001_dois.up.sql"), []byte("SELECT 1;"), 0o644))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "0001_dois.down.sql"), []byte("SELECT 1;"), 0o644))
		},
	}
	for nome, montar := range casos {
		t.Run(nome, func(t *testing.T) {
			dir := diretorioTemporario(t)
			montar(t, dir)
			_, err := listarPares(dir)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "migrations:")
		})
	}
}

func TestMarcaçãoManualDetectadaNaPrimeiraLinha(t *testing.T) {
	casos := []struct {
		nome     string
		conteudo string
		expected bool
	}{
		{"manual na primeira linha", "-- manual\nCREATE INDEX x ON y (z);", true},
		{"manual com espaço antes", "-- manual\r\nSELECT 1;", true},
		{"comentário comum não é manual", "-- cria índice\nCREATE INDEX x ON y (z);", false},
		{"sem comentário", "SELECT 1;", false},
		{"linha em branco antes da marcação: primeira linha útil vale", "\n-- manual\nSELECT 1;", true},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			assert.Equal(t, caso.expected, primeiraLinhaEhManual([]byte(caso.conteudo)))
		})
	}
}

// --- Unidade: validar sem conexão --------------------------------------------

func TestValidar(t *testing.T) {
	t.Run("diretório válido passa", func(t *testing.T) {
		dir := diretorioTemporario(t)
		escreverPar(t, dir, 1, "identidade_x_uma", fixtureUpTabela, fixtureDownTabela)
		escreverPar(t, dir, 2, "identidade_y_dois", fixtureUpIndice, fixtureDownIndice)
		assert.NoError(t, Validar(dir))
	})

	t.Run("buraco na sequência reprova", func(t *testing.T) {
		dir := diretorioTemporario(t)
		escreverPar(t, dir, 1, "identidade_x_uma", fixtureUpTabela, fixtureDownTabela)
		escreverPar(t, dir, 3, "identidade_z_tres", fixtureUpTabela, fixtureDownTabela)
		err := Validar(dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "sequência quebrada")
		assert.Contains(t, err.Error(), "0002")
	})

	t.Run("SQL só de comentários reprova", func(t *testing.T) {
		dir := diretorioTemporario(t)
		escreverPar(t, dir, 1, "identidade_vazio", "-- nada aqui", "-- também nada")
		err := Validar(dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "vazio")
	})

	t.Run("diretório inexistente reprova", func(t *testing.T) {
		err := Validar(filepath.Join(diretorioTemporario(t), "nao-existe"))
		require.Error(t, err)
	})
}

// --- Unidade: create -----------------------------------------------------------

func TestCreateGeraProximoParNoPadrao(t *testing.T) {
	dir := diretorioTemporario(t)
	escreverPar(t, dir, 1, "identidade_existente", "SELECT 1;", "SELECT -1;")

	up, down, err := Create(dir, "identidade_workspace_criar_tabela")
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(up, "0002_identidade_workspace_criar_tabela.up.sql"))
	assert.True(t, strings.HasSuffix(down, "0002_identidade_workspace_criar_tabela.down.sql"))

	conteudo, err := os.ReadFile(up)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(conteudo), "-- Migration 0002_identidade_workspace_criar_tabela (up)."))
	assert.Contains(t, string(conteudo), "Escreva o DDL daqui para baixo.")
	// Nota: o par recém-criado é um SCAFFOLD só de comentários — o Validar
	// reprova até o dev preencher o SQL (é exatamente esse o gate desejado).
}

func TestCreateReprovaDescricaoForaDoPadrao(t *testing.T) {
	dir := diretorioTemporario(t)
	descricoes := []string{
		"",
		"SemUnderscore",
		"identidade",           // sem subdominio/desc
		"identidade_workspace", // falta a descrição
		"Identidade_X_Y",
		"identidade-x-y-z",
		"identidade_workspace_", // trecho vazio no fim
	}
	for _, descricao := range descricoes {
		_, _, err := Create(dir, descricao)
		assert.Error(t, err, "descrição %q deveria ser reprovada", descricao)
	}
}

func TestCreateEmDiretorioVazioComecaNoUm(t *testing.T) {
	up, _, err := Create(diretorioTemporario(t), "identidade_org_inicio")
	require.NoError(t, err)
	assert.Contains(t, filepath.Base(up), "0001_identidade_org_inicio.up.sql")
}

// --- Integração: up → down → up em banco efêmero -------------------------------

// TestMigrationsSobemEDescem aplica TODAS as migrations reais do template em
// ciclo completo — migration cujo down não desfaz o up reprova aqui.
func TestMigrationsSobemEDescem(t *testing.T) {
	if !dockerDisponivel(t) {
		t.Skip("docker indisponível: teste de migrations pulado")
	}
	db := subirBancoEfemero(t)
	fonte := fonteSQL{db: db, nome: "workspace"}
	dir := caminhoMigrationsReais(t)

	aplicadas, err := Subir(context.Background(), fonte, dir, TimeoutsMigracao{})
	require.NoError(t, err)

	estado, err := EstadoAtual(fonte, dir, TimeoutsMigracao{})
	require.NoError(t, err)
	assert.False(t, estado.Dirty)
	total := len(estado.Linhas)
	assert.GreaterOrEqual(t, aplicadas, 0)
	for _, linha := range estado.Linhas {
		assert.NotEqual(t, "pendente-manual", linha.Situacao, "arquivo manual não deveria existir no template ainda: %04d_%s", linha.Versao, linha.Nome)
	}

	if total > 0 {
		require.NoError(t, Descer(fonte, dir, total, TimeoutsMigracao{}))
		estadoDepois, err := EstadoAtual(fonte, dir, TimeoutsMigracao{})
		require.NoError(t, err)
		assert.Equal(t, uint(0), estadoDepois.VersaoAtual)
		assert.Equal(t, total, estadoDepois.Pendentes)
	}

	_, err = Subir(context.Background(), fonte, dir, TimeoutsMigracao{})
	require.NoError(t, err)
	estadoFinal, err := EstadoAtual(fonte, dir, TimeoutsMigracao{})
	require.NoError(t, err)
	assert.Equal(t, uint(total), estadoFinal.VersaoAtual)
	assert.Zero(t, estadoFinal.Pendentes)
}

// TestCicloCompletoIgnoraArquivoManual prova: up aplica só os executáveis,
// status lista o manual como pendente-manual e o down desfaz tudo limpo.
func TestCicloCompletoIgnoraArquivoManual(t *testing.T) {
	db := subirBancoEfemero(t)
	fonte := fonteSQL{db: db, nome: "workspace"}
	dir := diretorioTemporario(t)
	escreverPar(t, dir, 1, "identidade_exemplo_tabela", fixtureUpTabela, fixtureDownTabela)
	parIndiceUp, parIndiceDown, err := Create(dir, "identidade_exemplo_indice_manual")
	require.NoError(t, err)
	rewriteComCabecalhoManual(t, parIndiceUp, fixtureUpIndice)
	rewriteComCabecalhoManual(t, parIndiceDown, fixtureDownIndice)

	// UP: tabela nasce; índice manual NÃO é aplicado.
	_, err = Subir(context.Background(), fonte, dir, TimeoutsMigracao{})
	require.NoError(t, err)
	assert.True(t, tabelaExiste(t, db, "identidade_exemplo_tabela"))
	var indiceExiste bool
	require.NoError(t, db.QueryRow("SELECT to_regclass('public.identidade_exemplo_idx') IS NOT NULL").Scan(&indiceExiste))
	assert.False(t, indiceExiste, "migration manual nunca roda programaticamente")

	estado, err := EstadoAtual(fonte, dir, TimeoutsMigracao{})
	require.NoError(t, err)
	assert.Equal(t, uint(1), estado.VersaoAtual)
	require.Len(t, estado.Linhas, 2)
	assert.Equal(t, "aplicada", estado.Linhas[0].Situacao)
	assert.Equal(t, "pendente-manual", estado.Linhas[1].Situacao)
	assert.Equal(t, 1, estado.Pendentes)

	// DOWN: tabela some; estado volta a zero.
	require.NoError(t, Descer(fonte, dir, 1, TimeoutsMigracao{}))
	assert.False(t, tabelaExiste(t, db, "identidade_exemplo_tabela"))
	estadoAposDown, err := EstadoAtual(fonte, dir, TimeoutsMigracao{})
	require.NoError(t, err)
	assert.Equal(t, uint(0), estadoAposDown.VersaoAtual)
	assert.Equal(t, 2, estadoAposDown.Pendentes)

	// UP de novo: reversibilidade do ciclo completo.
	_, err = Subir(context.Background(), fonte, dir, TimeoutsMigracao{})
	require.NoError(t, err)
	assert.True(t, tabelaExiste(t, db, "identidade_exemplo_tabela"))
}

func rewriteComCabecalhoManual(t *testing.T, caminho, sqlCorpo string) {
	t.Helper()
	existente, err := os.ReadFile(caminho)
	require.NoError(t, err)
	linhas := strings.Split(string(existente), "\n")
	linhas[0] = PrefixoManual
	conteudo := strings.Join(linhas, "\n") + "\n" + sqlCorpo + "\n"
	require.NoError(t, os.WriteFile(caminho, []byte(conteudo), 0o644))
}

func caminhoMigrationsReais(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "db", "migrations"))
	require.NoError(t, err)
	return dir
}
