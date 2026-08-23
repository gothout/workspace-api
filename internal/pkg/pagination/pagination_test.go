package pagination

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func contextoComQuery(t *testing.T, rawQuery string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodGet, "/?"+rawQuery, nil)
	c.Request = req
	return c
}

func TestDoQueryDefaultsTetoEInvalidos(t *testing.T) {
	casos := []struct {
		nome     string
		query    string
		page     int
		pageSize int
	}{
		{"sem query", "", 1, 10},
		{"valores válidos", "page=3&pageSize=25", 3, 25},
		{"teto de 100 aplicado", "page=1&pageSize=500", 1, 100},
		{"page não numérica cai no default", "page=abc&pageSize=20", 1, 20},
		{"pageSize negativa cai no default", "page=2&pageSize=-5", 2, 10},
		{"zero cai no default", "page=0&pageSize=0", 1, 10},
		{"vazio explícito cai no default", "page=&pageSize=", 1, 10},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			p := DoQuery(contextoComQuery(t, caso.query))
			assert.Equal(t, caso.page, p.Page)
			assert.Equal(t, caso.pageSize, p.Limit())
		})
	}
}

func TestOffsetCalculo(t *testing.T) {
	casos := []struct {
		pagina   int
		tamanho  int
		expected int
	}{
		{1, 10, 0},
		{2, 10, 10},
		{3, 25, 50},
		{0, 10, 0}, // página inválida tratada como primeira
	}
	for _, caso := range casos {
		p := Pagination{Page: caso.pagina, PageSize: caso.tamanho}
		assert.Equal(t, caso.expected, p.Offset(), "página %d tamanho %d", caso.pagina, caso.tamanho)
	}
}

func TestTotalPaginas(t *testing.T) {
	p := Pagination{Page: 1, PageSize: 10}
	assert.Equal(t, 14, p.TotalPaginas(137))
	assert.Equal(t, 1, p.TotalPaginas(0))
	assert.Equal(t, 1, p.TotalPaginas(5))
}

func TestNovaResponseEnvelopeSnakeCase(t *testing.T) {
	itens := []string{"a", "b"}
	resp := NovaResponse(itens, 137, Pagination{Page: 2, PageSize: 500})
	assert.Equal(t, itens, resp.Items)
	assert.Equal(t, 2, resp.Page)
	assert.Equal(t, 100, resp.PageSize, "pageSize no envelope já sai com o teto aplicado")
	assert.Equal(t, int64(137), resp.Total)
}

func TestNovaResponseItensNilViraListaVazia(t *testing.T) {
	var itens []string
	resp := NovaResponse(itens, 0, Pagination{})
	assert.NotNil(t, resp.Items)
	assert.Len(t, resp.Items, 0)
}
