package licenca

import (
	"context"

	"github.com/google/uuid"

	modellicenca "workspace-api/internal/licensing/model/licenca"
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

func (s serviceObservado) Atribuir(ctx context.Context, organizationUUIDAlvo uuid.UUID, moduloSlug string) (*modellicenca.LicencaComModulo, error) {
	l, err := s.Service.Atribuir(ctx, organizationUUIDAlvo, moduloSlug)
	return l, s.observar(ctx, err)
}

func (s serviceObservado) Read(ctx context.Context, organizationUUIDAlvo, id uuid.UUID) (*modellicenca.LicencaComModulo, error) {
	l, err := s.Service.Read(ctx, organizationUUIDAlvo, id)
	return l, s.observar(ctx, err)
}

func (s serviceObservado) Listar(ctx context.Context, organizationUUIDAlvo uuid.UUID) ([]modellicenca.LicencaComModulo, error) {
	itens, err := s.Service.Listar(ctx, organizationUUIDAlvo)
	return itens, s.observar(ctx, err)
}

func (s serviceObservado) Revogar(ctx context.Context, organizationUUIDAlvo, id uuid.UUID) error {
	return s.observar(ctx, s.Service.Revogar(ctx, organizationUUIDAlvo, id))
}

func (s serviceObservado) ExisteViva(ctx context.Context, organizationUUID, moduloUUID uuid.UUID) (bool, error) {
	existe, err := s.Service.ExisteViva(ctx, organizationUUID, moduloUUID)
	return existe, s.observar(ctx, err)
}
