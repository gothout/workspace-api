// Package rest_err define o corpo padrão de erro da API e o registro global
// de erros do sistema. O `code` é o identificador estável usado pelo
// front-end como mapping; o status HTTP vai no header, nunca no corpo.
//
// Cada subdomínio registra seu catálogo (sentinela → código estável +
// mensagem PT-BR + status) no init() do errors.go dele; código duplicado no
// registro entra em pânico — nunca sobrescrita silenciosa.
package rest_err

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// RestErr é o corpo de erro padronizado {code, error, message, ray_trace}.
// Status é interno (`json:"-"`): via no header da resposta, nunca no corpo.
type RestErr struct {
	Status   int    `json:"-"`
	Code     string `json:"code"`
	Error    string `json:"error"`
	Message  string `json:"message"`
	RayTrace string `json:"ray_trace"`

	causa error // preservada para o log, NUNCA serializada na resposta
}

// Causa devolve o erro original que gerou a resposta — para o log.
func (r *RestErr) Causa() error { return r.causa }

// --- Construtores padronizados (e variantes ComCausa) ---------------------

func NewBadRequestError(message string) *RestErr {
	return novo(http.StatusBadRequest, message)
}

func NewBadRequestErrorComCausa(message string, causa error) *RestErr {
	return comCausa(http.StatusBadRequest, message, causa)
}

func NewUnauthorizedError(message string) *RestErr {
	return novo(http.StatusUnauthorized, message)
}

func NewUnauthorizedErrorComCausa(message string, causa error) *RestErr {
	return comCausa(http.StatusUnauthorized, message, causa)
}

func NewForbiddenError(message string) *RestErr {
	return novo(http.StatusForbidden, message)
}

func NewForbiddenErrorComCausa(message string, causa error) *RestErr {
	return comCausa(http.StatusForbidden, message, causa)
}

func NewNotFoundError(message string) *RestErr {
	return novo(http.StatusNotFound, message)
}

func NewNotFoundErrorComCausa(message string, causa error) *RestErr {
	return comCausa(http.StatusNotFound, message, causa)
}

func NewConflictError(message string) *RestErr {
	return novo(http.StatusConflict, message)
}

func NewConflictErrorComCausa(message string, causa error) *RestErr {
	return comCausa(http.StatusConflict, message, causa)
}

func NewUnprocessableEntityError(message string) *RestErr {
	return novo(http.StatusUnprocessableEntity, message)
}

func NewUnprocessableEntityErrorComCausa(message string, causa error) *RestErr {
	return comCausa(http.StatusUnprocessableEntity, message, causa)
}

func NewInternalServerError(message string) *RestErr {
	return novo(http.StatusInternalServerError, message)
}

func NewInternalServerErrorComCausa(message string, causa error) *RestErr {
	return comCausa(http.StatusInternalServerError, message, causa)
}

func novo(status int, message string) *RestErr {
	return &RestErr{
		Status:  status,
		Code:    codigoDeSistema(status),
		Error:   nomeDoStatus(status),
		Message: message,
	}
}

func comCausa(status int, message string, causa error) *RestErr {
	rerr := novo(status, message)
	rerr.causa = causa
	return rerr
}

// Interno devolve o 500 genérico do sistema: mensagem fixa, causa logada e
// ray_trace correlacionado — o erro original nunca vaza na resposta.
func Interno(causa error) *RestErr {
	if causa == nil {
		causa = errors.New("causa desconhecida")
	}
	rerr := novo(http.StatusInternalServerError, "Erro interno do servidor.")
	rerr.causa = causa
	return rerr
}

// codigoDeSistema monta o code estável dos erros de sistema (sem subdomínio):
// `sistema.bad_request`, `sistema.internal_server_error`, ...
func codigoDeSistema(status int) string {
	return "sistema." + nomeDoStatus(status)
}

func nomeDoStatus(status int) string {
	texto := http.StatusText(status)
	if texto == "" {
		return fmt.Sprintf("http_%d", status)
	}
	return strings.ToLower(strings.ReplaceAll(texto, " ", "_"))
}

// --- Registro global de erros (alimenta GET /api/system/errors) ----------

// ErroCatalogado é o contrato público de uma sentinela: código estável,
// mensagem PT-BR e status HTTP.
type ErroCatalogado struct {
	Codigo   string
	Mensagem string
	Status   int
}

// EntradaErro é a forma pública de um erro no mapa de erros (doc 04).
type EntradaErro struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"status"`
}

// GrupoErros agrupa os erros por domínio/subdomínio.
type GrupoErros struct {
	Dominio    string        `json:"dominio"`
	Subdominio string        `json:"subdominio"`
	Erros      []EntradaErro `json:"erros"`
}

var (
	registroMu sync.RWMutex
	registro   = map[string]map[error]ErroCatalogado{} // chave "dominio.subdominio" → catálogo
)

// RegistrarCatalogo inscreve o catálogo de um subdomínio no registro global.
// Chamado pelo init() do errors.go de cada subdomínio; código duplicado no
// registro = pânico (nunca sobrescrita) — só roda em tempo de boot.
func RegistrarCatalogo(dominio, subdominio string, catalogo map[error]ErroCatalogado) {
	registroMu.Lock()
	defer registroMu.Unlock()
	for _, entrada := range catalogo {
		if codigoRegistrado(entrada.Codigo) {
			panic(fmt.Sprintf("rest_err: código de erro duplicado %q (%s/%s já registrado antes)", entrada.Codigo, dominio, subdominio))
		}
	}
	registro[dominio+"."+subdominio] = catalogo
}

// codigoRegistrado procura um código em todo o registro; chamadora segura o lock.
func codigoRegistrado(codigo string) bool {
	for _, catalogo := range registro {
		for _, entrada := range catalogo {
			if entrada.Codigo == codigo {
				return true
			}
		}
	}
	return false
}

// DoCatalogo resolve a sentinela no registro global e devolve o RestErr com
// código estável, mensagem e status do catálogo. Sentinela sem entrada cai no
// 500 genérico do sistema (fail-closed: erro desconhecido não vira resposta
// improvisada).
func DoCatalogo(sentinela error) *RestErr {
	if sentinela == nil {
		return Interno(errors.New("DoCatalogo chamado com erro nulo"))
	}
	registroMu.RLock()
	defer registroMu.RUnlock()
	for _, catalogo := range registro {
		for candidata, entrada := range catalogo {
			if errors.Is(sentinela, candidata) {
				return &RestErr{
					Status:  entrada.Status,
					Code:    entrada.Codigo,
					Error:   nomeDoStatus(entrada.Status),
					Message: entrada.Mensagem,
					causa:   sentinela,
				}
			}
		}
	}
	return Interno(fmt.Errorf("erro sem entrada no catálogo: %w", sentinela))
}

// MapaErros devolve todos os erros registrados agrupados por domínio/subdomínio,
// em ordem determinística — é o corpo de GET /api/system/errors (doc 04).
func MapaErros() []GrupoErros {
	registroMu.RLock()
	defer registroMu.RUnlock()
	grupos := make([]GrupoErros, 0, len(registro))
	for chave, catalogo := range registro {
		parts := strings.SplitN(chave, ".", 2)
		dominio, subdominio := parts[0], parts[1]
		entradas := make([]EntradaErro, 0, len(catalogo))
		vistos := map[string]bool{}
		for _, entrada := range catalogo {
			if vistos[entrada.Codigo] {
				continue
			}
			vistos[entrada.Codigo] = true
			entradas = append(entradas, EntradaErro{Code: entrada.Codigo, Message: entrada.Mensagem, Status: entrada.Status})
		}
		sort.Slice(entradas, func(i, j int) bool { return entradas[i].Code < entradas[j].Code })
		grupos = append(grupos, GrupoErros{Dominio: dominio, Subdominio: subdominio, Erros: entradas})
	}
	sort.Slice(grupos, func(i, j int) bool {
		if grupos[i].Dominio != grupos[j].Dominio {
			return grupos[i].Dominio < grupos[j].Dominio
		}
		return grupos[i].Subdominio < grupos[j].Subdominio
	})
	return grupos
}

// ResetarRegistroParaTeste limpa o registro global — uso EXCLUSIVO dos testes.
func ResetarRegistroParaTeste() {
	registroMu.Lock()
	defer registroMu.Unlock()
	registro = map[string]map[error]ErroCatalogado{}
}

// --- Escrita da resposta ---------------------------------------------------

// WriteError é A ÚNICA forma de responder erro num controller/middleware:
// garante corpo padronizado, status no header, ray_trace correlacionado e
// log estruturado da causa (quando existir).
func WriteError(c *gin.Context, rerr *RestErr) {
	if rerr == nil {
		rerr = Interno(errors.New("WriteError chamado com erro nulo"))
	}
	if rerr.RayTrace == "" {
		rerr.RayTrace = rayTrace(c)
	}
	if rerr.Status >= 500 {
		slog.ErrorContext(c.Request.Context(), "requisicao.falhou",
			"code", rerr.Code, "status", rerr.Status, "ray_trace", rerr.RayTrace,
			"causa", textoDaCausa(rerr))
	} else {
		slog.WarnContext(c.Request.Context(), "requisicao.recusada",
			"code", rerr.Code, "status", rerr.Status, "ray_trace", rerr.RayTrace)
	}
	c.AbortWithStatusJSON(rerr.Status, rerr)
}

func textoDaCausa(rerr *RestErr) string {
	if rerr.causa == nil {
		return ""
	}
	return rerr.causa.Error()
}

// rayTrace reaproveita o X-Request-Id quando o cliente/enviado por upstream;
// se ausente, gera um uuid novo.
func rayTrace(c *gin.Context) string {
	if id := c.GetHeader("X-Request-Id"); id != "" {
		return id
	}
	return uuid.NewString()
}
