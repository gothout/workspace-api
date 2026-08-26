package modulo

import (
	"errors"
	"net/http"

	modelmodulo "workspace-api/internal/licensing/model/modulo"
	"workspace-api/internal/pkg/rest_err"
)

// Sentinelas do subdomínio — comparadas com errors.Is, nunca por texto.
// Regra de mapeamento sentinela → status: entrada inválida = 400; não encontrado = 404;
// conflito de UNICIDADE ou uso = 409; INVARIANTE de domínio violada = 422; desconhecido = 500.
var (
	ErrNotFound     = errors.New("módulo não encontrado")
	ErrInvalidInput = errors.New("dados de entrada inválidos")
	ErrSlugEmUso    = errors.New("slug já está em uso por outro módulo")
	ErrModuloEmUso  = errors.New("módulo possui licenças vivas e não pode ser removido")
)

// errorCatalog é o contrato público de cada sentinela: código estável, mensagem PT-BR, status.
// Registrado no mapa global do rest_err pelo init() — alimenta GET /api/system/errors.
// As sentinelas de invariante do modelo são registradas aqui também.
var errorCatalog = map[error]rest_err.ErroCatalogado{
	ErrNotFound:     {Codigo: "licensing.modulo.nao_encontrado", Mensagem: "Módulo não encontrado.", Status: http.StatusNotFound},
	ErrInvalidInput: {Codigo: "licensing.modulo.entrada_invalida", Mensagem: "Dados de entrada inválidos.", Status: http.StatusBadRequest},
	ErrSlugEmUso:    {Codigo: "licensing.modulo.slug_em_uso", Mensagem: "Já existe um módulo com este slug.", Status: http.StatusConflict},
	ErrModuloEmUso:  {Codigo: "licensing.modulo.em_uso", Mensagem: "Módulo possui licenças concedidas; desative-o em vez de remover.", Status: http.StatusConflict},

	modelmodulo.ErrSlugInvalido: {Codigo: "licensing.modulo.slug_invalido", Mensagem: "Slug fora do formato DNS.", Status: http.StatusBadRequest},
	modelmodulo.ErrNomeInvalido: {Codigo: "licensing.modulo.nome_invalido", Mensagem: "Nome fora do formato esperado.", Status: http.StatusUnprocessableEntity},
	modelmodulo.ErrJaAtivo:      {Codigo: "licensing.modulo.ja_ativo", Mensagem: "Módulo já está ativo.", Status: http.StatusUnprocessableEntity},
	modelmodulo.ErrJaInativo:    {Codigo: "licensing.modulo.ja_inativo", Mensagem: "Módulo já está inativo.", Status: http.StatusUnprocessableEntity},
}

func init() {
	rest_err.RegistrarCatalogo(modelmodulo.Dominio, modelmodulo.Subdominio, errorCatalog)
}
