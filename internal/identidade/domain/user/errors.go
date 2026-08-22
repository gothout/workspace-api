package user

import (
	"errors"
	"net/http"

	modeluser "workspace-api/internal/identidade/model/user"
	"workspace-api/internal/pkg/rest_err"
)

// Sentinelas OPERACIONAIS do subdomínio — comparadas com errors.Is, nunca por
// texto. As sentinelas de INVARIANTE do modelo moram no pacote folha
// (internal/identidade/model/user) e são registradas AQUI no catálogo.
// Regra de mapeamento sentinela → status: entrada inválida = 400;
// não encontrado = 404; conflito de UNICIDADE = 409; credencial/sessão =
// 401; INVARIANTE de domínio violada = 422; desconhecido = 500.
var (
	ErrNotFound                = errors.New("usuário não encontrado")
	ErrInvalidInput            = errors.New("dados de entrada inválidos")
	ErrEmailEmUso              = errors.New("e-mail já cadastrado nesta organization")
	ErrCredenciaisInvalidas    = errors.New("credenciais inválidas")
	ErrPapelNaoEncontrado      = errors.New("papel não encontrado")
	ErrAtribuicaoDuplicada     = errors.New("o usuário já exerce este papel neste workspace")
	ErrAtribuicaoNaoEncontrada = errors.New("atribuição não encontrada para este usuário")
	ErrWorkspaceInvalido       = errors.New("workspace inexistente ou inativo nesta organization")
	ErrRefreshTokenInvalido    = errors.New("refresh token inválido, expirado ou revogado")
)

// errorCatalog é o contrato público de cada sentinela: código estável,
// mensagem PT-BR, status. Registrado no mapa global do rest_err pelo init() —
// alimenta GET /api/system/errors. Sentinela nova SEM entrada aqui não fecha
// o checklist do subdomínio.
var errorCatalog = map[error]rest_err.ErroCatalogado{
	ErrNotFound:                {Codigo: "identidade.user.nao_encontrado", Mensagem: "Usuário não encontrado.", Status: http.StatusNotFound},
	ErrInvalidInput:            {Codigo: "identidade.user.entrada_invalida", Mensagem: "Dados de entrada inválidos.", Status: http.StatusBadRequest},
	ErrEmailEmUso:              {Codigo: "identidade.user.email_em_uso", Mensagem: "E-mail já cadastrado nesta organization.", Status: http.StatusConflict},
	ErrCredenciaisInvalidas:    {Codigo: "identidade.user.credenciais_invalidas", Mensagem: "Credenciais inválidas.", Status: http.StatusUnauthorized},
	ErrPapelNaoEncontrado:      {Codigo: "identidade.user.papel_nao_encontrado", Mensagem: "Papel não encontrado.", Status: http.StatusNotFound},
	ErrAtribuicaoDuplicada:     {Codigo: "identidade.user.atribuicao_duplicada", Mensagem: "O usuário já exerce este papel neste workspace.", Status: http.StatusConflict},
	ErrAtribuicaoNaoEncontrada: {Codigo: "identidade.user.atribuicao_nao_encontrada", Mensagem: "Atribuição não encontrada para este usuário.", Status: http.StatusNotFound},
	ErrWorkspaceInvalido:       {Codigo: "identidade.user.workspace_invalido", Mensagem: "Workspace inexistente ou inativo nesta organization.", Status: http.StatusUnprocessableEntity},
	ErrRefreshTokenInvalido:    {Codigo: "identidade.user.refresh_token_invalido", Mensagem: "Sessão inválida ou expirada.", Status: http.StatusUnauthorized},

	// Sentinelas de invariante vindas do pacote model (folha).
	modeluser.ErrEmailInvalido:      {Codigo: "identidade.user.email_invalido", Mensagem: "E-mail fora do formato esperado.", Status: http.StatusBadRequest},
	modeluser.ErrNomeInvalido:       {Codigo: "identidade.user.nome_invalido", Mensagem: "Nome do usuário fora do formato esperado.", Status: http.StatusBadRequest},
	modeluser.ErrSenhaInvalida:      {Codigo: "identidade.user.senha_invalida", Mensagem: "Senha fora da política de credenciais.", Status: http.StatusBadRequest},
	modeluser.ErrHashAusente:        {Codigo: "identidade.user.hash_ausente", Mensagem: "Credencial não processada pelo subdomínio.", Status: http.StatusInternalServerError},
	modeluser.ErrJaInativo:          {Codigo: "identidade.user.ja_inativo", Mensagem: "Usuário já está inativo.", Status: http.StatusUnprocessableEntity},
	modeluser.ErrJaAtivo:            {Codigo: "identidade.user.ja_ativo", Mensagem: "Usuário já está ativo.", Status: http.StatusUnprocessableEntity},
	modeluser.ErrAtribuicaoInvalida: {Codigo: "identidade.user.atribuicao_invalida", Mensagem: "Atribuição incompleta: informe usuário, organization, workspace e papel.", Status: http.StatusBadRequest},
	modeluser.ErrRefreshInvalido:    {Codigo: "identidade.user.dados_sessao_invalidos", Mensagem: "Dados da sessão fora do formato esperado.", Status: http.StatusBadRequest},
}

func init() {
	rest_err.RegistrarCatalogo(modeluser.Dominio, modeluser.Subdominio, errorCatalog)
}
