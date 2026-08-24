package provisionamento

import (
	"errors"
	"net/http"

	"workspace-api/internal/pkg/rest_err"
)

const (
	Dominio    = "identidade"
	Subdominio = "provisionamento"
)

// Sentinelas OPERACIONAIS da aplicação — comparadas com errors.Is, nunca por
// texto. As sentinelas de INVARIANTE dos VOs (e-mail/senha/slug) moram nos
// pacotes model (folha) e são resolvidas pelo traduzir() via registro global;
// as recusas dos SUBDOMÍNIOS chegam traduzidas pelos adaptadores do
// cmd/bootstrap para o vocabulário daqui.
var (
	ErrInvalidInput             = errors.New("dados de entrada inválidos")
	ErrOrganizacaoNaoEncontrada = errors.New("organization não encontrada")
	ErrOrganizacaoInativa       = errors.New("organization está inativa")
	ErrJaProvisionado           = errors.New("organization já possui workspace provisionado")
	ErrSlugIndisponivel         = errors.New("slug já está em uso por outro workspace")
	ErrEmailEmUso               = errors.New("e-mail já cadastrado nesta organization")
	ErrSemPoderPlataforma       = errors.New("provisionamento é restrito à plataforma")
	ErrPapelAusente             = errors.New("papel admin_organization ausente — rode o seed")
)

// errorCatalog é o contrato público de cada sentinela: código estável,
// mensagem PT-BR, status. Registrado no mapa global do rest_err pelo init() —
// alimenta GET /api/system/errors. Sentinela nova SEM entrada aqui não fecha
// o checklist.
var errorCatalog = map[error]rest_err.ErroCatalogado{
	ErrInvalidInput:             {Codigo: "identidade.provisionamento.entrada_invalida", Mensagem: "Dados de entrada inválidos.", Status: http.StatusBadRequest},
	ErrOrganizacaoNaoEncontrada: {Codigo: "identidade.provisionamento.organization_nao_encontrada", Mensagem: "Organization não encontrada.", Status: http.StatusNotFound},
	ErrOrganizacaoInativa:       {Codigo: "identidade.provisionamento.organization_inativa", Mensagem: "A organization informada está inativa.", Status: http.StatusUnprocessableEntity},
	ErrJaProvisionado:           {Codigo: "identidade.provisionamento.ja_provisionado", Mensagem: "Organization já provisionada: ela já possui um workspace inicial.", Status: http.StatusConflict},
	ErrSlugIndisponivel:         {Codigo: "identidade.provisionamento.slug_em_uso", Mensagem: "Slug já está em uso por outro workspace.", Status: http.StatusConflict},
	ErrEmailEmUso:               {Codigo: "identidade.provisionamento.email_em_uso", Mensagem: "E-mail já cadastrado nesta organization.", Status: http.StatusConflict},
	ErrSemPoderPlataforma:       {Codigo: "identidade.provisionamento.sem_poder_plataforma", Mensagem: "Provisionamento de organizations é restrito à plataforma.", Status: http.StatusForbidden},
	ErrPapelAusente:             {Codigo: "identidade.provisionamento.papel_ausente", Mensagem: "Papel admin_organization ausente — rode o seed da plataforma.", Status: http.StatusInternalServerError},

	// As sentinelas de INVARIANTE dos VOs do user (e-mail fora do formato,
	// senha fora da política) NÃO entram aqui: o catálogo delas já é
	// registrado pelo subdomínio user — recadastrar daria code duplicado
	// (pânico no boot). O traduzir() as resolve pelo registro global via
	// errors.Is.
}

func init() {
	rest_err.RegistrarCatalogo(Dominio, Subdominio, errorCatalog)
}
