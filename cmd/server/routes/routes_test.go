package routes

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"workspace-api/internal/pkg/config"
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
	engine := Montar(opcoesTeste(func(context.Context) error { return nil }))
	resp := requisicao(engine, http.MethodGet, RotaStatus)
	require.Equal(t, http.StatusOK, resp.Code)
	var corpo map[string]any
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &corpo))
	assert.Equal(t, "ok", corpo["status"])
	assert.Equal(t, true, corpo["banco"])
}

func TestStatusDegradadoSemBanco(t *testing.T) {
	engine := Montar(opcoesTeste(func(context.Context) error { return errors.New("banco fora") }))
	resp := requisicao(engine, http.MethodGet, RotaStatus)
	assert.Equal(t, http.StatusServiceUnavailable, resp.Code)
	assert.Contains(t, resp.Body.String(), "degradado")
}

func TestStatusSemSondaInjetadaFicaDegradado(t *testing.T) {
	engine := Montar(Opcoes{App: config.AppConfig{Name: "x", Env: "teste", BaseDomain: "localhost"}})
	resp := requisicao(engine, http.MethodGet, RotaStatus)
	assert.Equal(t, http.StatusServiceUnavailable, resp.Code)
}

func TestNoRouteRespondeCorpoPadronizado(t *testing.T) {
	engine := Montar(opcoesTeste(nil))
	resp := requisicao(engine, http.MethodGet, "/rota-estranha")
	require.Equal(t, http.StatusNotFound, resp.Code)
	corpo := resp.Body.String()
	assert.Contains(t, corpo, `"code":"sistema.not_found"`)
	assert.Contains(t, corpo, `"ray_trace"`)
	assert.NotContains(t, corpo, "404 page not found", "corpo default do gin é proibido")
}

func TestGruposBaseExistemMasNaoAceitamRaizVazia(t *testing.T) {
	engine := Montar(opcoesTeste(nil))
	for _, prefixo := range []string{PrefixoDominio, PrefixoAplicacao} {
		resp := requisicao(engine, http.MethodGet, prefixo)
		assert.Equal(t, http.StatusNotFound, resp.Code, "%s sem subdomínio registrado deve 404", prefixo)
	}
}

func TestCorsPorSufixoDoDominioBase(t *testing.T) {
	engine := Montar(opcoesTeste(nil))

	// Subdomínio do base_domain passa.
	req := httptest.NewRequest(http.MethodOptions, RotaStatus, nil)
	req.Header.Set("Origin", "https://filial-sul.localhost")
	resp := httptest.NewRecorder()
	engine.ServeHTTP(resp, req)
	assert.Equal(t, "https://filial-sul.localhost",
		resp.Header().Get("Access-Control-Allow-Origin"), "origem do base_domain deveria passar")

	// Origem exata extra da config passa.
	engineExtras := Montar(opcoesTeste(nil))
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
	engine := Montar(opcoesTeste(nil))
	resp := requisicao(engine, http.MethodGet, "/doc/index.html")
	assert.Less(t, resp.Code, 500, "UI do swagger deveria estar montada (redirect/200), obtive %d", resp.Code)
}
