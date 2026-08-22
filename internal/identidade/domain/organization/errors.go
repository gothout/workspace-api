package organization

import (
	"errors"
	"net/http"

	orgmodel "workspace-api/internal/identidade/model/organization"
	"workspace-api/internal/pkg/rest_err"
)

// Sentinelas OPERACIONAIS do subdomínio — comparadas com errors.Is, nunca por
// texto. As sentinelas de INVARIANTE do modelo moram no pacote folha
// (internal/identidade/model/organization) e são registradas AQUI no catálogo.
// Regra de mapeamento sentinela → status: entrada inválida = 400;
// não encontrado = 404; conflito de UNICIDADE = 409; INVARIANTE de domínio
// violada = 422; desconhecido = 500.
var (
	ErrNotFound            = errors.New("organization não encontrada")
	ErrInvalidInput        = errors.New("dados de entrada inválidos")
	ErrDominioEmUso        = errors.New("domínio já está em uso por outra organization")
	ErrChaveEmUso          = errors.New("hash de chave de API já registrado")
	ErrApiKeyNaoEncontrada = errors.New("chave de API não encontrada nesta organization")
)

// errorCatalog é o contrato público de cada sentinela: código estável,
// mensagem PT-BR, status. Registrado no mapa global do rest_err pelo init() —
// alimenta GET /api/system/errors. Sentinela nova SEM entrada aqui não fecha
// o checklist do subdomínio.
var errorCatalog = map[error]rest_err.ErroCatalogado{
	ErrNotFound:            {Codigo: "identidade.organization.nao_encontrado", Mensagem: "Organization não encontrada.", Status: http.StatusNotFound},
	ErrInvalidInput:        {Codigo: "identidade.organization.entrada_invalida", Mensagem: "Dados de entrada inválidos.", Status: http.StatusBadRequest},
	ErrDominioEmUso:        {Codigo: "identidade.organization.dominio_em_uso", Mensagem: "Domínio já está em uso por outra organization.", Status: http.StatusConflict},
	ErrChaveEmUso:          {Codigo: "identidade.organization.apikey_chave_em_uso", Mensagem: "Hash de chave de API já registrado.", Status: http.StatusConflict},
	ErrApiKeyNaoEncontrada: {Codigo: "identidade.organization.apikey_nao_encontrada", Mensagem: "Chave de API não encontrada nesta organization.", Status: http.StatusNotFound},

	// Sentinelas de invariante vindas do pacote model (folha).
	orgmodel.ErrNomeInvalido:         {Codigo: "identidade.organization.nome_invalido", Mensagem: "Nome da organization fora do formato esperado.", Status: http.StatusBadRequest},
	orgmodel.ErrDominioInvalido:      {Codigo: "identidade.organization.dominio_invalido", Mensagem: "Domínio fora do formato DNS ou proibido (igual ao domínio-base, ascendente/descendente dele ou public suffix).", Status: http.StatusBadRequest},
	orgmodel.ErrDominioNaoDefinido:   {Codigo: "identidade.organization.dominio_nao_definido", Mensagem: "Organization não tem domínio custom definido.", Status: http.StatusNotFound},
	orgmodel.ErrJaInativo:            {Codigo: "identidade.organization.ja_inativa", Mensagem: "Registro já está inativo.", Status: http.StatusUnprocessableEntity},
	orgmodel.ErrJaAtivo:              {Codigo: "identidade.organization.ja_ativa", Mensagem: "Registro já está ativo.", Status: http.StatusUnprocessableEntity},
	orgmodel.ErrApiKeyInvalida:       {Codigo: "identidade.organization.apikey_invalida", Mensagem: "Dados da chave de API inválidos.", Status: http.StatusBadRequest},
	orgmodel.ErrPermissaoInvalida:    {Codigo: "identidade.organization.permissao_invalida", Mensagem: "Permissão fora do formato dominio:subdominio:acao.", Status: http.StatusBadRequest},
	orgmodel.ErrEscopoApiKeyInvalido: {Codigo: "identidade.organization.apikey_escopo_invalido", Mensagem: "Escopo da chave inválido: informe a organization inteira ou ao menos um workspace permitido.", Status: http.StatusBadRequest},
}

func init() {
	rest_err.RegistrarCatalogo(orgmodel.Dominio, orgmodel.Subdominio, errorCatalog)
}
