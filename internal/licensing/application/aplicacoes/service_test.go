package aplicacoes

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	modelativacao "workspace-api/internal/licensing/model/ativacao"
	"workspace-api/internal/pkg/orgctx"
)

type provedorFalso struct {
	itens []modelativacao.AplicacaoDisponivelDto
	err   error
}

func (p provedorFalso) Liberadas(context.Context, uuid.UUID, uuid.UUID) ([]modelativacao.AplicacaoDisponivelDto, error) {
	return p.itens, p.err
}

func ctxComEscopo() context.Context {
	return orgctx.WithWorkspace(orgctx.WithOrganization(context.Background(), uuid.New()), uuid.New())
}

func TestMinhasAplicacoesDevolveListaDoProvedor(t *testing.T) {
	svc := NewService(provedorFalso{itens: []modelativacao.AplicacaoDisponivelDto{
		{Slug: "todolist", Nome: "Todolist"},
		{Slug: "crm", Nome: "CRM"},
	}})
	itens, err := svc.MinhasAplicacoes(ctxComEscopo())
	require.NoError(t, err)
	assert.Len(t, itens, 2)
	assert.Equal(t, "todolist", itens[0].Slug)
}

func TestMinhasAplicacoesNuncaDevolveNull(t *testing.T) {
	svc := NewService(provedorFalso{})
	itens, err := svc.MinhasAplicacoes(ctxComEscopo())
	require.NoError(t, err)
	assert.NotNil(t, itens)
	assert.Empty(t, itens, "contrato: lista vazia, nunca null — o seletor desenha o empty state")
}

func TestMinhasAplicacoesFailClosed(t *testing.T) {
	t.Run("provedor ausente recusa", func(t *testing.T) {
		svc := NewService(nil)
		_, err := svc.MinhasAplicacoes(ctxComEscopo())
		assert.ErrorIs(t, err, ErrInvalidInput)
	})
	t.Run("falha de infra sobe intacta", func(t *testing.T) {
		svc := NewService(provedorFalso{err: errors.New("banco fora")})
		_, err := svc.MinhasAplicacoes(ctxComEscopo())
		assert.Error(t, err)
		assert.NotErrorIs(t, err, ErrInvalidInput)
	})
}
