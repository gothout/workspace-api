// Package pagination padroniza a paginação das listagens da API (doc 04):
// query em camelCase (`page`, `pageSize`), resposta em snake_case
// (`items`, `page`, `page_size`, `total`). Valores inválidos caem nos
// defaults e o teto de 100 itens por página é aplicado aqui — paginação
// ruim nunca derruba uma listagem.
package pagination

import (
	"math"
	"strconv"

	"github.com/gin-gonic/gin"
)

const (
	// DefaultPage e DefaultPageSize são os defaults do contrato (doc 04).
	DefaultPage     = 1
	DefaultPageSize = 10

	// MaxPageSize é o teto duro por página: acima disso, clamp para 100.
	MaxPageSize = 100
)

// Pagination carrega a página corrente e o tamanho solicitado pelo cliente.
type Pagination struct {
	Page     int
	PageSize int
}

// Offset devolve o OFFSET pronto para o repository.
func (p Pagination) Offset() int {
	if p.Page <= 1 {
		return 0
	}
	return (p.Page - 1) * p.Limit()
}

// Limit devolve o LIMIT já com o teto aplicado.
func (p Pagination) Limit() int {
	if p.PageSize <= 0 {
		return DefaultPageSize
	}
	if p.PageSize > MaxPageSize {
		return MaxPageSize
	}
	return p.PageSize
}

// TotalPaginas calcula o total de páginas para o front montar o paginador.
func (p Pagination) TotalPaginas(total int64) int {
	pages := int(math.Ceil(float64(total) / float64(p.Limit())))
	if pages < 1 {
		return 1
	}
	return pages
}

// DoQuery lê `page`/`pageSize` da query string aplicando defaults e teto.
// Valor inválido (não numérico, negativo, zero) cai no default — nunca erro.
func DoQuery(c *gin.Context) Pagination {
	return Nova(c.Query("page"), c.Query("pageSize"))
}

// Nova aplica as regras de paginação sobre valores crus — testável sem gin.
func Nova(pageRaw, pageSizeRaw string) Pagination {
	return Pagination{
		Page:     inteiroOuDefault(pageRaw, DefaultPage),
		PageSize: inteiroOuDefault(pageSizeRaw, DefaultPageSize),
	}
}

func inteiroOuDefault(raw string, def int) int {
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

// Response é o envelope padrão das respostas de lista (doc 04).
type Response[T any] struct {
	Items    []T   `json:"items"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
	Total    int64 `json:"total"`
}

// NovaResponse monta o envelope com snake_case garantido.
func NovaResponse[T any](items []T, total int64, p Pagination) Response[T] {
	if items == nil {
		items = []T{}
	}
	return Response[T]{
		Items:    items,
		Page:     p.Page,
		PageSize: p.Limit(),
		Total:    total,
	}
}
