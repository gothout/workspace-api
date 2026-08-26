package modulo

import (
	modelmodulo "workspace-api/internal/licensing/model/modulo"
)

// CreateModuloRequestDto — entrada de POST /api/domain/licensing/modulos.
type CreateModuloRequestDto struct {
	Nome      string `json:"nome" binding:"required,min=2,max=120"`
	Slug      string `json:"slug" binding:"required,min=3,max=63,slugdns"`
	Descricao string `json:"descricao" binding:"omitempty,max=500"`
}

// ParaEntrada converte o DTO no input do service — controller nunca monta entidade.
func (d CreateModuloRequestDto) ParaEntrada() modelmodulo.CreateInput {
	return modelmodulo.CreateInput{Nome: d.Nome, Slug: d.Slug, Descricao: d.Descricao}
}

// UpdateModuloRequestDto — entrada de PATCH; ponteiros distinguem "ausente" de "vazio".
type UpdateModuloRequestDto struct {
	Nome      *string `json:"nome" binding:"omitempty,min=2,max=120"`
	Descricao *string `json:"descricao" binding:"omitempty,max=500"`
	Ativo     *bool   `json:"ativo" binding:"omitempty"`
}

// ParaEntrada converte o DTO no input do service — controller nunca monta entidade.
func (d UpdateModuloRequestDto) ParaEntrada() modelmodulo.UpdateInput {
	return modelmodulo.UpdateInput{Nome: d.Nome, Descricao: d.Descricao, Ativo: d.Ativo}
}
