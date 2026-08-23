package logs

import (
	"errors"
	"net/http"

	modeluser "workspace-api/internal/identidade/model/user"
	"workspace-api/internal/pkg/rest_err"
)

// Dominio e Subdominio identificam esta aplicação nos catálogos — rotas em
// /api/application/identidade/logs/...
const (
	Dominio    = modeluser.Dominio // "identidade"
	Subdominio = "logs"
)

// Sentinelas da aplicação. Regra de mapeamento (doc 04): filtro malformado =
// 400; pedido fora do recorte do chamador = 404 NÃO-ENCONTRADO no escopo
// (não confirma existência de dados alheios); destino de telemetria ausente
// = 503 padronizado — nunca 500 nem lista vazia silenciosa.
var (
	ErrIndisponivel   = errors.New("consulta de logs indisponível: destino das trilhas ausente")
	ErrFiltroInvalido = errors.New("filtro de consulta inválido")
	ErrForaDoEscopo   = errors.New("recurso fora do escopo do chamador")
)

// errorCatalog é o contrato público de cada sentinela: código estável,
// mensagem PT-BR e status. Registrado no mapa global do rest_err pelo init()
// — alimenta GET /api/system/errors.
var errorCatalog = map[error]rest_err.ErroCatalogado{
	ErrFiltroInvalido: {Codigo: "identidade.logs.filtro_invalido", Mensagem: "Filtro de consulta inválido.", Status: http.StatusBadRequest},
	ErrForaDoEscopo:   {Codigo: "identidade.logs.fora_do_escopo", Mensagem: "Recurso não encontrado no seu escopo de acesso.", Status: http.StatusNotFound},
	ErrIndisponivel:   {Codigo: "identidade.logs.indisponivel", Mensagem: "Consulta de logs indisponível: destino de telemetria ausente.", Status: http.StatusServiceUnavailable},
}

func init() {
	rest_err.RegistrarCatalogo(Dominio, Subdominio, errorCatalog)
}
