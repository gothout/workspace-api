package bootstrap

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dominioOrganizacao "workspace-api/internal/identidade/domain/organization"
	dominioWorkspace "workspace-api/internal/identidade/domain/workspace"
	orgmodel "workspace-api/internal/identidade/model/organization"
	modelworkspace "workspace-api/internal/identidade/model/workspace"
	"workspace-api/internal/pkg/orgctx"
)

// Fluxo do subdomínio workspace sobre o esquema migrado usando FUNÇÕES PURAS
// (nada de singleton — o único teste que boota os contêineres neste pacote é
// TestSubdominioOrganizationPontaAPonta, por causa do sync.Once por
// processo): CRUD com escopo por organization, unicidade GLOBAL do slug,
// reservados, remoção com índice único TOTAL (anti-takeover) e cascata.
func TestFluxoWorkspaceFuncoesPuras(t *testing.T) {
	if !dockerDisponivel(t) {
		t.Skip("docker indisponível — teste de integração pulado")
	}
	amb := subirAmbiente(t)

	orgRepo := dominioOrganizacao.NewRepository(amb.db)
	wsSvc := dominioWorkspace.NewService(dominioWorkspace.NewRepository(amb.db), nil)
	ctxFundo := context.Background()

	novaOrg := func(nome string) *orgmodel.Organization {
		o, err := orgmodel.NewOrganization(orgmodel.CreateInput{Nome: nome})
		require.NoError(t, err)
		require.NoError(t, orgRepo.Criar(ctxFundo, o))
		return o
	}
	orgA := novaOrg("Parceiro A")
	ctxA := orgctx.WithOrganization(amb.ctx, orgA.UUID)
	ctxB := orgctx.WithOrganization(amb.ctx, novaOrg("Concorrente B").UUID)
	ctxAlheia := orgctx.WithOrganization(amb.ctx, uuid.New())

	// --- CRUD com escopo --------------------------------------------------------
	wsA1, err := wsSvc.Create(ctxA, modelworkspace.CreateInput{Nome: "Filial Sul", Slug: "filial-sul"})
	require.NoError(t, err)
	assert.Equal(t, orgA.UUID, wsA1.OrganizationUUID, "escopo vem do ctx, nunca do corpo")

	visto, err := wsSvc.Read(ctxA, wsA1.UUID)
	require.NoError(t, err)
	assert.Equal(t, "filial-sul", visto.Slug.String())

	listados, total, err := wsSvc.List(ctxA, modelworkspace.ListFilter{})
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, listados, 1)

	_, err = wsSvc.Read(context.Background(), wsA1.UUID)
	assert.ErrorIs(t, err, orgctx.ErrEscopoAusente, "sem escopo a query é RECUSADA (fail-closed)")
	_, err = wsSvc.Read(ctxAlheia, wsA1.UUID)
	assert.ErrorIs(t, err, dominioWorkspace.ErrNotFound, "organization alheia não vaza existência")

	// --- Reservados + unicidade GLOBAL ------------------------------------------
	for _, fixo := range []string{"www", "api", "app", "admin", "docs", "status", "mail", "suporte", "painel"} {
		_, err := wsSvc.Create(ctxA, modelworkspace.CreateInput{Nome: "Fixo", Slug: fixo})
		assert.ErrorIs(t, err, dominioWorkspace.ErrSlugReservado, "reservado %q nunca é criável", fixo)
	}

	_, err = wsSvc.Create(ctxB, modelworkspace.CreateInput{Nome: "Concorrente", Slug: "filial-sul"})
	assert.ErrorIs(t, err, dominioWorkspace.ErrSlugEmUso,
		"outra organization NÃO cria o mesmo slug — unicidade global via índice único")

	// --- Remoção: índice único TOTAL impede takeover -----------------------------
	require.NoError(t, wsSvc.Delete(ctxA, wsA1.UUID))
	_, err = wsSvc.Read(ctxA, wsA1.UUID)
	assert.ErrorIs(t, err, dominioWorkspace.ErrNotFound)

	_, err = wsSvc.Create(ctxB, modelworkspace.CreateInput{Nome: "Oportunista", Slug: "filial-sul"})
	assert.ErrorIs(t, err, dominioWorkspace.ErrSlugEmUso,
		"slug removido (soft delete) NÃO se libera — anti-takeover")

	// --- Cascata REAL: inativar a organization suspende os workspaces dela --------
	wsA2, err := wsSvc.Create(ctxA, modelworkspace.CreateInput{Nome: "Filial Norte", Slug: "filial-norte"})
	require.NoError(t, err)

	suspensos, err := wsSvc.SuspenderPorOrganization(ctxA, orgA.UUID)
	require.NoError(t, err)
	assert.Equal(t, 1, suspensos)

	suspenso, err := wsSvc.Read(ctxA, wsA2.UUID)
	require.NoError(t, err)
	assert.Equal(t, modelworkspace.StatusInativo, suspenso.Status)

	suspensos, err = wsSvc.SuspenderPorOrganization(ctxA, orgA.UUID)
	require.NoError(t, err)
	assert.Equal(t, 0, suspensos, "cascata idempotente")
}
