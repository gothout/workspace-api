package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"workspace-api/internal/pkg/orgctx"
)

// resolvedorApiKeysFalso implementa o contrato de X-Api-Key em memória.
type resolvedorApiKeysFalso struct {
	porChave map[string]*IdentidadeChave
}

func (f *resolvedorApiKeysFalso) BuscarPorChave(_ context.Context, chave string) (*IdentidadeChave, error) {
	if f.porChave == nil {
		return nil, ErrNaoEncontrado
	}
	a, ok := f.porChave[chave]
	if !ok {
		return nil, ErrNaoEncontrado
	}
	return a, nil
}

// cadeiaCheia monta uma cadeia REAL inicializada com dublês dos contratos.
func cadeiaCheia(t *testing.T) (*Cadeia, *resolvedorWorkspacesFalso, *resolvedorPermissoesFalso) {
	t.Helper()
	configTeste(t, "exemplo.com")

	ws := novoResolvedorWorkspacesFalso()
	ws.registrar(WorkspaceResolvido{
		UUID:             wsFilialSul,
		OrganizationUUID: orgPlataforma,
		Slug:             "filial-sul",
		Ativo:            true,
	})
	perms := &resolvedorPermissoesFalso{
		vinculo: func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (bool, error) {
			return true, nil
		},
		efetivas: func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) ([]string, error) {
			return []string{"identidade:workspace:ler", "identidade:user:ler"}, nil
		},
	}
	cadeia := &Cadeia{deps: Dependencias{
		JWT:        managerTeste(t),
		Workspaces: ws,
		Permissoes: perms,
	}}
	return cadeia, ws, perms
}

// --- Passo 1: autenticação ---------------------------------------------------

func TestAuthSemCredencialReprova401(t *testing.T) {
	cadeia, _, _ := cadeiaCheia(t)
	resp, corpo := executar(t, rotaProtegida(cadeia, ""), "filial-sul.exemplo.com", nil)

	assert.Equal(t, http.StatusUnauthorized, resp.Code)
	assert.Equal(t, "sistema.unauthorized", corpo.Code)
}

func TestAuthTokenInvalidoReprova401SemDistinguirMotivo(t *testing.T) {
	cadeia, _, _ := cadeiaCheia(t)
	resp, corpo := executar(t, rotaProtegida(cadeia, ""),
		"filial-sul.exemplo.com",
		map[string]string{"Authorization": "Bearer lixo.totalmente.invalido"})

	assert.Equal(t, http.StatusUnauthorized, resp.Code)
	assert.Equal(t, "sistema.unauthorized", corpo.Code)
}

func TestAuthRefreshTokenNaoAbreRotaDeAcesso(t *testing.T) {
	configTeste(t, "exemplo.com")
	m := managerTeste(t)
	refresh, _, _, err := m.EmitirRefresh(jwtEntrada(userMaria, orgPlataforma))
	require.NoError(t, err)

	cadeia := &Cadeia{deps: Dependencias{JWT: m,
		Workspaces: novoResolvedorWorkspacesFalso(),
		Permissoes: &resolvedorPermissoesFalso{}}}
	resp, corpo := executar(t, rotaProtegida(cadeia, ""),
		"filial-sul.exemplo.com",
		map[string]string{"Authorization": "Bearer " + refresh})

	assert.Equal(t, http.StatusUnauthorized, resp.Code, "refresh não serve como access")
	assert.Equal(t, "sistema.unauthorized", corpo.Code)
}

func TestAuthValidoInjetaIdentidadeNoCtx(t *testing.T) {
	cadeia, _, _ := cadeiaCheia(t)
	token := tokenAcesso(t, cadeia.deps.JWT, userMaria, orgPlataforma)

	resp, corpo := executar(t, rotaProtegida(cadeia, "identidade:workspace:ler"),
		"filial-sul.exemplo.com",
		map[string]string{"Authorization": "Bearer " + token})

	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	assert.Equal(t, userMaria.String(), corpo.User)
	assert.Equal(t, wsFilialSul.String(), corpo.Workspace)
	assert.Contains(t, corpo.Permissoes, "identidade:workspace:ler")
}

func TestAuthApiKeySemResolvedorFalhaFechada(t *testing.T) {
	cadeia, _, _ := cadeiaChave(t, nil)
	resp, corpo := executar(t, rotaProtegida(cadeia, "identidade:workspace:ler"),
		"filial-sul.exemplo.com",
		map[string]string{"X-Api-Key": "chave-qualquer"})

	assert.Equal(t, http.StatusUnauthorized, resp.Code, "sem resolvedor ligado toda X-Api-Key falha 401")
	assert.Equal(t, "sistema.unauthorized", corpo.Code)
}

func TestAuthApiKeyDesconhecidaReprova401(t *testing.T) {
	resolvedor := &resolvedorApiKeysFalso{}
	cadeia, _, _ := cadeiaChave(t, resolvedor)
	resp, corpo := executar(t, rotaProtegida(cadeia, "identidade:workspace:ler"),
		"filial-sul.exemplo.com",
		map[string]string{"X-Api-Key": "nao-registrada"})

	assert.Equal(t, http.StatusUnauthorized, resp.Code)
	assert.Equal(t, "sistema.unauthorized", corpo.Code)
}

func TestAuthRayTraceVemDoHeaderOuENovo(t *testing.T) {
	cadeia, _, _ := cadeiaCheia(t)
	token := tokenAcesso(t, cadeia.deps.JWT, userMaria, orgPlataforma)

	gin.SetMode(gin.TestMode)
	var capturado string
	engine := gin.New()
	engine.GET("/teste", cadeia.SetContextAuthorization(), func(c *gin.Context) {
		capturado = orgctx.RayTrace(c.Request.Context())
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "http://filial-sul.exemplo.com/teste", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Request-Id", "ray-123")
	resp := httptest.NewRecorder()
	engine.ServeHTTP(resp, req)

	require.Equal(t, http.StatusOK, resp.Code)
	assert.Equal(t, "ray-123", capturado, "X-Request-Id presente é reaproveitado")
}

// --- Passo 3: RequirePermission ----------------------------------------------

func TestRequirePermissionExataLibera(t *testing.T) {
	cadeia, _, _ := cadeiaCheia(t)
	token := tokenAcesso(t, cadeia.deps.JWT, userMaria, orgPlataforma)

	resp, _ := executar(t, rotaProtegida(cadeia, "identidade:workspace:ler"),
		"filial-sul.exemplo.com",
		map[string]string{"Authorization": "Bearer " + token})
	assert.Equal(t, http.StatusOK, resp.Code)
}

func TestRequirePermissionAusenteNegaCom403(t *testing.T) {
	cadeia, _, _ := cadeiaCheia(t)
	token := tokenAcesso(t, cadeia.deps.JWT, userMaria, orgPlataforma)

	resp, corpo := executar(t, rotaProtegida(cadeia, "identidade:workspace:criar"),
		"filial-sul.exemplo.com",
		map[string]string{"Authorization": "Bearer " + token})

	assert.Equal(t, http.StatusForbidden, resp.Code)
	assert.Equal(t, "sistema.forbidden", corpo.Code)
}

func TestRequirePermissionExigenciaVaziaNegaSempre(t *testing.T) {
	cadeia, _, _ := cadeiaCheia(t) // usuário COM identidade:workspace:ler
	token := tokenAcesso(t, cadeia.deps.JWT, userMaria, orgPlataforma)

	resp, _ := executar(t, rotaProtegida(cadeia, ""),
		"filial-sul.exemplo.com",
		map[string]string{"Authorization": "Bearer " + token})

	assert.Equal(t, http.StatusForbidden, resp.Code, "exigência vazia nunca libera")
}

func TestRequirePermissionSemResolucaoAnteriorFecha(t *testing.T) {
	// RequirePermission sem ResolveWorkspace antes = cadeia declarada
	// incompleta → negativa (fail-closed), nunca aberta.
	configTeste(t, "exemplo.com")
	cadeia := &Cadeia{deps: Dependencias{JWT: managerTeste(t),
		Workspaces: novoResolvedorWorkspacesFalso(),
		Permissoes: &resolvedorPermissoesFalso{}}}

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/teste", cadeia.SetContextAuthorization(), // SEM resolução
		cadeia.RequirePermission("*:*"),
		func(c *gin.Context) { c.Status(http.StatusTeapot) })

	req := httptest.NewRequest(http.MethodGet, "http://painel.exemplo.com/teste", nil)
	req.Header.Set("Authorization", "Bearer "+tokenAcesso(t, cadeia.deps.JWT, userMaria, orgPlataforma))
	resp := httptest.NewRecorder()
	engine.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusForbidden, resp.Code)
}

func TestRequirePermissionCadeiaFechadaNega(t *testing.T) {
	resp, corpo := executar(t, rotaProtegida(&Cadeia{}, "*:*"), "filial-sul.exemplo.com", nil)
	assert.Equal(t, http.StatusForbidden, resp.Code)
	assert.Equal(t, "sistema.forbidden", corpo.Code)
}

// --- Matcher de curingas -------------------------------------------------------

func TestPermissaoAtendeCasos(t *testing.T) {
	casos := []struct {
		nome     string
		efetivas []string
		exigida  string
		libera   bool
	}{
		{"igualdade exata", []string{"identidade:workspace:ler"}, "identidade:workspace:ler", true},
		{"ação diferente no mesmo subdomínio", []string{"identidade:workspace:ler"}, "identidade:workspace:criar", false},
		{"curinga de ação do admin_workspace", []string{"identidade:user:*"}, "identidade:user:criar", true},
		{"curinga não vaza para outro subdomínio", []string{"identidade:user:*"}, "identidade:workspace:criar", false},
		{"super_admin atravessa tudo", []string{"*:*"}, "identidade:user:remover", true},
		{"curinga exige domínio igual", []string{"identidade:user:*"}, "billing:fatura:criar", false},
		{"permissão ausente", []string{"identidade:organization:gerenciar_apikeys"}, "identidade:workspace:ler", false},
		{"conjunto vazio nega", nil, "*:*", false},
		{"exigida malformada não é atendida por curinga", []string{"*:*"}, "duas-partes", false},
		{"efetiva malformada é ignorada", []string{"so-um-segmento"}, "identidade:workspace:ler", false},
		{"união de papéis atende", []string{"identidade:organization:ler", "identidade:workspace:editar"}, "identidade:workspace:editar", true},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			assert.Equal(t, caso.libera, permissaoAtende(caso.efetivas, caso.exigida))
		})
	}
}

// --- Falha de infraestrutura sobe como 500 -------------------------------------

func TestErroDoResolvedorSobeComo500(t *testing.T) {
	configTeste(t, "exemplo.com")
	ws := novoResolvedorWorkspacesFalso()
	ws.erroSlug = errors.New("postgres fora do ar")

	cadeia := &Cadeia{deps: Dependencias{JWT: managerTeste(t), Workspaces: ws,
		Permissoes: &resolvedorPermissoesFalso{}}}
	token := tokenAcesso(t, cadeia.deps.JWT, userMaria, orgPlataforma)

	resp, corpo := executar(t, rotaProtegida(cadeia, "identidade:workspace:ler"),
		"filial-sul.exemplo.com",
		map[string]string{"Authorization": "Bearer " + token})

	assert.Equal(t, http.StatusInternalServerError, resp.Code,
		"falha de infraestrutura não vira negativa de acesso")
	assert.Equal(t, "sistema.internal_server_error", corpo.Code)
}

// cadeiaChave monta cadeia com X-Api-Key opcionalmente ligada ao resolvedor.
func cadeiaChave(t *testing.T, r ResolvedorApiKeys) (*Cadeia, *resolvedorWorkspacesFalso, *resolvedorPermissoesFalso) {
	t.Helper()
	configTeste(t, "exemplo.com")
	ws := novoResolvedorWorkspacesFalso()
	ws.registrar(WorkspaceResolvido{
		UUID:             wsFilialSul,
		OrganizationUUID: orgPlataforma,
		Slug:             "filial-sul",
		Ativo:            true,
	})
	perms := &resolvedorPermissoesFalso{}
	return &Cadeia{deps: Dependencias{
		JWT:        managerTeste(t),
		Workspaces: ws,
		Permissoes: perms,
		ApiKeys:    r,
	}}, ws, perms
}
