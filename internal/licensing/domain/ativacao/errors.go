package ativacao

import (
	"errors"
	"net/http"

	modelativacao "workspace-api/internal/licensing/model/ativacao"
	"workspace-api/internal/pkg/rest_err"
)

// Sentinelas do subdomínio — comparadas com errors.Is, nunca por texto.
var (
	ErrNotFound          = errors.New("ativação não encontrada")
	ErrInvalidInput      = errors.New("dados de entrada inválidos")
	ErrJaAtivada         = errors.New("módulo já está ativado neste workspace")
	ErrSemLicenca        = errors.New("a organization não possui licença viva deste módulo")
	ErrWorkspaceInvalido = errors.New("workspace inexistente ou fora da organization")
	ErrModuloInvalido    = errors.New("módulo inexistente ou desativado no catálogo")
)

// errorCatalog é o contrato público de cada sentinela: código estável, mensagem PT-BR, status.
// Registrado no mapa global do rest_err pelo init() — alimenta GET /api/system/errors.
// Workspace e módulo inexistentes respondem o MESMO 404 (não vaza existência).
var errorCatalog = map[error]rest_err.ErroCatalogado{
	ErrNotFound:     {Codigo: "licensing.ativacao.nao_encontrada", Mensagem: "Ativação não encontrada.", Status: http.StatusNotFound},
	ErrInvalidInput: {Codigo: "licensing.ativacao.entrada_invalida", Mensagem: "Dados de entrada inválidos.", Status: http.StatusBadRequest},
	ErrJaAtivada:    {Codigo: "licensing.ativacao.ja_ativada", Mensagem: "Módulo já está ativado neste workspace.", Status: http.StatusConflict},
	ErrSemLicenca:   {Codigo: "licensing.ativacao.sem_licenca", Mensagem: "A organization não possui licença deste módulo; solicite ao administrador da plataforma.", Status: http.StatusUnprocessableEntity},

	ErrWorkspaceInvalido: {Codigo: "licensing.ativacao.workspace_invalido", Mensagem: "Workspace não encontrado no escopo da organization.", Status: http.StatusNotFound},
	ErrModuloInvalido:    {Codigo: "licensing.ativacao.modulo_invalido", Mensagem: "Módulo não encontrado no catálogo ou desativado.", Status: http.StatusNotFound},
}

func init() {
	rest_err.RegistrarCatalogo(modelativacao.Dominio, modelativacao.Subdominio, errorCatalog)
}
