package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"workspace-api/internal/pkg/orgctx"
)

// resolvedorAplicacoesFalso simula o contrato ResolvedorAplicacoes.
type resolvedorAplicacoesFalso struct {
	liberadas []string
	err       error
}

func (f resolvedorAplicacoesFalso) Liberadas(_ context.Context, org, ws uuid.UUID) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	if org == uuid.Nil || ws == uuid.Nil {
		return nil, nil // espelha o resolvedor real: sem par, sem módulo liberado
	}
	return f.liberadas, nil
}

// executarAplicacao roda o passo com o par (org, ws) pré-injetado no ctx —
// o ResolveWorkspace real já os resolveu; aqui o teste foca SÓ o novo passo.
func executarAplicacao(t *testing.T, cadeia *Cadeia, exigido string, header string, org, ws uuid.UUID) (*httptest.ResponseRecorder, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/app", cadeia.RequireAplicacao(exigido), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"aplicacao": orgctx.Aplicacao(c.Request.Context())})
	})
	req := httptest.NewRequest(http.MethodGet, "http://x/app", nil)
	if header != "" {
		req.Header.Set(CabecalhoAplicacao, header)
	}
	ctx := req.Context()
	if org != uuid.Nil || ws != uuid.Nil {
		ctx = orgctx.WithOrganization(ctx, org)
		ctx = orgctx.WithWorkspace(ctx, ws)
	}
	req = req.WithContext(ctx)
	resp := httptest.NewRecorder()
	engine.ServeHTTP(resp, req)
	return resp, resp.Body.String()
}

func TestRequireAplicacaoLiberaECasaHeader(t *testing.T) {
	org, ws := uuid.New(), uuid.New()
	cadeia := &Cadeia{deps: Dependencias{
		Aplicacoes: resolvedorAplicacoesFalso{liberadas: []string{"todolist", "crm"}},
	}}

	resp, corpo := executarAplicacao(t, cadeia, "todolist", "todolist", org, ws)
	assert.Equal(t, http.StatusOK, resp.Code, corpo)
	assert.Contains(t, corpo, "todolist")

	// Header divergente do exigido pela rota: 403 — mesmo que exista módulo
	// liberado com aquele slug.
	resp, _ = executarAplicacao(t, cadeia, "todolist", "crm", org, ws)
	assert.Equal(t, http.StatusForbidden, resp.Code)

	// Header ausente: 403.
	resp, _ = executarAplicacao(t, cadeia, "todolist", "", org, ws)
	assert.Equal(t, http.StatusForbidden, resp.Code)
}

func TestRequireAplicacaoNegaSemLicencaSemVazarExistencia(t *testing.T) {
	org, ws := uuid.New(), uuid.New()

	t.Run("módulo inexistente e sem licença respondem o mesmo 403", func(t *testing.T) {
		cadeia := &Cadeia{deps: Dependencias{
			Aplicacoes: resolvedorAplicacoesFalso{liberadas: []string{"todolist"}},
		}}
		semLicenca, corpoSemLicenca := executarAplicacao(t, cadeia, "crm", "crm", org, ws)
		inexistente, corpoInexistente := executarAplicacao(t, cadeia, "fantasma", "fantasma", org, ws)
		assert.Equal(t, http.StatusForbidden, semLicenca.Code)
		assert.Equal(t, http.StatusForbidden, inexistente.Code)
		// ray_trace difere por requisição: a MENSAGEM é o que não pode vazar
		// diferença entre "não existe" e "sem licença".
		msgSemLicenca := extrairMensagem(t, corpoSemLicenca)
		msgInexistente := extrairMensagem(t, corpoInexistente)
		assert.Equal(t, msgSemLicenca, msgInexistente, "mensagem idêntica: não vaza existência")
	})

	t.Run("falha de infra sobe 500, nunca 403", func(t *testing.T) {
		cadeia := &Cadeia{deps: Dependencias{
			Aplicacoes: resolvedorAplicacoesFalso{err: assert.AnError},
		}}
		resp, _ := executarAplicacao(t, cadeia, "todolist", "todolist", org, ws)
		assert.Equal(t, http.StatusInternalServerError, resp.Code)
	})

	t.Run("sem par resolvido nega", func(t *testing.T) {
		cadeia := &Cadeia{deps: Dependencias{
			Aplicacoes: resolvedorAplicacoesFalso{liberadas: []string{"todolist"}},
		}}
		resp, _ := executarAplicacao(t, cadeia, "todolist", "todolist", uuid.Nil, uuid.Nil)
		assert.Equal(t, http.StatusForbidden, resp.Code)
	})
}

func TestRequireAplicacaoCadeiaFechada(t *testing.T) {
	// Cadeia sem peça ligada (boot quebrado): passo responde 403 fechado.
	cadeia := &Cadeia{}
	resp, _ := executarAplicacao(t, cadeia, "todolist", "todolist", uuid.New(), uuid.New())
	assert.Equal(t, http.StatusForbidden, resp.Code)
}

func TestContemSlugExato(t *testing.T) {
	require.True(t, contemSlug([]string{"todolist", "crm"}, "todolist"))
	assert.False(t, contemSlug([]string{"todolist"}, "todo"), "prefixo não passa")
	assert.False(t, contemSlug([]string{"todolist"}, "todolist-x"), "sufixo não passa")
	assert.False(t, contemSlug(nil, "todolist"))
}

// extrairMensagem isola o campo message do corpo padronizado (ray_trace é
// único por requisição e não participa da comparação de indistinguibilidade).
func extrairMensagem(t *testing.T, corpo string) string {
	t.Helper()
	var parsed struct {
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal([]byte(corpo), &parsed))
	return parsed.Message
}
