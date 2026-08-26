// Package aplicacoes é a ORQUESTRAÇÃO do domínio licensing que responde ao
// SELEtor de aplicações: dado o par (organization, workspace) resolvido na
// requisição, devolve os módulos liberados — o login e o painel do front
// consomem ESTA lista para decidir se mostram seletor ou entram direto.
package aplicacoes

import (
	"context"

	modelativacao "workspace-api/internal/licensing/model/ativacao"
	"workspace-api/internal/pkg/orgctx"
)

// Dominio e Subdominio identificam esta aplicação nos catálogos e logs.
const (
	Dominio    = "licensing"
	Subdominio = "aplicacoes"
)

// Service é a interface do caso de uso.
type Service interface {
	MinhasAplicacoes(ctx context.Context) ([]modelativacao.AplicacaoDisponivelDto, error)
}

type serviceImpl struct {
	provedor ProvedorAcessos
}

// NewService monta o service DECORADO com o observador de erros (errobserve).
func NewService(provedor ProvedorAcessos) Service {
	return serviceObservado{Service: &serviceImpl{provedor: provedor}, obs: observadorErros}
}

// MinhasAplicacoes devolve os módulos liberados do par resolvido na
// requisição — escopo ausente é entrada inválida (fail-closed: nunca lista
// aberta).
func (s *serviceImpl) MinhasAplicacoes(ctx context.Context) ([]modelativacao.AplicacaoDisponivelDto, error) {
	if s.provedor == nil {
		// Peça faltando é boot quebrado: recusa em vez de lista vazia
		// silenciosa (o seletor mostraria "sem aplicações" por erro de boot).
		return nil, ErrInvalidInput
	}
	itens, err := s.provedor.Liberadas(ctx, orgctx.OrganizationUUID(ctx), orgctx.WorkspaceUUID(ctx))
	if err != nil {
		return nil, err
	}
	if itens == nil {
		itens = []modelativacao.AplicacaoDisponivelDto{} // contrato: lista vazia, nunca null
	}
	return itens, nil
}
