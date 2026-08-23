package middleware

import (
	"context"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"workspace-api/internal/pkg/orgctx"
)

var (
	orgParceiro = uuid.MustParse("aaaaaaaa-0000-4000-8000-000000000099")
	wsLoja      = uuid.MustParse("bbbbbbbb-0000-4000-8000-000000000088")
	tokenMaria  = func(t *testing.T, c *Cadeia) string {
		return tokenAcesso(t, c.deps.JWT, userMaria, orgPlataforma)
	}
)

// cadeiaResolucao monta cadeia com workspaces da plataforma E do parceiro.
func cadeiaResolucao(t *testing.T) (*Cadeia, *resolvedorWorkspacesFalso) {
	t.Helper()
	cadeia, ws, _ := cadeiaCheia(t)
	ws.registrar(WorkspaceResolvido{UUID: wsLoja, OrganizationUUID: orgParceiro, Slug: "loja", Ativo: true})
	ws.registrar(WorkspaceResolvido{UUID: uuid.New(), OrganizationUUID: orgPlataforma, Slug: "pausada", Ativo: false})
	cadeia.deps.DominiosCustom = &provedorDominiosFalso{lista: []DominioCustom{
		{Dominio: "parceiro.com", OrganizationUUID: orgParceiro},
	}}
	return cadeia, ws
}

func TestHostDesconhecidoReprova404SemFallbackAberto(t *testing.T) {
	cadeia, _ := cadeiaResolucao(t)
	resp, corpo := executar(t, rotaProtegida(cadeia, ""),
		"filial-sul.dominio-alheio.com",
		map[string]string{"Authorization": "Bearer " + tokenMaria(t, cadeia)})

	assert.Equal(t, http.StatusNotFound, resp.Code,
		"host fora dos domínios conhecidos nunca resolve aberto")
	assert.Equal(t, "sistema.not_found", corpo.Code)
}

func TestResolucaoPorSufixoDaPlataforma(t *testing.T) {
	cadeia, _ := cadeiaResolucao(t)
	token := tokenMaria(t, cadeia)

	resp, corpo := executar(t, rotaProtegida(cadeia, "identidade:workspace:ler"),
		"filial-sul.exemplo.com",
		map[string]string{"Authorization": "Bearer " + token})

	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	assert.Equal(t, wsFilialSul.String(), corpo.Workspace)
	assert.Equal(t, userMaria.String(), corpo.User)
}

func TestResolucaoIgnoraPortaECaixaDoHost(t *testing.T) {
	cadeia, _ := cadeiaResolucao(t)
	token := tokenMaria(t, cadeia)

	for _, host := range []string{"FILIAL-SUL.exemplo.com:8080", "filial-sul.EXEMPLO.com"} {
		resp, corpo := executar(t, rotaProtegida(cadeia, "identidade:workspace:ler"), host,
			map[string]string{"Authorization": "Bearer " + token})
		require.Equal(t, http.StatusOK, resp.Code, host)
		assert.Equal(t, wsFilialSul.String(), corpo.Workspace, host)
	}
}

// White-label: {slug}.{dominio-custom} resolve quando o workspace pertence à
// organization DONA do domínio.
func TestResolucaoPorDominioCustomDaOrganization(t *testing.T) {
	cadeia, _ := cadeiaResolucao(t)
	token := tokenAcesso(t, cadeia.deps.JWT, userMaria, orgParceiro)

	resp, corpo := executar(t, rotaProtegida(cadeia, "identidade:workspace:ler"),
		"loja.parceiro.com",
		map[string]string{"Authorization": "Bearer " + token})

	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	assert.Equal(t, wsLoja.String(), corpo.Workspace)
}

// Workspace alheio no domínio do parceiro = 404: não vaza existência.
func TestWorkspaceAlheioNoDominioDoParceiroReprova404(t *testing.T) {
	cadeia, _ := cadeiaResolucao(t)
	// filial-sul pertence à plataforma, NÃO ao dono de parceiro.com
	token := tokenAcesso(t, cadeia.deps.JWT, userMaria, orgPlataforma)

	resp, corpo := executar(t, rotaProtegida(cadeia, ""),
		"filial-sul.parceiro.com",
		map[string]string{"Authorization": "Bearer " + token})

	assert.Equal(t, http.StatusNotFound, resp.Code)
	assert.Equal(t, "sistema.not_found", corpo.Code)
}

func TestWorkspaceInativoOuInexistenteReprova404(t *testing.T) {
	cadeia, _ := cadeiaResolucao(t)
	token := tokenMaria(t, cadeia)

	for _, caso := range []struct{ nome, host string }{
		{"inativo", "pausada.exemplo.com"},
		{"inexistente", "fantasma.exemplo.com"},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			resp, _ := executar(t, rotaProtegida(cadeia, ""), caso.host,
				map[string]string{"Authorization": "Bearer " + token})
			assert.Equal(t, http.StatusNotFound, resp.Code)
		})
	}
}

func TestSemVinculoComWorkspaceReprova403(t *testing.T) {
	cadeia, _, perms := cadeiaCheia(t)
	perms.vinculo = func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (bool, error) {
		return false, nil // sem atribuição e sem suporte
	}
	token := tokenMaria(t, cadeia)

	resp, corpo := executar(t, rotaProtegida(cadeia, ""),
		"filial-sul.exemplo.com",
		map[string]string{"Authorization": "Bearer " + token})

	assert.Equal(t, http.StatusForbidden, resp.Code)
	assert.Equal(t, "sistema.forbidden", corpo.Code)
}

// Acesso de suporte: TemVinculo devolve true pela concessão auditada e as
// permissões efetivas já carregam o papel de suporte.
func TestSuporteConcedeEntradaComPermissoesDoPapel(t *testing.T) {
	cadeia, _ := cadeiaResolucao(t)
	perms := cadeia.deps.Permissoes.(*resolvedorPermissoesFalso)
	perms.vinculo = func(_ context.Context, u, o, w uuid.UUID) (bool, error) {
		return true, nil // concessão de suporte detectada pelo adaptador
	}
	perms.efetivas = func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) ([]string, error) {
		return []string{"identidade:user:*"}, nil // permissões do papel admin
	}
	token := tokenMaria(t, cadeia)

	resp, corpo := executar(t, rotaProtegida(cadeia, "identidade:user:criar"),
		"filial-sul.exemplo.com",
		map[string]string{"Authorization": "Bearer " + token})

	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	assert.Contains(t, corpo.Permissoes, "identidade:user:*")
}

// Endereço fixo (painel.) NUNCA vira workspace — nem consulta o resolvedor.
func TestEnderecoFixoSaiSemEscopoDeWorkspace(t *testing.T) {
	cadeia, ws := cadeiaResolucao(t)
	token := tokenMaria(t, cadeia)

	resp, corpo := executar(t, rotaSemPermissao(cadeia),
		"painel.exemplo.com",
		map[string]string{"Authorization": "Bearer " + token})

	require.Equal(t, http.StatusOK, resp.Code)
	assert.Zero(t, ws.buscasSlug, "endereço fixo não consulta o resolvedor")
	assert.Equal(t, uuid.Nil.String(), corpo.Workspace, "sem workspace resolvido")

	// Sem escopo de workspace, rota protegida NEGA por falta de permissão.
	resp2, _ := executar(t, rotaProtegida(cadeia, "identidade:workspace:ler"),
		"painel.exemplo.com",
		map[string]string{"Authorization": "Bearer " + token})
	assert.Equal(t, http.StatusForbidden, resp2.Code, "console master não ganha permissão de graça")
}

func TestBaseDomainNuSegueSemWorkspace(t *testing.T) {
	cadeia, ws := cadeiaResolucao(t)
	token := tokenMaria(t, cadeia)

	resp, corpo := executar(t, rotaSemPermissao(cadeia),
		"exemplo.com",
		map[string]string{"Authorization": "Bearer " + token})

	require.Equal(t, http.StatusOK, resp.Code)
	assert.Zero(t, ws.buscasSlug)
	assert.Equal(t, uuid.Nil.String(), corpo.Workspace, "sem workspace resolvido")
}

// rotaSemPermissao monta só autenticação+resolução — para provar que a
// requisição SEM workspace passa adiante sem escopo (o passo seguinte é que
// nega por falta de permissão).
func rotaSemPermissao(cadeia *Cadeia) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/teste",
		cadeia.SetContextAuthorization(),
		cadeia.ResolveWorkspace(),
		func(c *gin.Context) {
			ctx := c.Request.Context()
			c.JSON(http.StatusOK, gin.H{"workspace": orgctx.WorkspaceUUID(ctx).String()})
		})
	return engine
}

// X-Workspace-Id vale SÓ fora de subdomínio (api./localhost/dev).
func TestFallbackHeaderResolveWorkspace(t *testing.T) {
	cadeia, _ := cadeiaResolucao(t)
	token := tokenMaria(t, cadeia)

	resp, corpo := executar(t, rotaProtegida(cadeia, "identidade:workspace:ler"),
		"api.exemplo.com",
		map[string]string{
			"Authorization":  "Bearer " + token,
			"X-Workspace-Id": wsFilialSul.String(),
		})

	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	assert.Equal(t, wsFilialSul.String(), corpo.Workspace)
}

func TestSubdominioVenceHeaderDivergente(t *testing.T) {
	cadeia, _ := cadeiaResolucao(t)
	token := tokenMaria(t, cadeia)

	resp, corpo := executar(t, rotaProtegida(cadeia, "identidade:workspace:ler"),
		"filial-sul.exemplo.com", // subdomínio diz filial-sul…
		map[string]string{
			"Authorization":  "Bearer " + token,
			"X-Workspace-Id": wsLoja.String(), // …header tenta outra coisa
		})

	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	assert.Equal(t, wsFilialSul.String(), corpo.Workspace, "subdomínio vence: header é ignorado")
}

func TestFallbackHeaderInvalidoReprova400(t *testing.T) {
	cadeia, _ := cadeiaResolucao(t)
	token := tokenMaria(t, cadeia)

	resp, corpo := executar(t, rotaProtegida(cadeia, ""),
		"api.exemplo.com",
		map[string]string{
			"Authorization":  "Bearer " + token,
			"X-Workspace-Id": "nao-e-um-uuid",
		})

	assert.Equal(t, http.StatusBadRequest, resp.Code)
	assert.Equal(t, "sistema.bad_request", corpo.Code)
}

func TestFallbackHeaderDeWorkspaceInativoReprova404(t *testing.T) {
	cadeia, ws := cadeiaResolucao(t)
	inativa := ws.porSlug["pausada"]
	require.NotNil(t, inativa)
	token := tokenMaria(t, cadeia)

	resp, _ := executar(t, rotaProtegida(cadeia, ""),
		"api.exemplo.com",
		map[string]string{
			"Authorization":  "Bearer " + token,
			"X-Workspace-Id": inativa.UUID.String(),
		})

	assert.Equal(t, http.StatusNotFound, resp.Code)
}
