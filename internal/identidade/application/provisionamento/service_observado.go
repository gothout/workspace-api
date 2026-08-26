package provisionamento

import (
	"context"

	"github.com/google/uuid"

	"workspace-api/internal/pkg/errobserve"
)

// serviceObservado decora o Service da aplicação observando TODO erro que
// sobe ao chamador (evolução errobserve): o erro sai INTACTO — a telemetria
// nunca muda a resposta ao cliente. Montado DENTRO do NewService.
type serviceObservado struct {
	Service
	obs *errobserve.Observador
}

func (s serviceObservado) Provisionar(ctx context.Context, organizationUUID uuid.UUID, entrada ProvisionamentoRequestDto) (*ProvisionamentoResponseDto, error) {
	resp, err := s.Service.Provisionar(ctx, organizationUUID, entrada)
	if err != nil {
		return resp, s.obs.Observe(ctx, err)
	}
	return resp, nil
}
