package tarefa

import (
	"time"

	"github.com/google/uuid"

	modeltarefa "workspace-api/internal/todolist/model/tarefa"
	"workspace-api/internal/pkg/pagination"
)

// TarefaResponseDto — saída única do subdomínio.
type TarefaResponseDto struct {
	UUID      uuid.UUID `json:"uuid"`
	Titulo    string    `json:"titulo"`
	Descricao string    `json:"descricao"`
	Concluida bool      `json:"concluida"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func NovaTarefaResponseDto(t *modeltarefa.Tarefa) TarefaResponseDto {
	descricao := ""
	if t.Descricao != nil {
		descricao = *t.Descricao
	}
	return TarefaResponseDto{
		UUID: t.UUID, Titulo: t.Titulo, Descricao: descricao,
		Concluida: t.Concluida, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	}
}

func NovaTarefaListaResponseDto(items []modeltarefa.Tarefa, total int64, p pagination.Pagination) pagination.Response[TarefaResponseDto] {
	dtos := make([]TarefaResponseDto, 0, len(items))
	for i := range items {
		dtos = append(dtos, NovaTarefaResponseDto(&items[i]))
	}
	return pagination.NovaResponse(dtos, total, p)
}
