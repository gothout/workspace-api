package licenca

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	modellicenca "workspace-api/internal/licensing/model/licenca"
	modelmodulo "workspace-api/internal/licensing/model/modulo"
	"workspace-api/internal/pkg/orgctx"
)

// --- Dublês (regra: testes de service NUNCA passam pelo singleton) ----------

type repoFake struct {
	mu    sync.Mutex
	porID map[uuid.UUID]*modellicenca.Licenca
	pares map[uuid.UUID]map[uuid.UUID]bool // organization → módulos vivos (índice único parcial)
}

func novoRepoFake() *repoFake {
	return &repoFake{
		porID: map[uuid.UUID]*modellicenca.Licenca{},
		pares: map[uuid.UUID]map[uuid.UUID]bool{},
	}
}

func (r *repoFake) Criar(_ context.Context, l *modellicenca.Licenca) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pares[l.OrganizationUUID][l.ModuloUUID] {
		return ErrJaConcedida
	}
	copia := *l
	r.porID[l.UUID] = &copia
	if r.pares[l.OrganizationUUID] == nil {
		r.pares[l.OrganizationUUID] = map[uuid.UUID]bool{}
	}
	r.pares[l.OrganizationUUID][l.ModuloUUID] = true
	return nil
}

func (r *repoFake) buscar(l *modellicenca.Licenca) *modellicenca.LicencaComModulo {
	return &modellicenca.LicencaComModulo{Licenca: *l, ModuloSlug: "fake", ModuloNome: "Fake"}
}

func (r *repoFake) BuscarPorUUID(ctx context.Context, id uuid.UUID) (*modellicenca.LicencaComModulo, error) {
	org := orgctx.OrganizationUUID(ctx)
	if org == uuid.Nil {
		return nil, orgctx.ErrEscopoAusente
	}
	return r.BuscarPorUUIDNaOrganization(ctx, org, id)
}

func (r *repoFake) BuscarPorUUIDNaOrganization(_ context.Context, organizationUUID, id uuid.UUID) (*modellicenca.LicencaComModulo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	l, ok := r.porID[id]
	if !ok || l.OrganizationUUID != organizationUUID {
		return nil, ErrNotFound
	}
	return r.buscar(l), nil
}

func (r *repoFake) Listar(_ context.Context, organizationUUID uuid.UUID) ([]modellicenca.LicencaComModulo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]modellicenca.LicencaComModulo, 0)
	for _, l := range r.porID {
		if l.OrganizationUUID == organizationUUID {
			items = append(items, *r.buscar(l))
		}
	}
	return items, nil
}

func (r *repoFake) ExisteParaModulo(_ context.Context, moduloUUID uuid.UUID) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, l := range r.porID {
		if l.ModuloUUID == moduloUUID {
			return true, nil
		}
	}
	return false, nil
}

func (r *repoFake) ExisteNaOrganization(_ context.Context, organizationUUID, moduloUUID uuid.UUID) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pares[organizationUUID][moduloUUID], nil
}

func (r *repoFake) Revogar(_ context.Context, organizationUUID, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	l, ok := r.porID[id]
	if !ok || l.OrganizationUUID != organizationUUID {
		return ErrNotFound
	}
	delete(r.porID, id)
	delete(r.pares[l.OrganizationUUID], l.ModuloUUID)
	return nil
}

type buscadorFake struct {
	porSlug map[string]*modelmodulo.Modulo
}

// invalidadorFake registra as invalidações grosseiras por organization.
type invalidadorFake struct{ orgs []string }

func (i *invalidadorFake) InvalidarOrganization(_ context.Context, organizationUUID string) {
	i.orgs = append(i.orgs, organizationUUID)
}

func novoBuscadorFake(slugs ...string) buscadorFake {
	m := map[string]*modelmodulo.Modulo{}
	for _, slug := range slugs {
		mod, err := modelmodulo.NewModulo(modelmodulo.CreateInput{Nome: "Módulo " + slug, Slug: slug})
		if err != nil {
			panic(err)
		}
		m[slug] = mod
	}
	return buscadorFake{porSlug: m}
}

func (b buscadorFake) BuscarPorSlug(_ context.Context, slug string) (*modelmodulo.Modulo, error) {
	if mod, ok := b.porSlug[slug]; ok {
		return mod, nil
	}
	return nil, ErrModuloNaoEncontrado
}

// --- Suíte -------------------------------------------------------------------

func montarServico(t *testing.T) (Service, *repoFake, buscadorFake) {
	t.Helper()
	repo := novoRepoFake()
	buscador := novoBuscadorFake("todolist")
	return NewService(repo, buscador), repo, buscador
}

func TestAtribuirResolveSlugEGravaComConcessor(t *testing.T) {
	svc, repo, _ := montarServico(t)
	alvo := uuid.New()
	concessor := uuid.New()
	ctx := orgctx.WithUser(orgctx.WithRayTrace(context.Background(), "teste"), concessor)

	l, err := svc.Atribuir(ctx, alvo, "todolist")
	require.NoError(t, err)
	assert.Equal(t, alvo, l.OrganizationUUID)
	assert.Equal(t, "todolist", l.ModuloSlug)
	require.NotNil(t, l.ConcedidaPor)
	assert.Equal(t, concessor, *l.ConcedidaPor)
	require.Len(t, repo.porID, 1)
}

func TestAtribuirRecusaModuloInexistenteEOrgVazia(t *testing.T) {
	svc, _, _ := montarServico(t)
	_, err := svc.Atribuir(context.Background(), uuid.New(), "fantasma")
	assert.ErrorIs(t, err, ErrModuloInvalido)

	_, err = svc.Atribuir(context.Background(), uuid.Nil, "todolist")
	assert.ErrorIs(t, err, ErrInvalidInput)
}

func TestAtribuirDuplicadaRecusa409(t *testing.T) {
	svc, _, _ := montarServico(t)
	alvo := uuid.New()
	ctx := orgctx.WithOrganization(context.Background(), alvo)
	_, err := svc.Atribuir(ctx, alvo, "todolist")
	require.NoError(t, err)
	_, err = svc.Atribuir(ctx, alvo, "todolist")
	assert.ErrorIs(t, err, ErrJaConcedida)
}

func TestLeituraPropriaVsAlheia(t *testing.T) {
	svc, repo, _ := montarServico(t)
	dona := uuid.New()
	alheia := uuid.New()
	ctx := orgctx.WithOrganization(orgctx.WithUser(context.Background(), uuid.New()), dona)

	l, err := svc.Atribuir(ctx, dona, "todolist")
	require.NoError(t, err)
	require.Len(t, repo.porID, 1)

	// Própria organization: passa sem exigência extra.
	itens, err := svc.Listar(ctx, dona)
	require.NoError(t, err)
	require.Len(t, itens, 1)

	// Organization ALHEIA sem permissão de plataforma: recusa — e a leitura
	// pontual não vaza existência além do 403.
	_, err = svc.Listar(ctx, alheia)
	assert.ErrorIs(t, err, ErrSemAcesso)
	_, err = svc.Read(ctx, alheia, l.UUID)
	assert.ErrorIs(t, err, ErrSemAcesso)

	// Com o curinga da plataforma nas efetivas: atravessa.
	super := orgctx.WithPermissoes(ctx, []string{"*:*"})
	itens, err = svc.Listar(super, alheia)
	require.NoError(t, err)
	assert.Empty(t, itens, "atravessa a fronteira, mas a alheia não tem licenças")

	// Revogação é exclusiva do super_admin na rota; o service valida apenas
	// o par (organization alvo, licença): fora dele = NotFound.
	assert.ErrorIs(t, svc.Revogar(ctx, alheia, l.UUID), ErrNotFound)
	require.NoError(t, svc.Revogar(ctx, dona, l.UUID))
	assert.Empty(t, repo.porID)
}

func TestExisteVivaValidaEntrada(t *testing.T) {
	svc, _, _ := montarServico(t)
	// Escopo incompleto é entrada inválida — nunca consulta aberta.
	_, err := svc.ExisteViva(context.Background(), uuid.Nil, uuid.New())
	assert.ErrorIs(t, err, ErrInvalidInput)
}

func TestEscritasInvalidamOCacheDaOrganization(t *testing.T) {
	repo := novoRepoFake()
	invalidador := &invalidadorFake{}
	svc := NewService(repo, novoBuscadorFake("todolist"), ComInvalidadorAcessos(invalidador))

	alvo := uuid.New()
	ctx := orgctx.WithOrganization(context.Background(), alvo)
	_, err := svc.Atribuir(ctx, alvo, "todolist")
	require.NoError(t, err)
	require.Len(t, invalidador.orgs, 1)
	assert.Equal(t, alvo.String(), invalidador.orgs[0], "conceder invalida app:{org}:*")

	// Acha a licença criada pelo fake para revogar.
	var licencaUUID uuid.UUID
	for id := range repo.porID {
		licencaUUID = id
	}
	require.NoError(t, svc.Revogar(ctx, alvo, licencaUUID))
	require.Len(t, invalidador.orgs, 2)
	assert.Equal(t, alvo.String(), invalidador.orgs[1], "revogar invalida app:{org}:*")
}
