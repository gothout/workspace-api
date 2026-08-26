package bootstrap

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	aplicacaologs "workspace-api/internal/identidade/application/logs"
	dominioOrganizacao "workspace-api/internal/identidade/domain/organization"
	dominioUsuario "workspace-api/internal/identidade/domain/user"
	dominioWorkspace "workspace-api/internal/identidade/domain/workspace"
	modeluser "workspace-api/internal/identidade/model/user"
	modelworkspace "workspace-api/internal/identidade/model/workspace"
	"workspace-api/internal/pkg/orgctx"
)

// --- Cenário UX3 (issue #30): opções de filtro recortadas --------------------

// TestOpcoesFiltroSobreEsquemaReal: o provedor ligado aos repositórios PUROS
// dos três subdomínios devolve as listas na granularidade de CADA recorte —
// plataforma vê tudo, organization fica na própria, workspace só com quem tem
// atribuição nele — sobre postgres efêmero migrado e semeado.
func TestOpcoesFiltroSobreEsquemaReal(t *testing.T) {
	if !dockerDisponivel(t) {
		t.Skip("docker indisponível — teste de integração pulado")
	}
	amb := subirAmbiente(t)
	require.NoError(t, semearPapeis(amb.ctx, amb.db))
	semearOrganization(t, amb, orgA)
	semearOrganization(t, amb, orgForasteira)

	// Organizations extras com nomes ordenáveis via SQL cru (mesma porta da
	// semeadura padrão do pacote; uuids fixos = idempotência).
	for _, extra := range []struct {
		id   uuid.UUID
		nome string
	}{
		{uuid.MustParse("aaaaaaaa-0000-4000-8000-00000000aa02"), "Org Alfa"},
		{uuid.MustParse("aaaaaaaa-0000-4000-8000-00000000aa03"), "Org Beta"},
	} {
		require.NoError(t, amb.db.Exec(`INSERT INTO identidade_organization_organization (uuid, nome, status)
			VALUES (?, ?, 'ativo') ON CONFLICT (uuid) DO NOTHING`, extra.id, extra.nome).Error)
	}

	svcWorkspace := dominioWorkspace.NewService(dominioWorkspace.NewRepository(amb.db), nil)
	for _, slug := range []string{"matriz", "filial-sul"} {
		_, err := svcWorkspace.Create(orgctx.WithOrganization(amb.ctx, orgA), modelworkspace.CreateInput{
			Nome: slug, Slug: slug,
		})
		require.NoError(t, err)
	}

	repoUsuario := dominioUsuario.NewRepository(amb.db)
	svcUsuario := dominioUsuario.NewService(
		repoUsuario, dominioUsuario.NewRepositorioAtribuicoes(amb.db),
		validadorSemprePertence{}, dominioUsuario.NovasCredenciaisBcrypt())
	ana, err := svcUsuario.Create(orgctx.WithOrganization(amb.ctx, orgA), dominioUsuario.EntradaCriacao{
		Dados: modeluser.CreateInput{Nome: "Ana Lima", Email: "ana@opcoes.com"}, Senha: "senha-segura-123",
	})
	require.NoError(t, err)
	bruno, err := svcUsuario.Create(orgctx.WithOrganization(amb.ctx, orgForasteira), dominioUsuario.EntradaCriacao{
		Dados: modeluser.CreateInput{Nome: "Bruno Fora", Email: "bruno@fora.com"}, Senha: "senha-segura-123",
	})
	require.NoError(t, err)

	wsMatriz := uuid.MustParse("cccccccc-0000-4000-8000-00000000cca1")
	var idTexto string
	require.NoError(t, amb.db.Raw(`SELECT uuid::text FROM identidade_user_papel WHERE nome = ?`, papelAdminWorkspace).
		Scan(&idTexto).Error)
	require.NoError(t, amb.db.Exec(
		`INSERT INTO identidade_user_atribuicao (uuid, organization_uuid, workspace_uuid, user_uuid, papel_uuid, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, now(), now())`,
		uuid.New(), orgA, wsMatriz, ana.UUID, uuid.MustParse(idTexto)).Error)

	provedor := provedorOpcoesLogs{
		repoOrganizacao: func() dominioOrganizacao.Repository { return dominioOrganizacao.NewRepository(amb.db) },
		repoWorkspace:   func() dominioWorkspace.Repository { return dominioWorkspace.NewRepository(amb.db) },
		repoUsuario:     func() dominioUsuario.Repository { return repoUsuario },
	}
	opcoes := func(ctx context.Context) aplicacaologs.OpcoesFiltroResponseDto {
		resp, err := aplicacaologs.NewService(
			trilhasComUsuarios{}, aplicacaologs.ComProvedorOpcoes(provedor),
		).OpcoesFiltro(ctx)
		require.NoError(t, err)
		return resp
	}

	// PLATAFORMA: todas as organizations, todos os workspaces e usuários.
	ctxPlataforma := orgctx.WithPermissoes(context.Background(), []string{"*:*"})
	respPlataforma := opcoes(ctxPlataforma)
	assert.GreaterOrEqual(t, len(respPlataforma.Organizacoes), 4, "plataforma lista TODAS as organizations")
	assert.GreaterOrEqual(t, len(respPlataforma.Workspaces), 2)
	assert.Len(t, respPlataforma.Usuarios, 2)

	// ORGANIZATION (:ler_organization): só a própria nos três eixos.
	ctxOrgAmpliada := orgctx.WithPermissoes(
		orgctx.WithWorkspace(orgctx.WithOrganization(context.Background(), orgA), wsMatriz),
		[]string{aplicacaologs.PermLer, aplicacaologs.PermLerOrganization})
	respOrg := opcoes(ctxOrgAmpliada)
	require.Len(t, respOrg.Organizacoes, 1)
	assert.Equal(t, orgA.String(), respOrg.Organizacoes[0].UUID)
	assert.Equal(t, "Org de teste", respOrg.Organizacoes[0].Nome)
	require.Len(t, respOrg.Workspaces, 2, "os dois workspaces da própria org")
	assert.ElementsMatch(t, []string{"matriz", "filial-sul"}, nomesDe(respOrg.Workspaces))
	require.Len(t, respOrg.Usuarios, 1, "usuário de organization alheia não é opção")
	assert.Equal(t, "Ana Lima", respOrg.Usuarios[0].Nome)

	// WORKSPACE (:ler sem ampliação): usuários COM ATRIBUIÇÃO viva no próprio
	// workspace — Bruno nem pertence à org, e Ana só entra por ter atribuição.
	ctxWs := orgctx.WithPermissoes(
		orgctx.WithWorkspace(orgctx.WithOrganization(context.Background(), orgA), wsMatriz),
		[]string{aplicacaologs.PermLer})
	respWs := opcoes(ctxWs)
	require.Len(t, respWs.Usuarios, 1)
	assert.Equal(t, ana.UUID.String(), respWs.Usuarios[0].UUID)
	assert.NotContains(t, nomesDe(respWs.Usuarios), bruno.Nome)

	// Fail-closed: chamador sem escopo derivável não lê opção nenhuma.
	semEscopo := orgctx.WithUser(context.Background(), ana.UUID)
	semEscopo = orgctx.WithPermissoes(semEscopo, []string{aplicacaologs.PermLer})
	_, err = aplicacaologs.NewService(
		trilhasComUsuarios{}, aplicacaologs.ComProvedorOpcoes(provedor),
	).OpcoesFiltro(semEscopo)
	require.ErrorIs(t, err, aplicacaologs.ErrForaDoEscopo)
}

func nomesDe(opcoes []aplicacaologs.OpcaoDto) []string {
	nomes := make([]string, 0, len(opcoes))
	for _, o := range opcoes {
		nomes = append(nomes, o.Nome)
	}
	return nomes
}
