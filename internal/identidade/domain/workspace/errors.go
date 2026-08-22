package workspace

import (
	"errors"
	"net/http"

	modelworkspace "workspace-api/internal/identidade/model/workspace"
	"workspace-api/internal/pkg/rest_err"
)

// Sentinelas OPERACIONAIS do subdomínio — comparadas com errors.Is, nunca por
// texto. As sentinelas de INVARIANTE do modelo moram no pacote folha
// (internal/identidade/model/workspace) e são registradas AQUI no catálogo.
// Regra de mapeamento sentinela → status: entrada inválida = 400;
// não encontrado = 404; conflito de UNICIDADE = 409; INVARIANTE de domínio
// violada = 422; desconhecido = 500.
var (
	ErrNotFound      = errors.New("workspace não encontrado")
	ErrInvalidInput  = errors.New("dados de entrada inválidos")
	ErrSlugEmUso     = errors.New("slug já está em uso por outro workspace")
	ErrSlugReservado = errors.New("slug reservado pela plataforma")
)

// errorCatalog é o contrato público de cada sentinela: código estável,
// mensagem PT-BR, status. Registrado no mapa global do rest_err pelo init() —
// alimenta GET /api/system/errors. Sentinela nova SEM entrada aqui não fecha
// o checklist do subdomínio.
var errorCatalog = map[error]rest_err.ErroCatalogado{
	ErrNotFound:      {Codigo: "identidade.workspace.nao_encontrado", Mensagem: "Workspace não encontrado.", Status: http.StatusNotFound},
	ErrInvalidInput:  {Codigo: "identidade.workspace.entrada_invalida", Mensagem: "Dados de entrada inválidos.", Status: http.StatusBadRequest},
	ErrSlugEmUso:     {Codigo: "identidade.workspace.slug_em_uso", Mensagem: "Slug já está em uso por outro workspace.", Status: http.StatusConflict},
	ErrSlugReservado: {Codigo: "identidade.workspace.slug_reservado", Mensagem: "Slug reservado pela plataforma.", Status: http.StatusUnprocessableEntity},

	// Sentinelas de invariante vindas do pacote model (folha).
	modelworkspace.ErrSlugInvalido: {Codigo: "identidade.workspace.slug_invalido", Mensagem: "Slug fora do formato DNS.", Status: http.StatusBadRequest},
	modelworkspace.ErrNomeInvalido: {Codigo: "identidade.workspace.nome_invalido", Mensagem: "Nome do workspace fora do formato esperado.", Status: http.StatusBadRequest},
	modelworkspace.ErrJaInativo:    {Codigo: "identidade.workspace.ja_inativo", Mensagem: "Workspace já está inativo.", Status: http.StatusUnprocessableEntity},
	modelworkspace.ErrJaAtivo:      {Codigo: "identidade.workspace.ja_ativo", Mensagem: "Workspace já está ativo.", Status: http.StatusUnprocessableEntity},
}

func init() {
	rest_err.RegistrarCatalogo(modelworkspace.Dominio, modelworkspace.Subdominio, errorCatalog)
}
