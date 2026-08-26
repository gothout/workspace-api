package aplicacoes

import (
	"context"

	modelativacao "workspace-api/internal/licensing/model/ativacao"
	"workspace-api/internal/pkg/errobserve"
)

// serviceObservado decora o Service observando TODO erro que sobe ao chamador
// (evolução errobserve): o erro sai INTACTO — a telemetria nunca muda a
// resposta ao cliente.
type serviceObservado struct {
	Service
	obs *errobserve.Observador
}

func (s serviceObservado) MinhasAplicacoes(ctx context.Context) ([]modelativacao.AplicacaoDisponivelDto, error) {
	itens, err := s.Service.MinhasAplicacoes(ctx)
	if err != nil {
		_ = s.obs.Observe(ctx, err)
	}
	return itens, err
}
