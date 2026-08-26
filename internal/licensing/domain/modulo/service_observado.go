package modulo

import (
	"context"

	"github.com/google/uuid"

	modelmodulo "workspace-api/internal/licensing/model/modulo"
	"workspace-api/internal/pkg/errobserve"
)

// serviceObservado decora o Service do subdomínio observando TODO erro que
// sobe ao chamador (evolução errobserve): o erro sai INTACTO — a telemetria
// nunca muda a resposta ao cliente. Montado DENTRO do NewService.
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

func (s serviceObservado) Create(ctx context.Context, in modelmodulo.CreateInput) (*modelmodulo.Modulo, error) {
	m, err := s.Service.Create(ctx, in)
	return m, s.observar(ctx, err)
}

func (s serviceObservado) Read(ctx context.Context, id uuid.UUID) (*modelmodulo.Modulo, error) {
	m, err := s.Service.Read(ctx, id)
	return m, s.observar(ctx, err)
}

func (s serviceObservado) BuscarPorSlug(ctx context.Context, slug string) (*modelmodulo.Modulo, error) {
	m, err := s.Service.BuscarPorSlug(ctx, slug)
	return m, s.observar(ctx, err)
}

func (s serviceObservado) List(ctx context.Context, f modelmodulo.ListFilter) ([]modelmodulo.Modulo, int64, error) {
	itens, total, err := s.Service.List(ctx, f)
	return itens, total, s.observar(ctx, err)
}

func (s serviceObservado) Update(ctx context.Context, id uuid.UUID, in modelmodulo.UpdateInput) (*modelmodulo.Modulo, error) {
	m, err := s.Service.Update(ctx, id, in)
	return m, s.observar(ctx, err)
}

func (s serviceObservado) Delete(ctx context.Context, id uuid.UUID) error {
	return s.observar(ctx, s.Service.Delete(ctx, id))
}
