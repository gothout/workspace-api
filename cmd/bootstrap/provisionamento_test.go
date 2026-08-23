package bootstrap

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gormio "gorm.io/gorm"

	dominioUsuario "workspace-api/internal/identidade/domain/user"
	dominioWorkspace "workspace-api/internal/identidade/domain/workspace"
	modeluser "workspace-api/internal/identidade/model/user"
	modelworkspace "workspace-api/internal/identidade/model/workspace"
	"workspace-api/internal/pkg/orgctx"
)

// Montagem idêntica à do provisionarBootstrap: construtores PUROS dos
// subdomínios sobre o banco efêmero — nunca os singletons do processo.
func novoServicoUsuarioSeed(db *gormio.DB) dominioUsuario.Service {
	repoUsuario := dominioUsuario.NewRepository(db)
	return dominioUsuario.NewService(
		repoUsuario,
		dominioUsuario.NewRepositorioAtribuicoes(db),
		validadorWorkspacesSeed{repo: dominioWorkspace.NewRepository(db)},
		dominioUsuario.NovasCredenciaisBcrypt())
}

// TestProvisionamentoValidaEntradasAntesDoBanco prova que falha de FORMATO
// (slug, e-mail, senha) morre nos VOs do modelo ANTES de qualquer acesso ao
// banco — db nil nunca é dereferenciado. Roda sem docker.
func TestProvisionamentoValidaEntradasAntesDoBanco(t *testing.T) {
	casos := []struct {
		nome string
		prov Provisionamento
		erro string
	}{
		{
			nome: "slug fora do formato DNS",
			prov: Provisionamento{EmailSuperAdmin: "admin@plataforma.teste", SenhaSuperAdmin: "senha-forte-123", SlugWorkspace: "Slug Invalido!"},
			erro: "slug do workspace inicial inválido",
		},
		{
			nome: "e-mail inválido",
			prov: Provisionamento{EmailSuperAdmin: "sem-arroba", SenhaSuperAdmin: "senha-forte-123"},
			erro: "e-mail do super_admin inválido",
		},
		{
			nome: "senha abaixo da política",
			prov: Provisionamento{EmailSuperAdmin: "admin@plataforma.teste", SenhaSuperAdmin: "curta"},
			erro: "senha fora da política",
		},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			err := provisionarBootstrap(context.Background(), nil, caso.prov)
			require.Error(t, err)
			assert.Contains(t, err.Error(), caso.erro)
		})
	}
}

// Ponta a ponta sobre o esquema migrado+semeado: o provisionamento cria o
// workspace inicial, o primeiro super_admin (hash bcrypt real) e a atribuição
// — e a SEGUNDA execução é idempotente (nenhuma linha nova). Login e vínculo
// via service provam que o template sobe ponta a ponta sem insert manual.
func TestProvisionamentoBootstrapIdempotente(t *testing.T) {
	if !dockerDisponivel(t) {
		t.Skip("docker indisponível — teste de integração pulado")
	}
	amb := subirAmbiente(t)
	require.NoError(t, semearPapeis(amb.ctx, amb.db))
	require.NoError(t, semearOrganizacaoRaiz(amb.ctx, amb.db))

	prov := Provisionamento{
		EmailSuperAdmin: "admin@plataforma.teste",
		SenhaSuperAdmin: "senha-forte-123",
		SlugWorkspace:   "principal",
	}
	require.NoError(t, provisionarBootstrap(amb.ctx, amb.db, prov))
	require.NoError(t, provisionarBootstrap(amb.ctx, amb.db, prov), "segunda execução é idempotente")

	ctxRaiz := orgctx.WithOrganization(amb.ctx, uuidOrganizacaoRaiz())

	// Workspace inicial: um só, ativo, na organization raiz.
	var workspaces int64
	require.NoError(t, amb.db.Table("identidade_workspace_workspace").Count(&workspaces).Error)
	assert.EqualValues(t, 1, workspaces)

	servicoWorkspace := dominioWorkspace.NewService(dominioWorkspace.NewRepository(amb.db), nil)
	resolvido, err := servicoWorkspace.ResolverPorSlug(ctxRaiz, "principal")
	require.NoError(t, err)
	assert.True(t, resolvido.Ativo())
	assert.Equal(t, uuidOrganizacaoRaiz(), resolvido.OrganizationUUID)
	wUUID := resolvido.UUID

	// Super_admin: um só, hash bcrypt que compara com a senha semeada.
	var usuarios int64
	require.NoError(t, amb.db.Table("identidade_user_user").Count(&usuarios).Error)
	assert.EqualValues(t, 1, usuarios)

	repoUsuario := dominioUsuario.NewRepository(amb.db)
	servicoUsuario := novoServicoUsuarioSeed(amb.db)
	u, err := repoUsuario.BuscarPorEmail(ctxRaiz, modeluser.Email(prov.EmailSuperAdmin))
	require.NoError(t, err)
	assert.Equal(t, nomeSuperAdminSeed, u.Nome)
	assert.Equal(t, uuidOrganizacaoRaiz(), u.OrganizationUUID)
	assert.True(t, u.Autenticavel())

	credenciais := dominioUsuario.NovasCredenciaisBcrypt()
	assert.True(t, credenciais.Comparar(u.SenhaHash, prov.SenhaSuperAdmin), "hash semeado confere com a senha informada")
	assert.False(t, credenciais.Comparar(u.SenhaHash, "senha-errada"))

	// Atribuição super_admin × workspace inicial: exatamente uma, viva.
	atribuicoes, err := servicoUsuario.Atribuicoes(ctxRaiz, u.UUID)
	require.NoError(t, err)
	require.Len(t, atribuicoes, 1)
	assert.Equal(t, papelSuperAdmin, atribuicoes[0].PapelNome)
	assert.Equal(t, wUUID, atribuicoes[0].WorkspaceUUID)

	// Login via service: a credencial semeada autentica de verdade.
	logado, err := servicoUsuario.Autenticar(ctxRaiz, prov.EmailSuperAdmin, prov.SenhaSuperAdmin)
	require.NoError(t, err)
	assert.Equal(t, u.UUID, logado.UUID)

	// Vínculo direto no workspace inicial: ResolveWorkspace aceita o par.
	vinculo, err := servicoUsuario.TemVinculo(ctxRaiz, u.UUID, wUUID)
	require.NoError(t, err)
	assert.True(t, vinculo)

	// Idempotência não alterou nada: atribuição continua única.
	var totalAtribuicoes int64
	require.NoError(t, amb.db.Table("identidade_user_atribuicao").
		Where("deleted_at IS NULL").Count(&totalAtribuicoes).Error)
	assert.EqualValues(t, 1, totalAtribuicoes)
}

// Slug já tomado por OUTRA organization recusa o provisionamento — nunca toma
// endereço alheio (o slug é único GLOBAL). Slug reservado da plataforma é
// recusado pelo service do subdomínio (regra de LÁ, não do seed).
func TestProvisionamentoRecusaSlugProibido(t *testing.T) {
	if !dockerDisponivel(t) {
		t.Skip("docker indisponível — teste de integração pulado")
	}
	amb := subirAmbiente(t)
	require.NoError(t, semearPapeis(amb.ctx, amb.db))
	require.NoError(t, semearOrganizacaoRaiz(amb.ctx, amb.db))

	// Reservado ("painel" é endereço fixo do console master).
	err := provisionarBootstrap(amb.ctx, amb.db, Provisionamento{
		EmailSuperAdmin: "admin@plataforma.teste", SenhaSuperAdmin: "senha-forte-123", SlugWorkspace: "painel",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, dominioWorkspace.ErrSlugReservado)

	// Tomado por outra tenant.
	semearOrganization(t, amb, orgForasteira)
	_, err = dominioWorkspace.NewService(dominioWorkspace.NewRepository(amb.db), nil).Create(
		orgctx.WithOrganization(amb.ctx, orgForasteira),
		modelworkspace.CreateInput{Nome: "Tomado", Slug: "tomado"})
	require.NoError(t, err)

	err = provisionarBootstrap(amb.ctx, amb.db, Provisionamento{
		EmailSuperAdmin: "admin@plataforma.teste", SenhaSuperAdmin: "senha-forte-123", SlugWorkspace: "tomado",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "já pertence a outra organization")

	// Nenhum usuário foi criado em nenhum dos caminhos recusados.
	var usuarios int64
	require.NoError(t, amb.db.Table("identidade_user_user").Count(&usuarios).Error)
	assert.Zero(t, usuarios, "recusa do slug aborta antes de criar o super_admin")
}

// NovoProvisionamento: campos incompletos recusam cedo, no CLI.
func TestNovoProvisionamentoExigeEmailESenhaJuntos(t *testing.T) {
	prov, err := NovoProvisionamento("", "", "")
	require.NoError(t, err)
	assert.Nil(t, prov, "sem campos = seed simples")

	_, err = NovoProvisionamento("admin@plataforma.teste", "", "")
	require.Error(t, err)

	_, err = NovoProvisionamento("", "senha-forte-123", "principal")
	require.Error(t, err)

	prov, err = NovoProvisionamento("admin@plataforma.teste", "senha-forte-123", "")
	require.NoError(t, err)
	require.NotNil(t, prov)
	assert.Equal(t, SlugPadraoProvisionamento, prov.SlugWorkspace, "slug vazio herda o padrão")
}
