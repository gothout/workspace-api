package aplicacoes

import (
	"errors"
	"net/http"

	"workspace-api/internal/pkg/rest_err"
)

// Sentinelas do caso de uso — comparadas com errors.Is, nunca por texto.
var (
	ErrInvalidInput = errors.New("escopo de acesso ausente na requisição")
)

// errorCatalog é o contrato público: código estável + mensagem PT-BR + status.
var errorCatalog = map[error]rest_err.ErroCatalogado{
	ErrInvalidInput: {Codigo: "licensing.aplicacoes.entrada_invalida", Mensagem: "Escopo de acesso ausente na requisição.", Status: http.StatusBadRequest},
}

func init() {
	rest_err.RegistrarCatalogo(Dominio, Subdominio, errorCatalog)
}
