package modulo

import (
	"time"

	"github.com/google/uuid"

	modelmodulo "workspace-api/internal/licensing/model/modulo"
	"workspace-api/internal/pkg/pagination"
)

// ModuloResponseDto — saída única do subdomínio.
type ModuloResponseDto struct {
	UUID      uuid.UUID `json:"uuid"`
	Slug      string    `json:"slug"`
	Nome      string    `json:"nome"`
	Descricao string    `json:"descricao"`
	Ativo     bool      `json:"ativo"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func NovoModuloResponseDto(m *modelmodulo.Modulo) ModuloResponseDto {
	descricao := ""
	if m.Descricao != nil {
		descricao = *m.Descricao
	}
	return ModuloResponseDto{
		UUID: m.UUID, Slug: m.Slug.String(), Nome: m.Nome,
		Descricao: descricao, Ativo: m.Ativo,
		CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

func NovoModuloListaResponseDto(items []modelmodulo.Modulo, total int64, p pagination.Pagination) pagination.Response[ModuloResponseDto] {
	dtos := make([]ModuloResponseDto, 0, len(items))
	for i := range items {
		dtos = append(dtos, NovoModuloResponseDto(&items[i]))
	}
	return pagination.NovaResponse(dtos, total, p)
}
