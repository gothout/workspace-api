package catalogo

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"workspace-api/internal/middleware"
)

// PrefixoRotas é o path da aplicação dentro do grupo /api/application.
const PrefixoRotas = "/identidade/catalogo"

// RotaErrosSistema é a rota de sistema registrada NO ENGINE (fora das duas
// famílias domain/application — exceção de prefixo decidida no doc 01/04).
// Pública por decisão: o mapping de tradução precisa existir antes de
// qualquer auth.
const RotaErrosSistema = "/api/system/errors"

// PermLer libera a consulta da própria árvore. Constante espelhada no seed
// dos 4 papéis humanos (super_admin passa pelo curinga *:*): ver a própria
// permissão é pré-requisito de usar qualquer outra — sem ela o endpoint
// perde o propósito de alimentar menus.
const PermLer = "identidade:catalogo:ler"

type Controller interface {
	// Routes pendura as rotas em /api/application{PrefixoRotas}.
	Routes(routes gin.IRouter)
	// RoutesSistema pendura RotaErrosSistema direto no engine — o grupo
	// /api/application mudaria o path do contrato (doc 04).
	RoutesSistema(routes gin.IRouter)
	MinhasPermissoes(c *gin.Context)
	Erros(c *gin.Context)
}

type controllerImpl struct{ service Service }

func NewController(service Service) Controller { return &controllerImpl{service: service} }

// Routes declara a cadeia COMPLETA rota a rota (doc 03) — nunca no grupo.
func (ctrl *controllerImpl) Routes(routes gin.IRouter) {
	g := routes.Group(PrefixoRotas)
	g.GET("/permissoes/minhas",
		middleware.SetContextAuthorization(),
		middleware.ResolveWorkspace(),
		middleware.RequirePermission(PermLer),
		ctrl.MinhasPermissoes)
}

// RoutesSistema registra a rota pública de sistema SEM cadeia — decisão
// documentada no AGENTS.md do pacote e no doc 04.
func (ctrl *controllerImpl) RoutesSistema(routes gin.IRouter) {
	routes.GET(RotaErrosSistema, ctrl.Erros)
}

// Handlers finos: service → c.JSON. Leitura não audita; erro de sistema sai
// pelo rest_err.WriteError.

// @Summary      Lista as permissões do usuário autenticado
// @Description  Devolve a árvore dominio → subdominio → ações JÁ FILTRADA pelas permissões efetivas do usuário no workspace ativo — o front monta menu/botões sem hardcode de regra. super_admin recebe a árvore inteira
// @Tags         Identidade · Catálogo
// @Produce      json
// @Security     BearerAuth
// @Security     ApiKeyAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Success      200 {object} ArvoreResponseDto
// @Failure      401 {object} rest_err.RestErr
// @Failure      403 {object} rest_err.RestErr
// @Router       /api/application/identidade/catalogo/permissoes/minhas [get]
func (ctrl *controllerImpl) MinhasPermissoes(c *gin.Context) {
	c.JSON(http.StatusOK, ctrl.service.MinhasPermissoes(c.Request.Context()))
}

// @Summary      Mapa completo de erros do sistema
// @Description  Todos os erros possíveis, agrupados por domínio/subdomínio com code estável, mensagem PT-BR e status — mapping de tradução/listagem do front-end. Rota pública de sistema por decisão
// @Tags         Sistema · Catálogo
// @Produce      json
// @Success      200 {object} ErrosResponseDto
// @Router       /api/system/errors [get]
func (ctrl *controllerImpl) Erros(c *gin.Context) {
	c.JSON(http.StatusOK, ctrl.service.MapaDeErros())
}
