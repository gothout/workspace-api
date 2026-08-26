package tarefa

import (
	"errors"
	"net/http"

	modeltarefa "workspace-api/internal/todolist/model/tarefa"
	"workspace-api/internal/pkg/rest_err"
)

// Sentinelas do subdomínio — comparadas com errors.Is, nunca por texto.
var (
	ErrNotFound     = errors.New("tarefa não encontrada")
	ErrInvalidInput = errors.New("dados de entrada inválidos")
)

// errorCatalog é o contrato público de cada sentinela: código estável, mensagem PT-BR, status.
var errorCatalog = map[error]rest_err.ErroCatalogado{
	ErrNotFound:     {Codigo: "todolist.tarefa.nao_encontrada", Mensagem: "Tarefa não encontrada.", Status: http.StatusNotFound},
	ErrInvalidInput: {Codigo: "todolist.tarefa.entrada_invalida", Mensagem: "Dados de entrada inválidos.", Status: http.StatusBadRequest},

	modeltarefa.ErrTituloInvalido: {Codigo: "todolist.tarefa.titulo_invalido", Mensagem: "Título fora do formato esperado (2–200 caracteres).", Status: http.StatusUnprocessableEntity},
	modeltarefa.ErrJaConcluida:    {Codigo: "todolist.tarefa.ja_concluida", Mensagem: "Tarefa já está concluída.", Status: http.StatusUnprocessableEntity},
	modeltarefa.ErrJaPendente:     {Codigo: "todolist.tarefa.ja_pendente", Mensagem: "Tarefa já está pendente.", Status: http.StatusUnprocessableEntity},
}

func init() {
	rest_err.RegistrarCatalogo(modeltarefa.Dominio, modeltarefa.Subdominio, errorCatalog)
}
