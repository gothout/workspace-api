package workspace

import (
	"context"

	"github.com/google/uuid"

	modelworkspace "workspace-api/internal/identidade/model/workspace"
	"workspace-api/internal/pkg/errobserve"
)

// serviceObservado decora o Service do subdomínio observando TODO erro que
// sobe ao chamador (evolução errobserve): o erro sai INTACTO — a telemetria
// nunca muda a resposta ao cliente. Montado DENTRO do NewService; métodos
// sem erro (SlugsFixos) delegam pelo embedding.
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

func (s serviceObservado) Create(ctx context.Context, in modelworkspace.CreateInput) (*modelworkspace.Workspace, error) {
	w, err := s.Service.Create(ctx, in)
	return w, s.observar(ctx, err)
}

func (s serviceObservado) Read(ctx context.Context, id uuid.UUID) (*modelworkspace.Workspace, error) {
	w, err := s.Service.Read(ctx, id)
	return w, s.observar(ctx, err)
}

func (s serviceObservado) List(ctx context.Context, f modelworkspace.ListFilter) ([]modelworkspace.Workspace, int64, error) {
	itens, total, err := s.Service.List(ctx, f)
	return itens, total, s.observar(ctx, err)
}

func (s serviceObservado) Update(ctx context.Context, id uuid.UUID, in modelworkspace.UpdateInput) (*modelworkspace.Workspace, error) {
	w, err := s.Service.Update(ctx, id, in)
	return w, s.observar(ctx, err)
}

func (s serviceObservado) Reativar(ctx context.Context, id uuid.UUID) (*modelworkspace.Workspace, error) {
	w, err := s.Service.Reativar(ctx, id)
	return w, s.observar(ctx, err)
}

func (s serviceObservado) Delete(ctx context.Context, id uuid.UUID) error {
	return s.observar(ctx, s.Service.Delete(ctx, id))
}

func (s serviceObservado) SuspenderPorOrganization(ctx context.Context, organizationUUID uuid.UUID) (int, error) {
	suspensos, err := s.Service.SuspenderPorOrganization(ctx, organizationUUID)
	return suspensos, s.observar(ctx, err)
}

func (s serviceObservado) ResolverPorSlug(ctx context.Context, slug string) (*Resolvido, error) {
	resolvido, err := s.Service.ResolverPorSlug(ctx, slug)
	return resolvido, s.observar(ctx, err)
}

func (s serviceObservado) ResolverPorUUID(ctx context.Context, id uuid.UUID) (*Resolvido, error) {
	resolvido, err := s.Service.ResolverPorUUID(ctx, id)
	return resolvido, s.observar(ctx, err)
}
