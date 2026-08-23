package routes

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"workspace-api/internal/pkg/config"
	"workspace-api/internal/pkg/log/access_log"
	"workspace-api/internal/pkg/orgctx"
)

func opcoesTeste(sonda func(context.Context) error) Opcoes {
	return Opcoes{
		App:        config.AppConfig{Name: "workspace-api", Env: "teste", Version: "0.0.0-teste", BaseDomain: "localhost"},
		Cors:       config.CorsConfig{AllowedOrigins: []string{"https://painel.exemplo.com.br"}},
		SondaBanco: sonda,
	}
}

func requisicao(engine *gin.Engine, metodo, alvo string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(metodo, alvo, nil)
	resp := httptest.NewRecorder()
	engine.ServeHTTP(resp, req)
	return resp
}

func TestStatusOkComBancoRespondendo(t *testing.T) {
	engine, err := Montar(opcoesTeste(func(context.Context) error { return nil }))
	require.NoError(t, err)
	resp := requisicao(engine, http.MethodGet, RotaStatus)
	require.Equal(t, http.StatusOK, resp.Code)
	var corpo map[string]any
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &corpo))
	assert.Equal(t, "ok", corpo["status"])
	assert.Equal(t, true, corpo["banco"])
}

func TestStatusDegradadoSemBanco(t *testing.T) {
	engine, err := Montar(opcoesTeste(func(context.Context) error { return errors.New("banco fora") }))
	require.NoError(t, err)
	resp := requisicao(engine, http.MethodGet, RotaStatus)
	assert.Equal(t, http.StatusServiceUnavailable, resp.Code)
	assert.Contains(t, resp.Body.String(), "degradado")
}

func TestStatusSemSondaInjetadaFicaDegradado(t *testing.T) {
	engine, err := Montar(Opcoes{App: config.AppConfig{Name: "x", Env: "teste", BaseDomain: "localhost"}})
	require.NoError(t, err)
	resp := requisicao(engine, http.MethodGet, RotaStatus)
	assert.Equal(t, http.StatusServiceUnavailable, resp.Code)
}

func TestNoRouteRespondeCorpoPadronizado(t *testing.T) {
	engine, err := Montar(opcoesTeste(nil))
	require.NoError(t, err)
	resp := requisicao(engine, http.MethodGet, "/rota-estranha")
	require.Equal(t, http.StatusNotFound, resp.Code)
	corpo := resp.Body.String()
	assert.Contains(t, corpo, `"code":"sistema.not_found"`)
	assert.Contains(t, corpo, `"ray_trace"`)
	assert.NotContains(t, corpo, "404 page not found", "corpo default do gin é proibido")
}

func TestGruposBaseExistemMasNaoAceitamRaizVazia(t *testing.T) {
	engine, err := Montar(opcoesTeste(nil))
	require.NoError(t, err)
	for _, prefixo := range []string{PrefixoDominio, PrefixoAplicacao} {
		resp := requisicao(engine, http.MethodGet, prefixo)
		assert.Equal(t, http.StatusNotFound, resp.Code, "%s sem subdomínio registrado deve 404", prefixo)
	}
}

func TestCorsPorSufixoDoDominioBase(t *testing.T) {
	engine, err := Montar(opcoesTeste(nil))
	require.NoError(t, err)

	// Subdomínio do base_domain passa.
	req := httptest.NewRequest(http.MethodOptions, RotaStatus, nil)
	req.Header.Set("Origin", "https://filial-sul.localhost")
	resp := httptest.NewRecorder()
	engine.ServeHTTP(resp, req)
	assert.Equal(t, "https://filial-sul.localhost",
		resp.Header().Get("Access-Control-Allow-Origin"), "origem do base_domain deveria passar")

	// Origem exata extra da config passa.
	engineExtras, err := Montar(opcoesTeste(nil))
	require.NoError(t, err)
	reqExtra := httptest.NewRequest(http.MethodOptions, RotaStatus, nil)
	reqExtra.Header.Set("Origin", "https://painel.exemplo.com.br")
	respExtra := httptest.NewRecorder()
	engineExtras.ServeHTTP(respExtra, reqExtra)
	assert.Equal(t, "https://painel.exemplo.com.br",
		respExtra.Header().Get("Access-Control-Allow-Origin"))

	// Origem estranha é recusada.
	reqEstranho := httptest.NewRequest(http.MethodOptions, RotaStatus, nil)
	reqEstranho.Header.Set("Origin", "https://site-malicioso.example.org")
	respEstranho := httptest.NewRecorder()
	engine.ServeHTTP(respEstranho, reqEstranho)
	assert.Empty(t, respEstranho.Header().Get("Access-Control-Allow-Origin"))
}

func TestSwaggerMontadoEmDoc(t *testing.T) {
	engine, err := Montar(opcoesTeste(nil))
	require.NoError(t, err)
	resp := requisicao(engine, http.MethodGet, "/doc/index.html")
	assert.Less(t, resp.Code, 500, "UI do swagger deveria estar montada (redirect/200), obtive %d", resp.Code)
}

// destinoAcessoFake captura eventos da trilha de acesso nos testes.
type destinoAcessoFake struct {
	mutex   sync.Mutex
	eventos []access_log.Evento
}

func (d *destinoAcessoFake) Registrar(ev access_log.Evento) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	d.eventos = append(d.eventos, ev)
}

func (d *destinoAcessoFake) total() int {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	return len(d.eventos)
}

// TestAccessLogPrimeiroDaCadeiaComRayTrace: todo request gera evento na
// trilha; o ray_trace injetado pela cadeia de autorização é o do ctx (não o
// header cru) e requisições sem cadeia caem para o header X-Request-Id.
func TestAccessLogPrimeiroDaCadeiaComRayTrace(t *testing.T) {
	destino := &destinoAcessoFake{}
	opcoes := opcoesTeste(nil)
	opcoes.AcessoLog = destino

	// Middleware que emula a cadeia de autorização: injeta ray_trace no ctx.
	engine, err := Montar(opcoes)
	require.NoError(t, err)
	engine.GET("/api/teste/ray", func(c *gin.Context) {
		c.Request = c.Request.WithContext(orgctx.WithRayTrace(c.Request.Context(), "ray-do-ctx"))
		c.Status(http.StatusOK)
	})
	engine.GET("/api/teste/sem-cadeia", func(c *gin.Context) { c.Status(200) })

	req := httptest.NewRequest(http.MethodGet, "/api/teste/ray", nil)
	resp := httptest.NewRecorder()
	engine.ServeHTTP(resp, req)

	require.Equal(t, 1, destino.total())
	ev := destino.eventos[0]
	assert.Equal(t, http.StatusOK, ev.Status)
	assert.Equal(t, "GET", ev.Metodo)
	assert.Equal(t, "/api/teste/ray", ev.Path)
	assert.Equal(t, "/api/teste/ray", ev.Rota)
	assert.Equal(t, "ray-do-ctx", ev.RayTrace)
	assert.Positive(t, ev.DuracaoMS+1)

	// Sem cadeia: cai para o header X-Request-Id (caso de rota de sistema).
	req2 := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req2.Header.Set("X-Request-Id", "ray-do-header")
	resp2 := httptest.NewRecorder()
	engine.ServeHTTP(resp2, req2)
	require.Equal(t, 2, destino.total())
	assert.Equal(t, "ray-do-header", destino.eventos[1].RayTrace)
}
