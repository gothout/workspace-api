package logs

import (
	"context"

	"workspace-api/internal/infra/clickhouse"
	"workspace-api/internal/pkg/errobserve"
	"workspace-api/internal/pkg/pagination"
)

// serviceObservado decora o Service da aplicação observando TODO erro que
// sobe ao chamador (evolução errobserve): o erro sai INTACTO — a telemetria
// nunca muda a resposta. Montado DENTRO do NewService.
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

func (s serviceObservado) Auditoria(ctx context.Context, filtro clickhouse.FiltroTrilha, p pagination.Pagination) (pagination.Response[AuditoriaItemDto], error) {
	resp, err := s.Service.Auditoria(ctx, filtro, p)
	return resp, s.observar(ctx, err)
}

func (s serviceObservado) Acesso(ctx context.Context, filtro clickhouse.FiltroTrilha, p pagination.Pagination) (pagination.Response[AcessoItemDto], error) {
	resp, err := s.Service.Acesso(ctx, filtro, p)
	return resp, s.observar(ctx, err)
}

func (s serviceObservado) Erros(ctx context.Context, filtro clickhouse.FiltroTrilha, p pagination.Pagination) (pagination.Response[ErroItemDto], error) {
	resp, err := s.Service.Erros(ctx, filtro, p)
	return resp, s.observar(ctx, err)
}
