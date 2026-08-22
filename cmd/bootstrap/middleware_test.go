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

	dominioUsuario "workspace-api/internal/identidade/domain/user"
	modeluser "workspace-api/internal/identidade/model/user"
	"workspace-api/internal/infra/database/migrations"
	"workspace-api/internal/pkg/orgctx"
)

// --- Ambiente efêmero (padrão dos testes de infra: sem docker, pula) ------

// Identificadores fixos dos cenários de integração.
var (
	orgA          = uuid.MustParse("aaaaaaaa-0000-4000-8000-00000000aa01")
	orgForasteira = uuid.MustParse("aaaaaaaa-0000-4000-8000-00000000aa09")
)

// validadorSemprePertence substitui o adaptador do workspace nos testes de
// service sobre o banco efêmero — o singleton do irmão não existe aqui, e a
// tabela de atribuição não tem FK para workspaces (vínculo por uuid, F1).
type validadorSemprePertence struct{}

func (validadorSemprePertence) Pertence(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return true, nil
}

// semearOrganization cria a organization pai de um cenário — o usuário tem FK
// para identidade_organization_organization desde a migration 0006.
func semearOrganization(t *testing.T, amb *ambienteBanco, id uuid.UUID) {
	t.Helper()
	require.NoError(t, amb.db.Exec(`INSERT INTO identidade_organization_organization (uuid, nome, status)
		VALUES (?, ?, 'ativo') ON CONFLICT (uuid) DO NOTHING`, id, "Org de teste").Error)
}

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

// Resolvedor de permissões ponta a ponta sobre o esquema migrado+semeado,
// AGORA via service do subdomínio user (F4): atribuição direta, suporte do
// admin_organization e do super_admin, ausência de vínculo e união de
// permissões — sem passar pelo singleton do processo.
func TestResolvedorPermissoesViaServiceDoUserSobreEsquemaReal(t *testing.T) {
	amb := subirAmbiente(t)
	require.NoError(t, semearPapeis(amb.ctx, amb.db))
	semearOrganization(t, amb, orgA)

	repo := dominioUsuario.NewRepository(amb.db)
	atrib := dominioUsuario.NewRepositorioAtribuicoes(amb.db)
	svc := dominioUsuario.NewService(repo, atrib, validadorSemprePertence{}, dominioUsuario.NovasCredenciaisBcrypt())

	uuidDoPapel := func(nome string) uuid.UUID {
		var idTexto string
		require.NoError(t, amb.db.Raw(`SELECT uuid::text FROM identidade_user_papel WHERE nome = ?`, nome).
			Scan(&idTexto).Error)
		return uuid.MustParse(idTexto)
	}

	criarUsuario := func(email string) uuid.UUID {
		u, err := svc.Create(orgctx.WithOrganization(amb.ctx, orgA), dominioUsuario.EntradaCriacao{
			Dados: modeluser.CreateInput{Nome: "Usuário " + email, Email: email},
			Senha: "senha-segura-123",
		})
		require.NoError(t, err)
		return u.UUID
	}

	var (
		wsA  = uuid.MustParse("bbbbbbbb-0000-4000-8000-00000000bb01")
		wsA2 = uuid.MustParse("bbbbbbbb-0000-4000-8000-00000000bb03")
	)

	operador := criarUsuario("operador@exemplo.com")
	dono := criarUsuario("dono@exemplo.com")
	super := criarUsuario("super@exemplo.com")

	ctxOrg := orgctx.WithOrganization(amb.ctx, orgA)
	_, err := svc.AtribuirPapel(ctxOrg, operador, wsA, uuidDoPapel(papelUsuarioWorkspace))
	require.NoError(t, err)
	_, err = svc.AtribuirPapel(ctxOrg, dono, wsA, uuidDoPapel(papelAdminOrganization))
	require.NoError(t, err)
	_, err = svc.AtribuirPapel(ctxOrg, super, wsA, uuidDoPapel(papelSuperAdmin))
	require.NoError(t, err)

	// Atribuição direta: permissões do papel no workspace.
	vinculo, err := svc.TemVinculo(ctxOrg, operador, wsA)
	require.NoError(t, err)
	assert.True(t, vinculo)

	permissoes, err := svc.PermissoesEfetivas(ctxOrg, operador, wsA)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"identidade:workspace:ler", "identidade:catalogo:ler"}, permissoes)

	// Suporte auditado: dono entra em OUTRO workspace da própria organization.
	vinculo, err = svc.TemVinculo(ctxOrg, dono, wsA2)
	require.NoError(t, err)
	assert.True(t, vinculo, "admin_organization atravessa a própria organization")

	permissoes, err = svc.PermissoesEfetivas(ctxOrg, dono, wsA2)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"identidade:workspace:*", "identidade:user:*",
		"identidade:organization:gerenciar_apikeys", "identidade:catalogo:ler"}, permissoes)

	// Sem vínculo e sem suporte: negativa limpa (forasteiro nem existe aqui).
	forasteiro := uuid.MustParse("cccccccc-0000-4000-8000-00000000cc03")
	vinculo, err = svc.TemVinculo(ctxOrg, forasteiro, wsA)
	require.NoError(t, err)
	assert.False(t, vinculo)

	permissoes, err = svc.PermissoesEfetivas(ctxOrg, forasteiro, wsA)
	require.NoError(t, err)
	assert.Empty(t, permissoes)

	// Escopo fail-closed: ctx sem organization recusa a query.
	_, err = svc.TemVinculo(amb.ctx, operador, wsA)
	assert.ErrorIs(t, err, orgctx.ErrEscopoAusente)
}

// Login INDISTINGUÍVEL com bcrypt real + sessão persistida com revogação por
// jti sobre o esquema migrado.
func TestAutenticacaoESessaoSobreEsquemaReal(t *testing.T) {
	amb := subirAmbiente(t)
	require.NoError(t, semearPapeis(amb.ctx, amb.db))
	semearOrganization(t, amb, orgA)
	semearOrganization(t, amb, orgForasteira)

	repo := dominioUsuario.NewRepository(amb.db)
	atrib := dominioUsuario.NewRepositorioAtribuicoes(amb.db)
	svc := dominioUsuario.NewService(repo, atrib, validadorSemprePertence{}, dominioUsuario.NovasCredenciaisBcrypt())

	ctx := orgctx.WithOrganization(amb.ctx, orgA)
	ana, err := svc.Create(ctx, dominioUsuario.EntradaCriacao{
		Dados: modeluser.CreateInput{Nome: "Ana Real", Email: "ana@real.com"},
		Senha: "senha-segura-123",
	})
	require.NoError(t, err)

	// Senha errada e usuário inexistente: MESMO sentinela.
	_, err = svc.Autenticar(ctx, "ana@real.com", "errada-de-propósito")
	assert.ErrorIs(t, err, dominioUsuario.ErrCredenciaisInvalidas)
	_, err = svc.Autenticar(ctx, "fantasma@real.com", "senha-segura-123")
	assert.ErrorIs(t, err, dominioUsuario.ErrCredenciaisInvalidas)

	// E-mail de outra organization não existe para esta.
	_, err = svc.Autenticar(orgctx.WithOrganization(amb.ctx, orgForasteira), "ana@real.com", "senha-segura-123")
	assert.ErrorIs(t, err, dominioUsuario.ErrCredenciaisInvalidas)

	ok, err := svc.Autenticar(ctx, "ana@real.com", "senha-segura-123")
	require.NoError(t, err)
	assert.Equal(t, ana.UUID, ok.UUID)

	// Sessão persistida: ativa → revogada → revogado visível até globalmente.
	expira := time.Now().UTC().Add(time.Hour)
	require.NoError(t, svc.RegistrarSessao(ctx, ana.UUID, "jti-real-1", expira))
	ativa, err := svc.SessaoAtiva(ctx, ana.UUID, "jti-real-1")
	require.NoError(t, err)
	assert.True(t, ativa)

	require.NoError(t, svc.EncerrarSessao(ctx, ana.UUID, "jti-real-1"))
	ativa, err = svc.SessaoAtiva(ctx, ana.UUID, "jti-real-1")
	require.NoError(t, err)
	assert.False(t, ativa)

	revogado, err := repo.RefreshTokenRevogado("jti-real-1")
	require.NoError(t, err)
	assert.True(t, revogado, "revogação persistida visível ao RevogadorDeRefresh do jwt")
	revogado, err = repo.RefreshTokenRevogado("nunca-emitido")
	require.NoError(t, err)
	assert.False(t, revogado, "linha ausente não é revogada — decisão é das demais validações")
}

// Inativar usuário encerra as sessões abertas dele na hora.
func TestInativacaoRevogaSessoesAbertas(t *testing.T) {
	amb := subirAmbiente(t)
	semearOrganization(t, amb, orgA)
	repo := dominioUsuario.NewRepository(amb.db)
	atrib := dominioUsuario.NewRepositorioAtribuicoes(amb.db)
	svc := dominioUsuario.NewService(repo, atrib, validadorSemprePertence{}, dominioUsuario.NovasCredenciaisBcrypt())

	ctx := orgctx.WithOrganization(amb.ctx, orgA)
	ana, err := svc.Create(ctx, dominioUsuario.EntradaCriacao{
		Dados: modeluser.CreateInput{Nome: "Bruno Real", Email: "bruno@real.com"},
		Senha: "senha-segura-123",
	})
	require.NoError(t, err)
	require.NoError(t, svc.RegistrarSessao(ctx, ana.UUID, "jti-bruno-1", time.Now().UTC().Add(time.Hour)))

	inativo := modeluser.StatusInativo
	_, err = svc.Update(ctx, ana.UUID, modeluser.UpdateInput{Status: &inativo})
	require.NoError(t, err)

	ativa, err := svc.SessaoAtiva(ctx, ana.UUID, "jti-bruno-1")
	require.NoError(t, err)
	assert.False(t, ativa, "sessão não sobrevive à conta inativa")

	// Conta inativa não autentica — e a recusa é a genérica.
	_, err = svc.Autenticar(ctx, "bruno@real.com", "senha-segura-123")
	assert.ErrorIs(t, err, dominioUsuario.ErrCredenciaisInvalidas)
}
