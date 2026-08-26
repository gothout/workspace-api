package ativacao

import (
	"time"

	"github.com/google/uuid"

	modelativacao "workspace-api/internal/licensing/model/ativacao"
)

// AtivacaoResponseDto — saída única do subdomínio; já carrega o resumo do
// módulo (join de leitura) para o painel não fazer N chamadas.
type AtivacaoResponseDto struct {
	UUID             uuid.UUID `json:"uuid"`
	OrganizationUUID uuid.UUID `json:"organization_uuid"`
	WorkspaceUUID    uuid.UUID `json:"workspace_uuid"`
	ModuloUUID       uuid.UUID `json:"modulo_uuid"`
	ModuloSlug       string    `json:"modulo_slug"`
	ModuloNome       string    `json:"modulo_nome"`
	AtivadoPor       string    `json:"ativado_por"`
	CreatedAt        time.Time `json:"created_at"`
}

func NovaAtivacaoResponseDto(a *modelativacao.AtivacaoComModulo) AtivacaoResponseDto {
	ativadoPor := ""
	if a.AtivadoPor != nil {
		ativadoPor = a.AtivadoPor.String()
	}
	return AtivacaoResponseDto{
		UUID:             a.UUID,
		OrganizationUUID: a.OrganizationUUID,
		WorkspaceUUID:    a.WorkspaceUUID,
		ModuloUUID:       a.ModuloUUID,
		ModuloSlug:       a.ModuloSlug,
		ModuloNome:       a.ModuloNome,
		AtivadoPor:       ativadoPor,
		CreatedAt:        a.CreatedAt,
	}
}

func NovaAtivacaoListaResponseDto(items []modelativacao.AtivacaoComModulo) []AtivacaoResponseDto {
	dtos := make([]AtivacaoResponseDto, 0, len(items))
	for i := range items {
		dtos = append(dtos, NovaAtivacaoResponseDto(&items[i]))
	}
	return dtos
}

// AplicacaoDisponivelDto — item da lista "quais aplicações posso usar aqui",
// consumida pelo login e pelo seletor de aplicações do front-end. Alias do
// modelo (folha) para a aplicação não importar o domain.
type AplicacaoDisponivelDto = modelativacao.AplicacaoDisponivelDto
