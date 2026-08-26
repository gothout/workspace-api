package tarefa

import (
	modeltarefa "workspace-api/internal/todolist/model/tarefa"
)

// CreateTarefaRequestDto — entrada de POST /api/domain/todolist/tasks.
type CreateTarefaRequestDto struct {
	Titulo    string `json:"titulo" binding:"required,min=2,max=200"`
	Descricao string `json:"descricao" binding:"omitempty,max=1000"`
}

// ParaEntrada converte o DTO no input do service — controller nunca monta entidade.
func (d CreateTarefaRequestDto) ParaEntrada() modeltarefa.CreateInput {
	return modeltarefa.CreateInput{Titulo: d.Titulo, Descricao: d.Descricao}
}

// UpdateTarefaRequestDto — entrada de PATCH; ponteiros distinguem "ausente" de "vazio".
type UpdateTarefaRequestDto struct {
	Titulo    *string `json:"titulo" binding:"omitempty,min=2,max=200"`
	Descricao *string `json:"descricao" binding:"omitempty,max=1000"`
	Concluida *bool   `json:"concluida" binding:"omitempty"`
}

// ParaEntrada converte o DTO no input do service.
func (d UpdateTarefaRequestDto) ParaEntrada() modeltarefa.UpdateInput {
	return modeltarefa.UpdateInput{Titulo: d.Titulo, Descricao: d.Descricao, Concluida: d.Concluida}
}
