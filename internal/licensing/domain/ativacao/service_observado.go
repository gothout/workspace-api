package ativacao

import (
	"context"

	"github.com/google/uuid"

	modelativacao "workspace-api/internal/licensing/model/ativacao"
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

func (s serviceObservado) Ativar(ctx context.Context, workspaceUUIDAlvo uuid.UUID, moduloSlug string) (*modelativacao.AtivacaoComModulo, error) {
	a, err := s.Service.Ativar(ctx, workspaceUUIDAlvo, moduloSlug)
	return a, s.observar(ctx, err)
}

func (s serviceObservado) Read(ctx context.Context, workspaceUUIDAlvo, id uuid.UUID) (*modelativacao.AtivacaoComModulo, error) {
	a, err := s.Service.Read(ctx, workspaceUUIDAlvo, id)
	return a, s.observar(ctx, err)
}

func (s serviceObservado) Listar(ctx context.Context, workspaceUUIDAlvo uuid.UUID) ([]modelativacao.AtivacaoComModulo, error) {
	itens, err := s.Service.Listar(ctx, workspaceUUIDAlvo)
	return itens, s.observar(ctx, err)
}

func (s serviceObservado) Desativar(ctx context.Context, workspaceUUIDAlvo, id uuid.UUID) error {
	return s.observar(ctx, s.Service.Desativar(ctx, workspaceUUIDAlvo, id))
}

func (s serviceObservado) SlugsLiberados(ctx context.Context, organizationUUID, workspaceUUID uuid.UUID) ([]AplicacaoDisponivelDto, error) {
	itens, err := s.Service.SlugsLiberados(ctx, organizationUUID, workspaceUUID)
	return itens, s.observar(ctx, err)
}
