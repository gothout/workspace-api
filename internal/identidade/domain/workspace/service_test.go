package workspace

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	modelworkspace "workspace-api/internal/identidade/model/workspace"
	"workspace-api/internal/pkg/orgctx"
)

// --- Dublês (regra: testes de service NUNCA passam pelo singleton) ----------

type repoFake struct {
	mu       sync.Mutex
	porUUID  map[uuid.UUID]*modelworkspace.Workspace
	slugs    map[modelworkspace.Slug]bool // simula o índice único TOTAL do banco
	erroAoSalvar error                     // sentinela injetada (23505 → ErrSlugEmUso)
}

func novoRepoFake() *repoFake {
	return &repoFake{
		porUUID: map[uuid.UUID]*modelworkspace.Workspace{},
		slugs:   map[modelworkspace.Slug]bool{},
	}
}

func (r *repoFake) Criar(ctx context.Context, w *modelworkspace.Workspace) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.erroAoSalvar != nil {
		return r.erroAoSalvar
	}
	if r.slugs[w.Slug] {
		return ErrSlugEmUso
	}
	copia := *w
	r.porUUID[w.UUID] = &copia
	r.slugs[w.Slug] = true
	return nil
}

// exigirEscopo emula o fail-closed do orgctx.ScopeOrganization no banco real:
// ctx sem organization = query falha; organization divergente = não encontra.
func exigirEscopo(ctx context.Context, registro *modelworkspace.Workspace) error {
	org := orgctx.OrganizationUUID(ctx)
	if org == uuid.Nil {
		return orgctx.ErrEscopoAusente
	}
	if registro != nil && registro.OrganizationUUID != org {
		return ErrNotFound // filtro de escopo não encontra → não vaza existência
	}
	return nil
}

func (r *repoFake) BuscarPorUUID(ctx context.Context, id uuid.UUID) (*modelworkspace.Workspace, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	w, ok := r.porUUID[id]
	if !ok {
		return nil, ErrNotFound
	}
	if err := exigirEscopo(ctx, w); err != nil {
		return nil, err
	}
	copia := *w
	return &copia, nil
}

func (r *repoFake) BuscarPorSlug(_ context.Context, slug modelworkspace.Slug) (*modelworkspace.Workspace, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, w := range r.porUUID {
		if w.Slug == slug {
			copia := *w
			return &copia, nil
		}
	}
	return nil, ErrNotFound
}

func (r *repoFake) BuscarPorUUIDGlobal(ctx context.Context, id uuid.UUID) (*modelworkspace.Workspace, error) {
	return r.BuscarPorUUID(ctx, id)
}

func (r *repoFake) Listar(_ context.Context, f modelworkspace.ListFilter) ([]modelworkspace.Workspace, int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]modelworkspace.Workspace, 0, len(r.porUUID))
	for _, w := range r.porUUID {
		if f.Status != nil && w.Status != *f.Status {
			continue
		}
		items = append(items, *w)
	}
	return items, int64(len(items)), nil
}

func (r *repoFake) Atualizar(ctx context.Context, w *modelworkspace.Workspace) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	existente, ok := r.porUUID[w.UUID]
	if !ok {
		return ErrNotFound
	}
	if err := exigirEscopo(ctx, existente); err != nil {
		return err
	}
	if r.erroAoSalvar != nil {
		return r.erroAoSalvar
	}
	copia := *w
	r.porUUID[w.UUID] = &copia
	return nil
}

func (r *repoFake) Remover(ctx context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	w, ok := r.porUUID[id]
	if !ok {
		return ErrNotFound
	}
	if err := exigirEscopo(ctx, w); err != nil {
		return err
	}
	delete(r.porUUID, id)
	delete(r.slugs, w.Slug)
	return nil
}

func (r *repoFake) SuspenderPorOrganization(_ context.Context, organizationUUID uuid.UUID) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var total int64
	for _, w := range r.porUUID {
		if w.OrganizationUUID == organizationUUID && w.Status == modelworkspace.StatusAtivo {
			w.Status = modelworkspace.StatusInativo
			total++
		}
	}
	return total, nil
}

type cacheFake struct {
	mu          sync.Mutex
	porSlug     map[string]EntradaResolucao
	orgInvalidadas []string
	slugInvalidados []string
}

func novoCacheFake() *cacheFake {
	return &cacheFake{porSlug: map[string]EntradaResolucao{}}
}

func (c *cacheFake) Buscar(_ context.Context, slug string) (*EntradaResolucao, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.porSlug[slug]; ok {
		return &e, true
	}
	return nil, false
}

func (c *cacheFake) Guardar(_ context.Context, slug string, entrada EntradaResolucao) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.porSlug[slug] = entrada
}

func (c *cacheFake) Invalidar(_ context.Context, slug string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.porSlug, slug)
	c.slugInvalidados = append(c.slugInvalidados, slug)
}

func (c *cacheFake) InvalidarOrganization(_ context.Context, organizationUUID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for slug := range c.porSlug {
		delete(c.porSlug, slug)
	}
	c.orgInvalidadas = append(c.orgInvalidadas, organizationUUID)
}

// --- Suíte -------------------------------------------------------------------

func montarServico(t *testing.T) (Service, *repoFake, *cacheFake) {
	t.Helper()
	repo := novoRepoFake()
	cache := novoCacheFake()
	return NewService(repo, cache), repo, cache
}

func ctxDaOrganizacao(id uuid.UUID) context.Context {
	return orgctx.WithOrganization(context.Background(), id)
}

func criarWorkspace(t *testing.T, svc Service, ctx context.Context, slug string) *modelworkspace.Workspace {
	t.Helper()
	w, err := svc.Create(ctx, modelworkspace.CreateInput{Nome: "Filial " + slug, Slug: slug})
	require.NoError(t, err)
	return w
}

func TestCreateValidaInvariantesPeloConstrutor(t *testing.T) {
	svc, repo, _ := montarServico(t)
	ctx := ctxDaOrganizacao(uuid.New())

	w, err := svc.Create(ctx, modelworkspace.CreateInput{Nome: "  Filial Sul  ", Slug: "filial-sul"})
	require.NoError(t, err)
	assert.Equal(t, modelworkspace.StatusAtivo, w.Status)
	assert.Equal(t, "filial-sul", w.Slug.String())
	require.Len(t, repo.porUUID, 1)

	// Escopo vem do ctx: o uuid da organization gravado NUNCA é o do corpo.
	assert.NotEqual(t, uuid.Nil, w.OrganizationUUID)

	casos := []struct {
		nome, slug string
		entrada    modelworkspace.CreateInput
		esperado   error
	}{
		{"nome curto", "outro-slug", modelworkspace.CreateInput{Nome: "a", Slug: "outro-slug"}, modelworkspace.ErrNomeInvalido},
		{"slug maiúsculo", "Ruim", modelworkspace.CreateInput{Nome: "Válido", Slug: "Ruim"}, modelworkspace.ErrSlugInvalido},
		{"www reservado", "www", modelworkspace.CreateInput{Nome: "Válido", Slug: "www"}, ErrSlugReservado},
		{"painel reservado", "painel", modelworkspace.CreateInput{Nome: "Válido", Slug: "painel"}, ErrSlugReservado},
	}
	for _, caso := range casos {
		_, err := svc.Create(ctx, caso.entrada)
		assert.ErrorIs(t, err, caso.esperado, "%s deveria recusar", caso.nome)
	}
}

func TestCreateRecusaSlugDuplicadoGlobalmente(t *testing.T) {
	svc, _, _ := montarServico(t)

	criarWorkspace(t, svc, ctxDaOrganizacao(uuid.New()), "filial-sul")

	// OUTRA organization não pode o mesmo slug — unicidade é GLOBAL.
	_, err := svc.Create(ctxDaOrganizacao(uuid.New()), modelworkspace.CreateInput{Nome: "Concorrente", Slug: "filial-sul"})
	assert.ErrorIs(t, err, ErrSlugEmUso)
}

func TestLeituraNaoVazaWorkspaceDeOutraOrganization(t *testing.T) {
	svc, _, _ := montarServico(t)
	dono := uuid.New()
	w := criarWorkspace(t, svc, ctxDaOrganizacao(dono), "filial-sul")

	visto, err := svc.Read(ctxDaOrganizacao(dono), w.UUID)
	require.NoError(t, err)
	assert.Equal(t, w.UUID, visto.UUID)

	// Sem escopo no ctx a query é RECUSADA (fail-closed do orgctx); com
	// organization alheia o filtro de escopo não encontra (não vaza existência).
	_, err = svc.Read(context.Background(), w.UUID)
	assert.ErrorIs(t, err, orgctx.ErrEscopoAusente)
	_, err = svc.Read(ctxDaOrganizacao(uuid.New()), w.UUID)
	assert.ErrorIs(t, err, ErrNotFound, "organization alheia não vaza existência")
}

func TestUpdateTraduzInputEmComportamento(t *testing.T) {
	svc, _, cache := montarServico(t)
	dono := uuid.New()
	w := criarWorkspace(t, svc, ctxDaOrganizacao(dono), "filial-sul")
	ctx := ctxDaOrganizacao(dono)

	novoNome := "Filial Sul — Centro"
	inativo := modelworkspace.StatusInativo
	atualizado, err := svc.Update(ctx, w.UUID, modelworkspace.UpdateInput{Nome: &novoNome, Status: &inativo})
	require.NoError(t, err)
	assert.Equal(t, novoNome, atualizado.Nome)
	assert.Equal(t, modelworkspace.StatusInativo, atualizado.Status)
	assert.Contains(t, cache.slugInvalidados, "filial-sul", "inativar invalida a resolução na hora")

	// Inativar de novo é invariante violada (422), nem chega ao repo.
	_, err = svc.Update(ctx, w.UUID, modelworkspace.UpdateInput{Status: &inativo})
	assert.ErrorIs(t, err, modelworkspace.ErrJaInativo)

	// PATCH não reativa: ação própria.
	statusAtivo := modelworkspace.StatusAtivo
	_, err = svc.Update(ctx, w.UUID, modelworkspace.UpdateInput{Status: &statusAtivo})
	require.NoError(t, err, "status ativo em PATCH é ignorado (sem transição)")
	visto, err := svc.Read(ctx, w.UUID)
	require.NoError(t, err)
	assert.Equal(t, modelworkspace.StatusInativo, visto.Status, "reativar NÃO acontece por PATCH")
}

func TestReativarETransicaoUnica(t *testing.T) {
	svc, _, _ := montarServico(t)
	dono := uuid.New()
	w := criarWorkspace(t, svc, ctxDaOrganizacao(dono), "filial-sul")
	ctx := ctxDaOrganizacao(dono)

	_, err := svc.Reativar(ctx, w.UUID)
	assert.ErrorIs(t, err, modelworkspace.ErrJaAtivo, "reativar ativo é recusado")

	inativo := modelworkspace.StatusInativo
	_, err = svc.Update(ctx, w.UUID, modelworkspace.UpdateInput{Status: &inativo})
	require.NoError(t, err)

	reativado, err := svc.Reativar(ctx, w.UUID)
	require.NoError(t, err)
	assert.Equal(t, modelworkspace.StatusAtivo, reativado.Status)
}

func TestDeleteRemoveESlugNaoSeLibera(t *testing.T) {
	svc, repo, cache := montarServico(t)
	dono := uuid.New()
	w := criarWorkspace(t, svc, ctxDaOrganizacao(dono), "filial-sul")
	ctx := ctxDaOrganizacao(dono)

	require.NoError(t, svc.Delete(ctx, w.UUID))
	assert.NotContains(t, repo.porUUID, w.UUID)
	assert.Contains(t, cache.slugInvalidados, "filial-sul", "remover invalida a resolução")

	assert.ErrorIs(t, svc.Delete(ctx, w.UUID), ErrNotFound)

	// O fake devolveu o slug ao pool ao remover — mas o REPO REAL mantém o
	// índice único TOTAL (teste de integração cobre o anti-takeover).
}

func TestCascataSuspendeSomenteOsWorkspacesDaOrganization(t *testing.T) {
	svc, _, cache := montarServico(t)
	orgA, orgB := uuid.New(), uuid.New()

	wA1 := criarWorkspace(t, svc, ctxDaOrganizacao(orgA), "sul-a1")
	wA2 := criarWorkspace(t, svc, ctxDaOrganizacao(orgA), "sul-a2")
	wB1 := criarWorkspace(t, svc, ctxDaOrganizacao(orgB), "norte-b1")

	suspensos, err := svc.SuspenderPorOrganization(ctxDaOrganizacao(orgA), orgA)
	require.NoError(t, err)
	assert.Equal(t, 2, suspensos)

	vistoA1, err := svc.Read(ctxDaOrganizacao(orgA), wA1.UUID)
	require.NoError(t, err)
	assert.Equal(t, modelworkspace.StatusInativo, vistoA1.Status)
	vistoA2, _ := svc.Read(ctxDaOrganizacao(orgA), wA2.UUID)
	assert.Equal(t, modelworkspace.StatusInativo, vistoA2.Status)
	vistoB, err := svc.Read(ctxDaOrganizacao(orgB), wB1.UUID)
	require.NoError(t, err)
	assert.Equal(t, modelworkspace.StatusAtivo, vistoB.Status, "workspace de outra organization não é tocado")

	// Idempotente + invalidação grosseira da organization na cascata.
	suspensos, err = svc.SuspenderPorOrganization(ctxDaOrganizacao(orgA), orgA)
	require.NoError(t, err)
	assert.Equal(t, 0, suspensos)
	assert.Contains(t, cache.orgInvalidadas, orgA.String())
}

func TestResolverPorSlugUsaCacheEValidaFormato(t *testing.T) {
	svc, _, cache := montarServico(t)
	dono := uuid.New()
	w := criarWorkspace(t, svc, ctxDaOrganizacao(dono), "filial-sul")
	ctx := context.Background()

	resolvido, err := svc.ResolverPorSlug(ctx, "filial-sul")
	require.NoError(t, err)
	assert.True(t, resolvido.Ativo())
	assert.Equal(t, dono, resolvido.OrganizationUUID)
	require.Contains(t, cache.porSlug, "filial-sul", "primeira resolução popula o cache")

	// Segunda consulta vem DO CACHE (repo esvaziado não muda o resultado).
	repoEsvaziado := novoRepoFake()
	svcSemBanco := NewService(repoEsvaziado, cache)
	doCache, err := svcSemBanco.ResolverPorSlug(ctx, "filial-sul")
	require.NoError(t, err)
	assert.Equal(t, resolvido.UUID, doCache.UUID)

	// Rótulo malformado vira ErrNotFound — não distingue existência.
	for _, rotulo := range []string{"Ruim", "", "-x", "a b"} {
		_, err := svc.ResolverPorSlug(ctx, rotulo)
		assert.ErrorIs(t, err, ErrNotFound, "rótulo %q falha fechada", rotulo)
	}

	// Inativação invalida: próxima resolução volta ao repo e reflete o estado.
	inativo := modelworkspace.StatusInativo
	_, err = svc.Update(ctxDaOrganizacao(dono), w.UUID, modelworkspace.UpdateInput{Status: &inativo})
	require.NoError(t, err)
	resolvido, err = svc.ResolverPorSlug(ctx, "filial-sul")
	require.NoError(t, err)
	assert.False(t, resolvido.Ativo(), "workspace inativo resolve com Ativo=false → middleware responde 404")
}

func TestSlugsFixosExpoeListaCompleta(t *testing.T) {
	svc, _, _ := montarServico(t)
	fixos := svc.SlugsFixos()
	assert.ElementsMatch(t, []string{"www", "api", "app", "admin", "docs", "status", "mail", "suporte", "painel"}, fixos)
	for _, fixo := range fixos {
		_, err := svc.Create(ctxDaOrganizacao(uuid.New()), modelworkspace.CreateInput{Nome: fmt.Sprintf("X %s", fixo), Slug: fixo})
		assert.ErrorIs(t, err, ErrSlugReservado, "reservado %s nunca é criável", fixo)
	}
}
