package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Cadeia não inicializada = rota FECHADA: as três funções de pacote devolvem
// handlers que respondem 403 — nunca pânico, nunca rota aberta.
func TestCadeiaNaoInicializadaFechaTodaRota(t *testing.T) {
	ResetarParaTeste()

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/fechada",
		SetContextAuthorization(),
		ResolveWorkspace(),
		RequirePermission("identidade:workspace:ler"),
		func(c *gin.Context) { c.Status(http.StatusTeapot) })

	req := httptest.NewRequest(http.MethodGet, "http://filial-sul.exemplo.com/fechada", nil)
	resp := httptest.NewRecorder()
	engine.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusForbidden, resp.Code, "cadeia fechada responde 403")
	assert.Contains(t, resp.Body.String(), "sistema.forbidden")
}

func TestNewReprovaDependenciasAusentes(t *testing.T) {
	manager := managerTeste(t)

	// sync.Once consome na primeira tentativa: cada caso parte de estado limpo.
	ResetarParaTeste()
	require.Error(t, New(Dependencias{}), "sem JWT reprova")

	ResetarParaTeste()
	require.Error(t, New(Dependencias{JWT: manager}), "sem resolvedor de workspaces reprova")

	ResetarParaTeste()
	require.NoError(t, New(Dependencias{
		JWT:        manager,
		Workspaces: novoResolvedorWorkspacesFalso(),
		Permissoes: &resolvedorPermissoesFalso{},
	}))
}

// O singleton guarda UMA cadeia: segunda chamada de New devolve a mesma
// instância (sync.Once), e MustUse só aceita boot feito.
func TestCicloDoSingletonDaCadeia(t *testing.T) {
	ResetarParaTeste()
	defer ResetarParaTeste()

	assert.NotNil(t, Use(), "sem New, Use devolve a cadeia FECHADA — nunca nil")

	require.NoError(t, New(Dependencias{
		JWT:        managerTeste(t),
		Workspaces: novoResolvedorWorkspacesFalso(),
		Permissoes: &resolvedorPermissoesFalso{},
	}))
	primeira := Use()
	require.NoError(t, New(Dependencias{})) // sync.Once ignora re-chamada
	assert.Same(t, primeira, Use())

	assert.NotNil(t, MustUse())
}
