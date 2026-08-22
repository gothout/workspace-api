package catalogo

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"workspace-api/internal/pkg/orgctx"
	"workspace-api/internal/pkg/rest_err"
)

func engineDoController(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	ctrl := NewController(NewService(provedorFalso{itens: catalogoFalso()}))
	ctrl.RoutesSistema(engine)
	grupos := engine.Group("/api/application")
	ctrl.Routes(grupos)
	return engine
}

func TestRotaErrosPublicaRespondeMapa(t *testing.T) {
	rest_err.ResetarRegistroParaTeste()
	defer rest_err.ResetarRegistroParaTeste()

	req := httptest.NewRequest(http.MethodGet, RotaErrosSistema, nil)
	resp := httptest.NewRecorder()
	engineDoController(t).ServeHTTP(resp, req)

	require.Equal(t, http.StatusOK, resp.Code, "rota de sistema é pública por decisão")
	var corpo map[string]any
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &corpo))
	assert.Contains(t, corpo, "erros", "contrato do doc 04: {erros: [...]}")
}

func TestPermissoesMinhasSemBootFecha403(t *testing.T) {
	// Sem middleware.New no boot a cadeia FECHADA responde 403 — fail-closed
	// testável (AGENTS.md do middleware); nunca rota aberta.
	req := httptest.NewRequest(http.MethodGet, "/api/application"+PrefixoRotas+"/permissoes/minhas", nil)
	resp := httptest.NewRecorder()
	engineDoController(t).ServeHTTP(resp, req)
	assert.Equal(t, http.StatusForbidden, resp.Code)
}

func TestPermissoesMinhasComIdentidadeNoCtxRespondeArvore(t *testing.T) {
	// O handler é FINO: com o ctx já escopado pelo middleware (simulado aqui
	// injetando as permissões direto no request), devolve a árvore filtrada.
	// A rota é montada SEM a cadeia — quem testa a cadeia é o middleware;
	// aqui interessa o contrato do handler.
	rest_err.ResetarRegistroParaTeste()
	defer rest_err.ResetarRegistroParaTeste()

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	ctxComPermissao := func(c *gin.Context) {
		c.Request = c.Request.WithContext(
			orgctx.WithPermissoes(c.Request.Context(), []string{"identidade:user:ler"}))
		c.Next()
	}
	ctrl := NewController(NewService(provedorFalso{itens: catalogoFalso()}))
	engine.GET("/api/application"+PrefixoRotas+"/permissoes/minhas", ctxComPermissao, ctrl.MinhasPermissoes)

	req := httptest.NewRequest(http.MethodGet, "/api/application"+PrefixoRotas+"/permissoes/minhas", nil)
	resp := httptest.NewRecorder()
	engine.ServeHTTP(resp, req)

	require.Equal(t, http.StatusOK, resp.Code)
	var arvore ArvoreResponseDto
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &arvore))
	require.Len(t, arvore.Dominios, 1)
	assert.Equal(t, "user", arvore.Dominios[0].Subdominios[0].Subdominio)
}
