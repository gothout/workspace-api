package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"workspace-api/internal/infra/jwt"
	"workspace-api/internal/pkg/config"
	"workspace-api/internal/pkg/orgctx"
)

// --- Identidades e tokens de teste ------------------------------------------

var (
	orgPlataforma = uuid.MustParse("aaaaaaaa-0000-4000-8000-000000000001")
	wsFilialSul   = uuid.MustParse("bbbbbbbb-0000-4000-8000-000000000002")
	userMaria     = uuid.MustParse("cccccccc-0000-4000-8000-000000000003")
)

func managerTeste(t *testing.T) *jwt.Manager {
	t.Helper()
	m, err := jwt.Connect("segredo-suficientemente-longo-do-teste", 30, 24)
	require.NoError(t, err)
	return m
}

func tokenAcesso(t *testing.T, m *jwt.Manager, usuario, organizacao uuid.UUID) string {
	t.Helper()
	token, err := m.EmitirAcesso(jwt.EntradaToken{
		UserUUID:         usuario,
		OrganizationUUID: organizacao,
		Nome:             "Maria",
		Email:            "maria@example.com",
	})
	require.NoError(t, err)
	return token
}

// jwtEntrada monta entrada mínima para tokens de refresh nos testes.
func jwtEntrada(usuario, organizacao uuid.UUID) jwt.EntradaToken {
	return jwt.EntradaToken{UserUUID: usuario, OrganizationUUID: organizacao}
}

// --- Dublês dos contratos ----------------------------------------------------

type resolvedorWorkspacesFalso struct {
	mu         sync.Mutex
	porSlug    map[string]*WorkspaceResolvido
	erroSlug   error
	fixos      []string
	buscasSlug int
}

func novoResolvedorWorkspacesFalso() *resolvedorWorkspacesFalso {
	return &resolvedorWorkspacesFalso{
		porSlug: map[string]*WorkspaceResolvido{},
		fixos:   []string{"www", "api", "app", "admin", "docs", "status", "mail", "suporte", "painel"},
	}
}

func (f *resolvedorWorkspacesFalso) registrar(ws WorkspaceResolvido) {
	copia := ws
	f.porSlug[ws.Slug] = &copia
}

func (f *resolvedorWorkspacesFalso) BuscarPorSlug(_ context.Context, slug string) (*WorkspaceResolvido, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.buscasSlug++
	if f.erroSlug != nil {
		return nil, f.erroSlug
	}
	ws, ok := f.porSlug[slug]
	if !ok {
		return nil, ErrNaoEncontrado
	}
	return ws, nil
}

func (f *resolvedorWorkspacesFalso) BuscarPorUUID(_ context.Context, id uuid.UUID) (*WorkspaceResolvido, error) {
	for _, ws := range f.porSlug {
		if ws.UUID == id {
			return ws, nil
		}
	}
	return nil, ErrNaoEncontrado
}

func (f *resolvedorWorkspacesFalso) Fixos() []string { return f.fixos }

type provedorDominiosFalso struct{ lista []DominioCustom }

func (p *provedorDominiosFalso) Listar(context.Context) ([]DominioCustom, error) {
	return p.lista, nil
}

type resolvedorPermissoesFalso struct {
	vinculo  func(ctx context.Context, u, o, w uuid.UUID) (bool, error)
	efetivas func(ctx context.Context, u, o, w uuid.UUID) ([]string, error)
}

func (r *resolvedorPermissoesFalso) TemVinculo(ctx context.Context, u, o, w uuid.UUID) (bool, error) {
	if r.vinculo == nil {
		return false, nil
	}
	return r.vinculo(ctx, u, o, w)
}

func (r *resolvedorPermissoesFalso) PermissoesEfetivas(ctx context.Context, u, o, w uuid.UUID) ([]string, error) {
	if r.efetivas == nil {
		return []string{}, nil
	}
	return r.efetivas(ctx, u, o, w)
}

// --- Montagem de engine/cadeia de teste --------------------------------------

func configTeste(t *testing.T, baseDomain string) {
	t.Helper()
	diretorio := t.TempDir()
	arquivo := filepath.Join(diretorio, "configs.json")
	conteudo := `{
	  "app": {"name": "workspace-api", "env": "dev", "version": "0.1.0", "base_domain": "` + baseDomain + `"},
	  "server": {"http": {"port": 8080, "shutdown_timeout_sec": 5}},
	  "security": {"jwt_secret": "segredo-de-teste-suficiente!", "jwt_ttl_min": 30, "jwt_refresh_ttl_hours": 24},
	  "databases": {
	    "postgres": {"host": "localhost", "port": 5432, "name": "workspace"},
	    "migrations": {"path": "db/migrations"}
	  }
	}`
	require.NoError(t, os.WriteFile(arquivo, []byte(conteudo), 0o600))
	require.NoError(t, config.Init(arquivo))
	t.Cleanup(config.ResetarParaTeste)
}

// rotaProtegida monta a cadeia completa num GET com handler que ecoa o ctx.
func rotaProtegida(cadeia *Cadeia, permissao string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/teste",
		cadeia.SetContextAuthorization(),
		cadeia.ResolveWorkspace(),
		cadeia.RequirePermission(permissao),
		func(c *gin.Context) {
			ctx := c.Request.Context()
			c.JSON(http.StatusOK, gin.H{
				"user":       orgctx.UserUUID(ctx).String(),
				"workspace":  orgctx.WorkspaceUUID(ctx).String(),
				"permissoes": orgctx.Permissoes(ctx),
			})
		})
	return engine
}

func executar(t *testing.T, engine *gin.Engine, host string, cabecalhos map[string]string) (*httptest.ResponseRecorder, respostaCorpo) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "http://"+host+"/teste", nil)
	for k, v := range cabecalhos {
		req.Header.Set(k, v)
	}
	resp := httptest.NewRecorder()
	engine.ServeHTTP(resp, req)

	var corpo respostaCorpo
	_ = json.Unmarshal(resp.Body.Bytes(), &corpo)
	return resp, corpo
}

type respostaCorpo struct {
	Code       string   `json:"code"`
	Message    string   `json:"message"`
	User       string   `json:"user"`
	Workspace  string   `json:"workspace"`
	Permissoes []string `json:"permissoes"`
}
