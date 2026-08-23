package logs

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"workspace-api/internal/pkg/pagination"
)

// rotasDeLeituraRegistradas confere que as TRÊS trilhas estão declaradas
// rota a rota com o caminho do contrato (doc 04).
func TestRotasDeLeituraRegistradasRotaARota(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctrl := NewController(NewService(&consultorFake{}))
	engine := gin.New()
	ctrl.Routes(engine.Group("/api/application"))

	registradas := map[string]bool{}
	for _, r := range engine.Routes() {
		registradas[r.Method+" "+r.Path] = true
	}
	for _, trilha := range []string{"auditoria", "acesso", "erros"} {
		caminho := "/api/application" + PrefixoRotas + "/" + trilha
		assert.True(t, registradas["GET "+caminho], "rota %s ausente", caminho)
	}
}

// TestCadeiaNaoInicializadaFechaAsRotas: engine montado SEM boot responde
// 403 em toda rota de leitura — fail-closed da cadeia de pacote, nunca
// pânico nem rota aberta.
func TestCadeiaNaoInicializadaFechaAsRotas(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctrl := NewController(NewService(&consultorFake{}))
	engine := gin.New()
	ctrl.Routes(engine.Group("/api/application"))

	for _, trilha := range []string{"auditoria", "acesso", "erros"} {
		req := httptest.NewRequest(http.MethodGet, "/api/application"+PrefixoRotas+"/"+trilha, nil)
		resp := httptest.NewRecorder()
		engine.ServeHTTP(resp, req)
		assert.Equal(t, http.StatusForbidden, resp.Code, "trilha %s deveria fechar 403 sem cadeia", trilha)
	}
}

// guarda de compilação: o envelope padrão continua sendo o do pacote pagination.
var _ = pagination.NovaResponse[AuditoriaItemDto]
