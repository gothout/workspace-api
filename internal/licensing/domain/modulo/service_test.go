package modulo

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	modelmodulo "workspace-api/internal/licensing/model/modulo"
)

// --- Dublês (regra: testes de service NUNCA passam pelo singleton) ----------

type repoFake struct {
	mu      sync.Mutex
	porUUID map[uuid.UUID]*modelmodulo.Modulo
	slugs   map[modelmodulo.Slug]bool // simula o índice único TOTAL do banco
}

func novoRepoFake() *repoFake {
	return &repoFake{
		porUUID: map[uuid.UUID]*modelmodulo.Modulo{},
		slugs:   map[modelmodulo.Slug]bool{},
	}
}

func (r *repoFake) Criar(_ context.Context, m *modelmodulo.Modulo) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.slugs[m.Slug] {
		return ErrSlugEmUso
	}
	copia := *m
	r.porUUID[m.UUID] = &copia
	r.slugs[m.Slug] = true
	return nil
}

func (r *repoFake) BuscarPorUUID(_ context.Context, id uuid.UUID) (*modelmodulo.Modulo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.porUUID[id]
	if !ok {
		return nil, ErrNotFound
	}
	copia := *m
	return &copia, nil
}

func (r *repoFake) BuscarPorSlug(_ context.Context, slug modelmodulo.Slug) (*modelmodulo.Modulo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, m := range r.porUUID {
		if m.Slug == slug {
			copia := *m
			return &copia, nil
		}
	}
	return nil, ErrNotFound
}

func (r *repoFake) Listar(_ context.Context, f modelmodulo.ListFilter) ([]modelmodulo.Modulo, int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]modelmodulo.Modulo, 0, len(r.porUUID))
	for _, m := range r.porUUID {
		if f.Ativo != nil && m.Ativo != *f.Ativo {
			continue
		}
		items = append(items, *m)
	}
	return items, int64(len(items)), nil
}

func (r *repoFake) Atualizar(_ context.Context, m *modelmodulo.Modulo) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.porUUID[m.UUID]; !ok {
		return ErrNotFound
	}
	copia := *m
	r.porUUID[m.UUID] = &copia
	return nil
}

func (r *repoFake) Remover(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.porUUID[id]
	if !ok {
		return ErrNotFound
	}
	delete(r.porUUID, id)
	delete(r.slugs, m.Slug)
	return nil
}

type verificadorFake struct {
	emUso bool
	err   error
}

func (v verificadorFake) ExisteParaModulo(context.Context, uuid.UUID) (bool, error) {
	return v.emUso, v.err
}

// invalidadorFake conta as invalidações disparadas pelas escritas.
type invalidadorFake struct{ invalidacoes int }

func (i *invalidadorFake) InvalidarTudo(context.Context) { i.invalidacoes++ }

// --- Suíte -------------------------------------------------------------------

func montarServico(t *testing.T, verificador VerificadorLicencas) (Service, *repoFake) {
	t.Helper()
	repo := novoRepoFake()
	return NewService(repo, verificador), repo
}

// O catálogo é GLOBAL da plataforma: criar/l SEM organization no ctx é
// operação normal (a fronteira é a permissão rota a rota, não o tenancy).
var ctxPlataforma = context.Background()

func TestCreateNasceAtivoEValidaInvariantes(t *testing.T) {
	svc, repo := montarServico(t, verificadorFake{})

	m, err := svc.Create(ctxPlataforma, modelmodulo.CreateInput{Nome: "  Todolist  ", Slug: "todolist", Descricao: "  Lista de tarefas  "})
	require.NoError(t, err)
	assert.True(t, m.Ativo)
	assert.Equal(t, "todolist", m.Slug.String())
	assert.Equal(t, "Todolist", m.Nome)
	require.NotNil(t, m.Descricao)
	assert.Equal(t, "Lista de tarefas", *m.Descricao)
	require.Len(t, repo.porUUID, 1)

	// Descrição vazia vira NULL (sem string vazia gravada).
	vazio, err := svc.Create(ctxPlataforma, modelmodulo.CreateInput{Nome: "Vazio", Slug: "vazio"})
	require.NoError(t, err)
	assert.Nil(t, vazio.Descricao)

	casos := []struct {
		nome    string
		entrada modelmodulo.CreateInput
		esperado error
	}{
		{"nome curto", modelmodulo.CreateInput{Nome: "a", Slug: "outro"}, modelmodulo.ErrNomeInvalido},
		{"slug maiúsculo", modelmodulo.CreateInput{Nome: "Válido", Slug: "Ruim"}, modelmodulo.ErrSlugInvalido},
	}
	for _, caso := range casos {
		_, err := svc.Create(ctxPlataforma, caso.entrada)
		assert.ErrorIs(t, err, caso.esperado, "%s deveria recusar", caso.nome)
	}
}

func TestCreateRecusaSlugDuplicado(t *testing.T) {
	svc, _ := montarServico(t, verificadorFake{})
	_, err := svc.Create(ctxPlataforma, modelmodulo.CreateInput{Nome: "Primeiro", Slug: "todolist"})
	require.NoError(t, err)
	_, err = svc.Create(ctxPlataforma, modelmodulo.CreateInput{Nome: "Segundo", Slug: "todolist"})
	assert.ErrorIs(t, err, ErrSlugEmUso)
}

func TestUpdateTraduzInputEmComportamento(t *testing.T) {
	invalidador := &invalidadorFake{}
	svc := NewService(novoRepoFake(), verificadorFake{}, ComInvalidadorAcessos(invalidador))
	m, err := svc.Create(ctxPlataforma, modelmodulo.CreateInput{Nome: "Todolist", Slug: "todolist"})
	require.NoError(t, err)
	require.Equal(t, 0, invalidador.invalidacoes, "criar módulo novo não invalida resolução de acesso")

	novoNome := "Todolist Pro"
	falso := false
	atualizado, err := svc.Update(ctxPlataforma, m.UUID, modelmodulo.UpdateInput{Nome: &novoNome, Ativo: &falso})
	require.NoError(t, err)
	assert.Equal(t, novoNome, atualizado.Nome)
	assert.False(t, atualizado.Ativo)
	assert.Equal(t, 1, invalidador.invalidacoes, "desativar invalida o cache de aplicações")

	// Desativar de novo: invariante violada nem chega ao repo (422).
	_, err = svc.Update(ctxPlataforma, m.UUID, modelmodulo.UpdateInput{Ativo: &falso})
	assert.ErrorIs(t, err, modelmodulo.ErrJaInativo)

	// Reativar pelo PATCH (ativo=true) — transição única.
	verdadeiro := true
	reativado, err := svc.Update(ctxPlataforma, m.UUID, modelmodulo.UpdateInput{Ativo: &verdadeiro})
	require.NoError(t, err)
	assert.True(t, reativado.Ativo)
	assert.Equal(t, 2, invalidador.invalidacoes, "reativar também mexe na resolução")
}

func TestDeleteExigeVerificadorESemLicenca(t *testing.T) {
	t.Run("verificador ausente recusa fechada", func(t *testing.T) {
		svc, repo := montarServico(t, nil)
		m, err := svc.Create(ctxPlataforma, modelmodulo.CreateInput{Nome: "Todolist", Slug: "todolist"})
		require.NoError(t, err)
		assert.ErrorIs(t, svc.Delete(ctxPlataforma, m.UUID), ErrModuloEmUso)
		assert.Len(t, repo.porUUID, 1, "nada é removido sem saber o estado real")
	})
	t.Run("com licença viva recusa", func(t *testing.T) {
		svc, repo := montarServico(t, verificadorFake{emUso: true})
		m, err := svc.Create(ctxPlataforma, modelmodulo.CreateInput{Nome: "Todolist", Slug: "todolist"})
		require.NoError(t, err)
		assert.ErrorIs(t, svc.Delete(ctxPlataforma, m.UUID), ErrModuloEmUso)
		assert.Len(t, repo.porUUID, 1)
	})
	t.Run("falha do verificador sobe intacta", func(t *testing.T) {
		svc, _ := montarServico(t, verificadorFake{err: errors.New("banco fora")})
		m, err := svc.Create(ctxPlataforma, modelmodulo.CreateInput{Nome: "Todolist", Slug: "todolist"})
		require.NoError(t, err)
		err = svc.Delete(ctxPlataforma, m.UUID)
		require.Error(t, err)
		assert.NotErrorIs(t, err, ErrModuloEmUso)
	})
	t.Run("sem licença remove e não volta", func(t *testing.T) {
		svc, _ := montarServico(t, verificadorFake{})
		m, err := svc.Create(ctxPlataforma, modelmodulo.CreateInput{Nome: "Todolist", Slug: "todolist"})
		require.NoError(t, err)
		require.NoError(t, svc.Delete(ctxPlataforma, m.UUID))
		_, err = svc.Read(ctxPlataforma, m.UUID)
		assert.ErrorIs(t, err, ErrNotFound)
	})
}

func TestBuscarPorSlugRotuloMalformadoViraNotFound(t *testing.T) {
	svc, _ := montarServico(t, verificadorFake{})
	_, err := svc.BuscarPorSlug(ctxPlataforma, "Ruim")
	assert.ErrorIs(t, err, ErrNotFound)
}
