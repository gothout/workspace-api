package tarefa

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	modeltarefa "workspace-api/internal/todolist/model/tarefa"
	"workspace-api/internal/pkg/orgctx"
)

// --- Dublê (regra: testes de service NUNCA passam pelo singleton) -----------

type repoFake struct {
	mu     sync.Mutex
	porID  map[uuid.UUID]*modeltarefa.Tarefa
}

func novoRepoFake() *repoFake { return &repoFake{porID: map[uuid.UUID]*modeltarefa.Tarefa{}} }

// exigirEscopo emula o fail-closed do orgctx.Scope no banco real.
func exigirEscopo(ctx context.Context, t *modeltarefa.Tarefa) error {
	org, ws := orgctx.OrganizationUUID(ctx), orgctx.WorkspaceUUID(ctx)
	if org == uuid.Nil || ws == uuid.Nil {
		return orgctx.ErrEscopoAusente
	}
	if t.OrganizationUUID != org || t.WorkspaceUUID != ws {
		return ErrNotFound // filtro de escopo não encontra → não vaza existência
	}
	return nil
}

func (r *repoFake) Criar(ctx context.Context, t *modeltarefa.Tarefa) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := exigirEscopo(ctx, t); err != nil {
		return err
	}
	copia := *t
	r.porID[t.UUID] = &copia
	return nil
}

func (r *repoFake) BuscarPorUUID(ctx context.Context, id uuid.UUID) (*modeltarefa.Tarefa, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.porID[id]
	if !ok {
		return nil, ErrNotFound
	}
	if err := exigirEscopo(ctx, t); err != nil {
		return nil, err
	}
	copia := *t
	return &copia, nil
}

func (r *repoFake) Listar(ctx context.Context, f modeltarefa.ListFilter) ([]modeltarefa.Tarefa, int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]modeltarefa.Tarefa, 0, len(r.porID))
	for _, t := range r.porID {
		if err := exigirEscopo(ctx, t); err != nil {
			continue
		}
		if f.Concluida != nil && t.Concluida != *f.Concluida {
			continue
		}
		items = append(items, *t)
	}
	return items, int64(len(items)), nil
}

func (r *repoFake) Atualizar(ctx context.Context, t *modeltarefa.Tarefa) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	existente, ok := r.porID[t.UUID]
	if !ok {
		return ErrNotFound
	}
	if err := exigirEscopo(ctx, existente); err != nil {
		return err
	}
	copia := *t
	r.porID[t.UUID] = &copia
	return nil
}

func (r *repoFake) Remover(ctx context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.porID[id]
	if !ok {
		return ErrNotFound
	}
	if err := exigirEscopo(ctx, t); err != nil {
		return err
	}
	delete(r.porID, id)
	return nil
}

// --- Suíte -------------------------------------------------------------------

func montarServico(t *testing.T) (Service, *repoFake) {
	t.Helper()
	repo := novoRepoFake()
	return NewService(repo), repo
}

func ctxDoPar(org, ws uuid.UUID) context.Context {
	return orgctx.WithWorkspace(orgctx.WithOrganization(context.Background(), org), ws)
}

func TestCreateNascePendenteComTenancyDoCtx(t *testing.T) {
	svc, repo := montarServico(t)
	org, ws := uuid.New(), uuid.New()

	tarefa, err := svc.Create(ctxDoPar(org, ws), modeltarefa.CreateInput{Titulo: "  Comprar leite  "})
	require.NoError(t, err)
	assert.Equal(t, "Comprar leite", tarefa.Titulo)
	assert.False(t, tarefa.Concluida)
	assert.Equal(t, org, tarefa.OrganizationUUID, "tenancy vem do ctx, nunca do corpo")
	assert.Equal(t, ws, tarefa.WorkspaceUUID)
	require.Len(t, repo.porID, 1)

	// Título curto recusa pelo construtor.
	_, err = svc.Create(ctxDoPar(org, ws), modeltarefa.CreateInput{Titulo: "a"})
	assert.ErrorIs(t, err, modeltarefa.ErrTituloInvalido)
}

func TestLeituraFailClosedENaoVazaEntreWorkspaces(t *testing.T) {
	svc, _ := montarServico(t)
	org := uuid.New()
	wsA, wsB := uuid.New(), uuid.New()

	tarefa, err := svc.Create(ctxDoPar(org, wsA), modeltarefa.CreateInput{Titulo: "Do ws A"})
	require.NoError(t, err)

	// Mesma organization, OUTRO workspace: não enxerga (escopo completo).
	_, err = svc.Read(ctxDoPar(org, wsB), tarefa.UUID)
	assert.ErrorIs(t, err, ErrNotFound)

	// Sem escopo no ctx: query recusada (fail-closed).
	_, err = svc.Read(context.Background(), tarefa.UUID)
	assert.ErrorIs(t, err, orgctx.ErrEscopoAusente)

	visto, err := svc.Read(ctxDoPar(org, wsA), tarefa.UUID)
	require.NoError(t, err)
	assert.Equal(t, "Do ws A", visto.Titulo)
}

func TestUpdateTraduzInputEmComportamento(t *testing.T) {
	svc, _ := montarServico(t)
	org, ws := uuid.New(), uuid.New()
	ctx := ctxDoPar(org, ws)
	tarefa, err := svc.Create(ctx, modeltarefa.CreateInput{Titulo: "Original", Descricao: "Detalhe"})
	require.NoError(t, err)

	novoTitulo := "Original editada"
	concluida := true
	atualizada, err := svc.Update(ctx, tarefa.UUID, modeltarefa.UpdateInput{Titulo: &novoTitulo, Concluida: &concluida})
	require.NoError(t, err)
	assert.Equal(t, novoTitulo, atualizada.Titulo)
	assert.True(t, atualizada.Concluida)

	// Concluir de novo: invariante violada.
	_, err = svc.Update(ctx, tarefa.UUID, modeltarefa.UpdateInput{Concluida: &concluida})
	assert.ErrorIs(t, err, modeltarefa.ErrJaConcluida)

	// Reabrir: transição única.
	pendente := false
	reaberta, err := svc.Update(ctx, tarefa.UUID, modeltarefa.UpdateInput{Concluida: &pendente})
	require.NoError(t, err)
	assert.False(t, reaberta.Concluida)
}

func TestDeleteRemoveENaoVazaEntreWorkspaces(t *testing.T) {
	svc, repo := montarServico(t)
	org := uuid.New()
	wsA, wsB := uuid.New(), uuid.New()
	tarefa, err := svc.Create(ctxDoPar(org, wsA), modeltarefa.CreateInput{Titulo: "Apagar"})
	require.NoError(t, err)

	// Outro workspace não remove (par confere).
	assert.ErrorIs(t, svc.Delete(ctxDoPar(org, wsB), tarefa.UUID), ErrNotFound)
	require.Len(t, repo.porID, 1)

	require.NoError(t, svc.Delete(ctxDoPar(org, wsA), tarefa.UUID))
	assert.Empty(t, repo.porID)
	assert.ErrorIs(t, svc.Delete(ctxDoPar(org, wsA), tarefa.UUID), ErrNotFound)
}
