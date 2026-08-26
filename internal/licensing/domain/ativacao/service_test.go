package ativacao

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	modelativacao "workspace-api/internal/licensing/model/ativacao"
	modelmodulo "workspace-api/internal/licensing/model/modulo"
	"workspace-api/internal/pkg/orgctx"
)

// --- Dublês (regra: testes de service NUNCA passam pelo singleton) ----------

type repoFake struct {
	mu      sync.Mutex
	porID   map[uuid.UUID]*modelativacao.Ativacao
	pares   map[uuid.UUID]map[uuid.UUID]bool // workspace → módulos vivos
	slugs   map[string][]AplicacaoDisponivelDto
}

func novoRepoFake() *repoFake {
	return &repoFake{
		porID: map[uuid.UUID]*modelativacao.Ativacao{},
		pares: map[uuid.UUID]map[uuid.UUID]bool{},
		slugs: map[string][]AplicacaoDisponivelDto{},
	}
}

func (r *repoFake) Criar(_ context.Context, a *modelativacao.Ativacao) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pares[a.WorkspaceUUID][a.ModuloUUID] {
		return ErrJaAtivada
	}
	copia := *a
	r.porID[a.UUID] = &copia
	if r.pares[a.WorkspaceUUID] == nil {
		r.pares[a.WorkspaceUUID] = map[uuid.UUID]bool{}
	}
	r.pares[a.WorkspaceUUID][a.ModuloUUID] = true
	return nil
}

func (r *repoFake) BuscarPorUUID(_ context.Context, organizationUUID, workspaceUUID, id uuid.UUID) (*modelativacao.AtivacaoComModulo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.porID[id]
	if !ok || a.OrganizationUUID != organizationUUID || a.WorkspaceUUID != workspaceUUID {
		return nil, ErrNotFound
	}
	return &modelativacao.AtivacaoComModulo{Ativacao: *a, ModuloSlug: "fake", ModuloNome: "Fake"}, nil
}

func (r *repoFake) Listar(_ context.Context, organizationUUID, workspaceUUID uuid.UUID) ([]modelativacao.AtivacaoComModulo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]modelativacao.AtivacaoComModulo, 0)
	for _, a := range r.porID {
		if a.OrganizationUUID == organizationUUID && a.WorkspaceUUID == workspaceUUID {
			items = append(items, modelativacao.AtivacaoComModulo{Ativacao: *a, ModuloSlug: "fake", ModuloNome: "Fake"})
		}
	}
	return items, nil
}

func (r *repoFake) ListarSlugsLiberados(_ context.Context, organizationUUID, workspaceUUID uuid.UUID) ([]AplicacaoDisponivelDto, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.slugs[organizationUUID.String()+"|"+workspaceUUID.String()], nil
}

func (r *repoFake) Remover(_ context.Context, organizationUUID, workspaceUUID, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.porID[id]
	if !ok || a.OrganizationUUID != organizationUUID || a.WorkspaceUUID != workspaceUUID {
		return ErrNotFound
	}
	delete(r.porID, id)
	delete(r.pares[a.WorkspaceUUID], a.ModuloUUID)
	return nil
}

type buscadorFake struct {
	porSlug map[string]*modelmodulo.Modulo
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

type verificadorLicencasFake struct{ tem bool }

func (v verificadorLicencasFake) Existe(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return v.tem, nil
}

type validadorWorkspacesFake struct{ pertence bool }

func (v validadorWorkspacesFake) Pertence(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return v.pertence, nil
}

// invalidadorFake registra as invalidações do par (org, ws).
type invalidadorFake struct{ pares []string }

func (i *invalidadorFake) InvalidarPar(_ context.Context, organizationUUID, workspaceUUID string) {
	i.pares = append(i.pares, organizationUUID+"|"+workspaceUUID)
}

// --- Suíte -------------------------------------------------------------------

type deps struct {
	modulos    BuscadorModulos
	licencas   VerificadorLicencas
	workspaces ValidadorWorkspaces
}

func montarServico(t *testing.T, d deps) (Service, *repoFake) {
	t.Helper()
	repo := novoRepoFake()
	svc := NewService(repo, d.modulos, d.licencas, d.workspaces)
	return svc, repo
}

func ctxDaOrganizacao(id uuid.UUID) context.Context {
	return orgctx.WithOrganization(orgctx.WithUser(context.Background(), uuid.New()), id)
}

func TestAtivarValidaOTripéNaOrdem(t *testing.T) {
	org, ws := uuid.New(), uuid.New()

	t.Run("workspace fora da organization recusa primeiro", func(t *testing.T) {
		svc, _ := montarServico(t, deps{
			modulos:    novoBuscadorFake("todolist"),
			licencas:   verificadorLicencasFake{tem: true},
			workspaces: validadorWorkspacesFake{pertence: false},
		})
		_, err := svc.Ativar(ctxDaOrganizacao(org), ws, "todolist")
		assert.ErrorIs(t, err, ErrWorkspaceInvalido)
	})
	t.Run("peça faltando no boot recusa fechada", func(t *testing.T) {
		svc, _ := montarServico(t, deps{modulos: novoBuscadorFake("todolist")})
		_, err := svc.Ativar(ctxDaOrganizacao(org), ws, "todolist")
		assert.ErrorIs(t, err, ErrWorkspaceInvalido)
	})
	t.Run("módulo inexistente recusa", func(t *testing.T) {
		svc, _ := montarServico(t, deps{
			modulos:    novoBuscadorFake(),
			licencas:   verificadorLicencasFake{tem: true},
			workspaces: validadorWorkspacesFake{pertence: true},
		})
		_, err := svc.Ativar(ctxDaOrganizacao(org), ws, "fantasma")
		assert.ErrorIs(t, err, ErrModuloInvalido)
	})
	t.Run("sem licença viva recusa", func(t *testing.T) {
		svc, _ := montarServico(t, deps{
			modulos:    novoBuscadorFake("todolist"),
			licencas:   verificadorLicencasFake{},
			workspaces: validadorWorkspacesFake{pertence: true},
		})
		_, err := svc.Ativar(ctxDaOrganizacao(org), ws, "todolist")
		assert.ErrorIs(t, err, ErrSemLicenca)
	})
	t.Run("tripé completo ativa com escopo do ctx", func(t *testing.T) {
		svc, repo := montarServico(t, deps{
			modulos:    novoBuscadorFake("todolist"),
			licencas:   verificadorLicencasFake{tem: true},
			workspaces: validadorWorkspacesFake{pertence: true},
		})
		a, err := svc.Ativar(ctxDaOrganizacao(org), ws, "todolist")
		require.NoError(t, err)
		assert.Equal(t, org, a.OrganizationUUID)
		assert.Equal(t, ws, a.WorkspaceUUID)
		assert.Equal(t, "todolist", a.ModuloSlug)
		require.NotNil(t, a.AtivadoPor, "quem ativou sai do ctx")
		require.Len(t, repo.porID, 1)
	})
	t.Run("duplicada no mesmo workspace recusa 409", func(t *testing.T) {
		svc, _ := montarServico(t, deps{
			modulos:    novoBuscadorFake("todolist"),
			licencas:   verificadorLicencasFake{tem: true},
			workspaces: validadorWorkspacesFake{pertence: true},
		})
		ctx := ctxDaOrganizacao(org)
		_, err := svc.Ativar(ctx, ws, "todolist")
		require.NoError(t, err)
		_, err = svc.Ativar(ctx, ws, "todolist")
		assert.ErrorIs(t, err, ErrJaAtivada)
	})
}

func TestDesativarRemoveEConfereParAlvo(t *testing.T) {
	repo := novoRepoFake()
	invalidador := &invalidadorFake{}
	svc := NewService(repo, novoBuscadorFake("todolist"), verificadorLicencasFake{tem: true}, validadorWorkspacesFake{pertence: true}, ComInvalidadorAcessos(invalidador))
	org, ws := uuid.New(), uuid.New()
	ctx := ctxDaOrganizacao(org)
	a, err := svc.Ativar(ctx, ws, "todolist")
	require.NoError(t, err)
	require.Len(t, repo.porID, 1)
	require.Len(t, invalidador.pares, 1)
	assert.Equal(t, org.String()+"|"+ws.String(), invalidador.pares[0], "ativar invalida app:{org}:{ws}")

	// Outro workspace não desativa a ativação alheia (par confere).
	assert.ErrorIs(t, svc.Desativar(ctx, uuid.New(), a.UUID), ErrNotFound)
	require.Len(t, repo.porID, 1)

	require.NoError(t, svc.Desativar(ctx, ws, a.UUID))
	assert.Empty(t, repo.porID)
	require.Len(t, invalidador.pares, 2, "desativar invalida o par de novo")
}

func TestSlugsLiberadosValidaEntradaERepassa(t *testing.T) {
	repo := novoRepoFake()

	chave := func(org, ws uuid.UUID) string { return org.String() + "|" + ws.String() }
	org, ws := uuid.New(), uuid.New()
	liberados := []AplicacaoDisponivelDto{{Slug: "todolist", Nome: "Todolist"}}
	repo.slugs[chave(org, ws)] = liberados

	svc := NewService(repo, novoBuscadorFake("todolist"), verificadorLicencasFake{tem: true}, validadorWorkspacesFake{pertence: true})

	_, err := svc.SlugsLiberados(context.Background(), uuid.Nil, ws)
	assert.ErrorIs(t, err, ErrInvalidInput, "escopo incompleto é entrada inválida")

	itens, err := svc.SlugsLiberados(context.Background(), org, ws)
	require.NoError(t, err)
	assert.Equal(t, liberados, itens)
}