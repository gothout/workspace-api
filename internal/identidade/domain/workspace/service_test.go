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
	mu           sync.Mutex
	porUUID      map[uuid.UUID]*modelworkspace.Workspace
	slugs        map[modelworkspace.Slug]bool // simula o índice único TOTAL do banco
	erroAoSalvar error                        // sentinela injetada (23505 → ErrSlugEmUso)
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
	// Emula o fail-closed do orgctx.ScopeOrganization no banco real: INSERT
	// sem organization no ctx é RECUSADO (o erro acumulado impede o SQL).
	if orgctx.OrganizationUUID(ctx) == uuid.Nil {
		return orgctx.ErrEscopoAusente
	}
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

func (r *repoFake) Listar(ctx context.Context, f modelworkspace.ListFilter) ([]modelworkspace.Workspace, int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Emula o fail-closed do orgctx.ScopeOrganization no banco real: ctx sem
	// organization = query RECUSADA; com organization = recorte exato.
	org := orgctx.OrganizationUUID(ctx)
	if org == uuid.Nil {
		return nil, 0, orgctx.ErrEscopoAusente
	}
	items := make([]modelworkspace.Workspace, 0, len(r.porUUID))
	for _, w := range r.porUUID {
		if w.OrganizationUUID != org {
			continue
		}
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
	mu              sync.Mutex
	porSlug         map[string]EntradaResolucao
	orgInvalidadas  []string
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

func (r *repoFake) ListarOpcoes(_ context.Context, organizacaoUUID *uuid.UUID) ([]modelworkspace.Workspace, error) {
	return nil, nil
}

// --- UX4: gestão cross-tenant (filtro/criação pela plataforma) ---------------

// estadoFake implementa o contrato ResolvedorEstadoOrganization com resposta
// fixa — a vitalidade real do irmão é coisa de integração (bootstrap).
type estadoFake struct {
	existe bool
	ativa  bool
	erro   error
}

func (e estadoFake) Estado(context.Context, uuid.UUID) (bool, bool, error) {
	return e.existe, e.ativa, e.erro
}

// ctxPlataforma simula o super_admin no console master: posse EXATA de `*:*`
// SEM organization resolvida (painel.{base_domain} não tem workspace).
func ctxPlataforma() context.Context {
	return orgctx.WithPermissoes(context.Background(), []string{"*:*"})
}

// ctxDeAdminOrganizacao simula o dono do contrato: permissões de papel real,
// escopado na própria organization.
func ctxDeAdminOrganizacao(id uuid.UUID) context.Context {
	return orgctx.WithPermissoes(orgctx.WithOrganization(context.Background(), id),
		[]string{"identidade:workspace:*", "identidade:user:*", "identidade:catalogo:ler"})
}

func TestCreateCrossTenantSomentePlataforma(t *testing.T) {
	orgAlvo := uuid.New()

	casos := []struct {
		nome       string
		ctx        context.Context
		estado     ResolvedorEstadoOrganization
		esperado   error
		criou      bool
		donoEsperd uuid.UUID
	}{
		{
			nome:       "plataforma cria o primeiro workspace da organization nova",
			ctx:        ctxPlataforma(),
			estado:     estadoFake{existe: true, ativa: true},
			criou:      true,
			donoEsperd: orgAlvo,
		},
		{
			nome:     "plataforma não cria sob organization inexistente",
			ctx:      ctxPlataforma(),
			estado:   estadoFake{existe: false, ativa: false},
			esperado: ErrOrganizacaoNaoEncontrada,
		},
		{
			nome:     "plataforma não cria filho sob pai inativo",
			ctx:      ctxPlataforma(),
			estado:   estadoFake{existe: true, ativa: false},
			esperado: ErrOrganizacaoInativa,
		},
		{
			nome:     "sem o resolvedor ligado a criação cross-tenant falha FECHADA",
			ctx:      ctxPlataforma(),
			estado:   nil,
			esperado: nil, // erro interno genérico — assert separado abaixo
		},
		{
			nome:     "admin_organization apontando organization alheia é recusado sem vazar existência",
			ctx:      ctxDeAdminOrganizacao(uuid.New()),
			estado:   estadoFake{existe: true, ativa: true},
			esperado: ErrForaDoEscopo,
		},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			svc, repo, _ := montarServico(t)
			opcoes := []OpcaoServico{}
			if caso.estado != nil {
				opcoes = append(opcoes, ComEstadoOrganizacao(caso.estado))
			}
			svc = NewService(repo, novoCacheFake(), opcoes...)

			pedida := orgAlvo
			w, err := svc.Create(caso.ctx, modelworkspace.CreateInput{
				Nome: "Filial Nova", Slug: "filial-nova", OrganizationPedida: &pedida,
			})

			if caso.nome == "sem o resolvedor ligado a criação cross-tenant falha FECHADA" {
				require.Error(t, err, "fail-closed sem o contrato ligado")
				assert.Nil(t, w)
				assert.Empty(t, repo.porUUID, "nada persistido")
				return
			}
			if caso.esperado != nil {
				assert.ErrorIs(t, err, caso.esperado)
				assert.Empty(t, repo.porUUID, "recusa NUNCA persiste")
				return
			}
			require.NoError(t, err)
			require.True(t, caso.criou)
			assert.Equal(t, caso.donoEsperd, w.OrganizationUUID, "workspace nasce na organization PEDIDA")
		})
	}
}

func TestCreateSemPedidoExplicitoMantemEscopoDoCtx(t *testing.T) {
	svc, _, _ := montarServico(t)
	dono := uuid.New()

	// Sem organization_pedida o comportamento é o de sempre — mesmo para quem
	// seria plataforma: o escopo vem do ctx.
	w, err := svc.Create(ctxPlataforma(), modelworkspace.CreateInput{Nome: "Do Ctx", Slug: "do-ctx"})
	assert.ErrorIs(t, err, orgctx.ErrEscopoAusente, "plataforma sem organization no ctx não cria por acidente")

	w, err = svc.Create(ctxDaOrganizacao(dono), modelworkspace.CreateInput{Nome: "Do Ctx", Slug: "do-ctx"})
	require.NoError(t, err)
	assert.Equal(t, dono, w.OrganizationUUID)

	// Apontar a PRÓPRIA organization é aceito (equivalente ao escopo).
	pedida := dono
	mesmo, err := svc.Create(ctxDaOrganizacao(dono), modelworkspace.CreateInput{
		Nome: "Própria", Slug: "propria-explicita", OrganizationPedida: &pedida,
	})
	require.NoError(t, err)
	assert.Equal(t, dono, mesmo.OrganizationUUID)

	// Unicidade global segue valendo para criação cross-tenant: mesmo slug,
	// organization diferente — o índice é GLOBAL e recusa.
	repo2 := novoRepoFake()
	svc2 := NewService(repo2, novoCacheFake())
	_, err = svc2.Create(ctxDaOrganizacao(uuid.New()), modelworkspace.CreateInput{Nome: "Dono Um", Slug: "slug-global"})
	require.NoError(t, err)
	svc2 = NewService(repo2, novoCacheFake(), ComEstadoOrganizacao(estadoFake{existe: true, ativa: true}))
	pedida2 := uuid.New()
	_, err = svc2.Create(ctxPlataforma(), modelworkspace.CreateInput{Nome: "X Dois", Slug: "slug-global", OrganizationPedida: &pedida2})
	assert.ErrorIs(t, err, ErrSlugEmUso, "cross-tenant não escapa da unicidade GLOBAL do slug")
}

func TestListComFiltroCrossTenant(t *testing.T) {
	orgB := uuid.New()

	novoCenario := func(t *testing.T) (Service, *repoFake, uuid.UUID) {
		t.Helper()
		repo := novoRepoFake()
		svc := NewService(repo, novoCacheFake())
		donoA := uuid.New()
		for _, slug := range []string{"a-sul", "a-norte"} {
			_, err := svc.Create(ctxDaOrganizacao(donoA), modelworkspace.CreateInput{Nome: "A " + slug, Slug: slug})
			require.NoError(t, err)
		}
		for _, slug := range []string{"b-centro"} {
			_, err := svc.Create(ctxDaOrganizacao(orgB), modelworkspace.CreateInput{Nome: "B " + slug, Slug: slug})
			require.NoError(t, err)
		}
		return svc, repo, donoA
	}

	filtro := func(org uuid.UUID) modelworkspace.ListFilter {
		return modelworkspace.ListFilter{OrganizationUUID: &org}
	}

	t.Run("plataforma lista qualquer organization pelo filtro — mesmo sem escopo no ctx", func(t *testing.T) {
		svc, _, _ := novoCenario(t)
		itens, total, err := svc.List(ctxPlataforma(), filtro(orgB))
		require.NoError(t, err)
		assert.EqualValues(t, 1, total)
		require.Len(t, itens, 1)
		assert.Equal(t, orgB, itens[0].OrganizationUUID)
	})

	t.Run("sem filtro a plataforma segue fail-closed (comportamento atual)", func(t *testing.T) {
		svc, _, _ := novoCenario(t)
		_, _, err := svc.List(ctxPlataforma(), modelworkspace.ListFilter{})
		assert.ErrorIs(t, err, orgctx.ErrEscopoAusente)
	})

	t.Run("organization lista a própria pelo filtro", func(t *testing.T) {
		svc, _, donoA := novoCenario(t)
		itens, total, err := svc.List(ctxDeAdminOrganizacao(donoA), filtro(donoA))
		require.NoError(t, err)
		assert.EqualValues(t, 2, total)
		for _, w := range itens {
			assert.Equal(t, donoA, w.OrganizationUUID)
		}
	})

	t.Run("organization apontando alheia recebe fora_do_escopo sem vazar existência", func(t *testing.T) {
		svc, _, donoA := novoCenario(t)
		_, _, err := svc.List(ctxDeAdminOrganizacao(donoA), filtro(orgB))
		assert.ErrorIs(t, err, ErrForaDoEscopo)

		// Alheia INEXISTENTE recebe a mesma resposta (não distingue).
		_, _, err = svc.List(ctxDeAdminOrganizacao(donoA), filtro(uuid.New()))
		assert.ErrorIs(t, err, ErrForaDoEscopo)
	})
}
