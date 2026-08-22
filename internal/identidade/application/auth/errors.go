package auth

import (
	"errors"
	"net/http"

	modeluser "workspace-api/internal/identidade/model/user"
	"workspace-api/internal/pkg/rest_err"
)

// Dominio e Subdominio identificam esta aplicação no catálogo de erros —
// rotas em /api/application/identidade/auth/...
const (
	Dominio    = modeluser.Dominio // "identidade"
	Subdominio = "auth"
)

// errorCatalog é o contrato público das sentinelas da aplicação. Toda falha
// de login (usuário inexistente, senha errada, host sem organization
// resolvível) vira o MESMO corpo: mesmo code, mesma mensagem, mesmo 401 —
// indistinguibilidade é requisito do doc 03.
var errorCatalog = map[error]rest_err.ErroCatalogado{
	ErrCredenciaisInvalidas: {Codigo: "identidade.auth.credenciais_invalidas", Mensagem: "Credenciais inválidas.", Status: http.StatusUnauthorized},
	ErrSessaoInvalida:       {Codigo: "identidade.auth.sessao_invalida", Mensagem: "Sessão inválida ou expirada.", Status: http.StatusUnauthorized},
	ErrInvalidInput:         {Codigo: "identidade.auth.requisicao_invalida", Mensagem: "Dados de entrada inválidos.", Status: http.StatusBadRequest},
}

// ErrInvalidInput — sentinela operacional local para bind/query malformado.
var ErrInvalidInput = errors.New("dados de entrada inválidos")

func init() {
	rest_err.RegistrarCatalogo(Dominio, Subdominio, errorCatalog)
}
