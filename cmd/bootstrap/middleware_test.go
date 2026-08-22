package bootstrap

import (
	"context"
	"database/sql"
	"net/url"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	testpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"gorm.io/driver/postgres"
	gormio "gorm.io/gorm"

	"workspace-api/internal/infra/database/migrations"
)

// --- Ambiente efêmero (padrão dos testes de infra: sem docker, pula) ------

var travaAmbiente sync.Mutex

func dockerDisponivel(t *testing.T) bool {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "docker", "info", "--format", "{{.ServerVersion}}").Run() == nil
}

type ambienteBanco struct {
	ctx context.Context
	db  *gormio.DB
}

// subirAmbiente aplica TODAS as migrations num Postgres descartável e devolve
// a conexão gorm — os testes usam as FUNÇÕES PURAS direto, nunca o singleton.
func subirAmbiente(t *testing.T) *ambienteBanco {
	t.Helper()
	travaAmbiente.Lock()
	t.Cleanup(travaAmbiente.Unlock)

	if !dockerDisponivel(t) {
		t.Skip("docker indisponível — teste de integração pulado")
	}
	ctx := context.Background()
	container, err := testpostgres.Run(ctx, "postgres:16",
		testpostgres.WithDatabase("workspace"),
		testpostgres.WithUsername("workspace"),
		testpostgres.WithPassword("workspace"),
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

	sqlDB, err := sql.Open("pgx", (&url.URL{
		Scheme: "postgres",
		User:   url.UserPassword("workspace", "workspace"),
		Host:   host + ":" + porta.Port(),
		Path:   "workspace",
	}).String()+"?sslmode=disable")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	fonte := fonteEfemera{db: sqlDB}
	_, err = migrations.Subir(ctx, fonte, "../../db/migrations", migrations.TimeoutsMigracao{
		LockTimeout:      5 * time.Second,
		StatementTimeout: 10 * time.Minute,
	})
	require.NoError(t, err)

	banco, err := gormio.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gormio.Config{})
	require.NoError(t, err)
	return &ambienteBanco{ctx: ctx, db: banco}
}

// fonteEfemera adapta o sql.DB do container ao contrato FonteConexao.
type fonteEfemera struct{ db *sql.DB }

func (f fonteEfemera) SQLDB() (*sql.DB, error) { return f.db, nil }
func (f fonteEfemera) NomeDatabase() string    { return "workspace" }

// --- Testes -----------------------------------------------------------------

// Seed idempotente: rodar duas vezes não duplica papéis nem permissões.
func TestSeedDosPapeisIdempotente(t *testing.T) {
	amb := subirAmbiente(t)

	require.NoError(t, semearPapeis(amb.ctx, amb.db))
	require.NoError(t, semearPapeis(amb.ctx, amb.db), "segunda execução não falha")

	var papeis int64
	require.NoError(t, amb.db.Table("identidade_user_papel").Count(&papeis).Error)
	assert.EqualValues(t, 5, papeis, "os 5 papéis seed existem exatamente uma vez")

	var permissoes int64
	require.NoError(t, amb.db.Table("identidade_user_papel_permissao").Count(&permissoes).Error)
	assert.EqualValues(t, totalPermissoesSeed(), permissoes)

	var superAdmin struct {
		Descricao string `gorm:"column:descricao"`
	}
	require.NoError(t, amb.db.Raw(`SELECT descricao FROM identidade_user_papel WHERE nome = ?`,
		papelSuperAdmin).Scan(&superAdmin).Error)
	assert.Contains(t, superAdmin.Descricao, "plataforma")
}

func totalPermissoesSeed() int64 {
	var total int64
	for _, papel := range papeisSeed {
		total += int64(len(papel.permissoes))
	}
	return total
}

// Resolvedor provisório ponta a ponta sobre o esquema migrado+semeado:
// atribuição direta, suporte do admin_organization e ausência de vínculo.
func TestResolvedorPermissoesSobreEsquemaReal(t *testing.T) {
	amb := subirAmbiente(t)
	require.NoError(t, semearPapeis(amb.ctx, amb.db))

	var (
		orgA       = uuid.MustParse("aaaaaaaa-0000-4000-8000-00000000aa01")
		wsA        = uuid.MustParse("bbbbbbbb-0000-4000-8000-00000000bb01")
		wsA2       = uuid.MustParse("bbbbbbbb-0000-4000-8000-00000000bb03")
		operador   = uuid.MustParse("cccccccc-0000-4000-8000-00000000cc01")
		dono       = uuid.MustParse("cccccccc-0000-4000-8000-00000000cc02")
		forasteiro = uuid.MustParse("cccccccc-0000-4000-8000-00000000cc03")
	)

	uuidPapel := func(nome string) uuid.UUID {
		var idTexto string
		require.NoError(t, amb.db.Raw(`SELECT uuid::text FROM identidade_user_papel WHERE nome = ?`, nome).
			Scan(&idTexto).Error)
		return uuid.MustParse(idTexto)
	}
	atribuir := func(usuario uuid.UUID, papel string, org, ws uuid.UUID) {
		require.NoError(t, amb.db.Exec(`INSERT INTO identidade_user_atribuicao
			(uuid, organization_uuid, workspace_uuid, user_uuid, papel_uuid) VALUES (?, ?, ?, ?, ?)`,
			uuid.New(), org, ws, usuario, uuidPapel(papel)).Error)
	}

	atribuir(operador, papelUsuarioWorkspace, orgA, wsA)
	atribuir(dono, papelAdminOrganization, orgA, wsA) // dono da organization A

	// Atribuição direta: permissões do papel no workspace.
	vinculo, err := temVinculoNo(amb.ctx, amb.db, operador, orgA, wsA)
	require.NoError(t, err)
	assert.True(t, vinculo)

	permissoes, err := permissoesEfetivasNo(amb.ctx, amb.db, operador, orgA, wsA)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"identidade:workspace:ler"}, permissoes)

	// Suporte auditado: dono entra em OUTRO workspace da própria organization,
	// mesmo sem atribuição lá…
	vinculo, err = temVinculoNo(amb.ctx, amb.db, dono, orgA, wsA2)
	require.NoError(t, err)
	assert.True(t, vinculo, "admin_organization atravessa a própria organization")

	permissoes, err = permissoesEfetivasNo(amb.ctx, amb.db, dono, orgA, wsA2)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"identidade:workspace:*", "identidade:user:*",
		"identidade:organization:gerenciar_apikeys"}, permissoes)

	naoTemVinculo, err := temVinculoNo(amb.ctx, amb.db, forasteiro, orgA, wsA)
	require.NoError(t, err)
	assert.False(t, naoTemVinculo, "sem atribuição e sem suporte não há vínculo")

	permissaoVazia, err := permissoesEfetivasNo(amb.ctx, amb.db, forasteiro, orgA, wsA)
	require.NoError(t, err)
	assert.Empty(t, permissaoVazia)
}
