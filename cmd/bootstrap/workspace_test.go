package bootstrap

import (
	"context"
	"errors"
	"sync"
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

// TestUnicidadeSlugSobConcorrencia exercita o índice único TOTAL sob
// paralelismo: exatamente UMA goroutine cria o slug, as demais recebem
// ErrSlugEmUso. Invariante disputada — roda também sob `go test -race` na CI.
func TestUnicidadeSlugSobConcorrencia(t *testing.T) {
	if !dockerDisponivel(t) {
		t.Skip("docker indisponível — teste de integração pulado")
	}
	amb := subirAmbiente(t)

	disputante, err := orgmodel.NewOrganization(orgmodel.CreateInput{Nome: "Disputante"})
	require.NoError(t, err)
	require.NoError(t, dominioOrganizacao.NewRepository(amb.db).Criar(amb.ctx, disputante))

	svc := dominioWorkspace.NewService(dominioWorkspace.NewRepository(amb.db), nil)
	ctxDisputa := orgctx.WithOrganization(amb.ctx, disputante.UUID)

	const concorrentes = 12
	slug := "disputado"

	resultados := make(chan error, concorrentes)
	var prontos sync.WaitGroup
	prontos.Add(concorrentes)
	disparo := make(chan struct{})
	var done sync.WaitGroup
	done.Add(concorrentes)
	for i := 0; i < concorrentes; i++ {
		go func() {
			defer done.Done()
			prontos.Done() // sinaliza que está prestes a esperar o disparo
			<-disparo      // todos alinhados antes de disparar
			_, err := svc.Create(ctxDisputa, modelworkspace.CreateInput{Nome: "Disputa", Slug: slug})
			resultados <- err
		}()
	}
	prontos.Wait() // garante que todas as goroutines estão na barreira
	close(disparo)
	done.Wait()
	close(resultados)

	vitorias, conflitos := 0, 0
	for err := range resultados {
		switch {
		case err == nil:
			vitorias++
		case errors.Is(err, dominioWorkspace.ErrSlugEmUso):
			conflitos++
		default:
			t.Fatalf("erro inesperado na disputa: %v", err)
		}
	}
	assert.Equal(t, 1, vitorias, "exatamente UM create vence a disputa pelo slug")
	assert.Equal(t, concorrentes-1, conflitos, "demais recebem ErrSlugEmUso (nunca erro genérico)")
}
