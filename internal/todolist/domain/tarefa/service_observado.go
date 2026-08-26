package tarefa

import (
	"context"

	"github.com/google/uuid"

	modeltarefa "workspace-api/internal/todolist/model/tarefa"
	"workspace-api/internal/pkg/errobserve"
)

// serviceObservado decora o Service observando TODO erro que sobe ao chamador
// (evolução errobserve): o erro sai INTACTO.
type serviceObservado struct {
	Service
	obs *errobserve.Observador
}

func (s serviceObservado) observar(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	return s.obs.Observe(ctx, err)
}

func (s serviceObservado) Create(ctx context.Context, in modeltarefa.CreateInput) (*modeltarefa.Tarefa, error) {
	t, err := s.Service.Create(ctx, in)
	return t, s.observar(ctx, err)
}

func (s serviceObservado) Read(ctx context.Context, id uuid.UUID) (*modeltarefa.Tarefa, error) {
	t, err := s.Service.Read(ctx, id)
	return t, s.observar(ctx, err)
}

func (s serviceObservado) List(ctx context.Context, f modeltarefa.ListFilter) ([]modeltarefa.Tarefa, int64, error) {
	itens, total, err := s.Service.List(ctx, f)
	return itens, total, s.observar(ctx, err)
}

func (s serviceObservado) Update(ctx context.Context, id uuid.UUID, in modeltarefa.UpdateInput) (*modeltarefa.Tarefa, error) {
	t, err := s.Service.Update(ctx, id, in)
	return t, s.observar(ctx, err)
}

func (s serviceObservado) Delete(ctx context.Context, id uuid.UUID) error {
	return s.observar(ctx, s.Service.Delete(ctx, id))
}
