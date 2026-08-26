package licenca

import (
	"errors"
	"net/http"

	modellicenca "workspace-api/internal/licensing/model/licenca"
	"workspace-api/internal/pkg/rest_err"
)

// Sentinelas do subdomínio — comparadas com errors.Is, nunca por texto.
var (
	ErrNotFound      = errors.New("licença não encontrada")
	ErrInvalidInput  = errors.New("dados de entrada inválidos")
	ErrJaConcedida   = errors.New("organization já possui licença viva deste módulo")
	ErrSemAcesso     = errors.New("leitura restrita à própria organization")
	ErrModuloInvalido = errors.New("módulo informado não existe no catálogo")
)

// errorCatalog é o contrato público de cada sentinela: código estável, mensagem PT-BR, status.
// Registrado no mapa global do rest_err pelo init() — alimenta GET /api/system/errors.
var errorCatalog = map[error]rest_err.ErroCatalogado{
	ErrNotFound:       {Codigo: "licensing.licenca.nao_encontrada", Mensagem: "Licença não encontrada.", Status: http.StatusNotFound},
	ErrInvalidInput:   {Codigo: "licensing.licenca.entrada_invalida", Mensagem: "Dados de entrada inválidos.", Status: http.StatusBadRequest},
	ErrJaConcedida:    {Codigo: "licensing.licenca.ja_concedida", Mensagem: "A organization já possui licença deste módulo.", Status: http.StatusConflict},
	ErrSemAcesso:      {Codigo: "licensing.licenca.sem_acesso", Mensagem: "Leitura de licenças restrita à própria organization.", Status: http.StatusForbidden},
	ErrModuloInvalido: {Codigo: "licensing.licenca.modulo_invalido", Mensagem: "Módulo informado não existe no catálogo.", Status: http.StatusUnprocessableEntity},
}

func init() {
	rest_err.RegistrarCatalogo(modellicenca.Dominio, modellicenca.Subdominio, errorCatalog)
}
