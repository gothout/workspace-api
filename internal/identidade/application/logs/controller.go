package logs

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"workspace-api/internal/middleware"
	"workspace-api/internal/pkg/pagination"
	"workspace-api/internal/pkg/rest_err"
)

// PrefixoRotas é o path da aplicação dentro do grupo /api/application.
const PrefixoRotas = "/identidade/logs"

type Controller interface {
	Routes(routes gin.IRouter)
	Auditoria(c *gin.Context)
	Acesso(c *gin.Context)
	Erros(c *gin.Context)
}

type controllerImpl struct{ service Service }

func NewController(service Service) Controller { return &controllerImpl{service: service} }

// Routes declara a cadeia COMPLETA rota a rota (doc 03) — nunca no grupo. A
// exigência de rota é PermLer (recorte do próprio workspace); a AMPLIAÇÃO
// para organization inteira é conferida dentro do service via
// identidade:logs:ler_organization, e a plataforma passa pelo curinga *:*.
func (ctrl *controllerImpl) Routes(routes gin.IRouter) {
	g := routes.Group(PrefixoRotas)
	cadeia := []gin.HandlerFunc{
		middleware.SetContextAuthorization(),
		middleware.ResolveWorkspace(),
		middleware.RequirePermission(PermLer),
	}
	g.GET("/auditoria", append(cadeia, ctrl.Auditoria)...)
	g.GET("/acesso", append(cadeia, ctrl.Acesso)...)
	g.GET("/erros", append(cadeia, ctrl.Erros)...)
}

// filtroDoPedido centraliza bind+paginação dos handlers: query inválida =
// ErrFiltroInvalido (400); paginação sai do pacote padrão (teto 100).
func filtroDoPedido(c *gin.Context) (LogsFiltroRequestDto, pagination.Pagination, bool) {
	var dto LogsFiltroRequestDto
	if err := c.ShouldBindQuery(&dto); err != nil {
		rest_err.WriteError(c, traduzir(ErrFiltroInvalido))
		return dto, pagination.Pagination{}, false
	}
	return dto, pagination.DoQuery(c), true
}

// @Summary      Consulta a trilha de auditoria
// @Description  Lista paginada das escritas auditadas no recorte do chamador: super_admin lê qualquer organization, quem tem identidade:logs:ler_organization lê a própria organization inteira e os demais lêem só o workspace resolvido. Filtros fora do recorte respondem 404; ClickHouse ausente responde 503 padronizado
// @Tags         Identidade · Logs
// @Produce      json
// @Security     BearerAuth
// @Security     ApiKeyAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Param        page query int false "Página (default 1)"
// @Param        pageSize query int false "Itens por página (teto 100)"
// @Param        organization_uuid query string false "UUID da organization (só tem efeito para super_admin)"
// @Param        workspace_uuid query string false "UUID do workspace"
// @Param        user_uuid query string false "UUID do usuário autor"
// @Param        acao query string false "Ação estável do catálogo de eventos"
// @Param        ray_trace query string false "Correlação por requisição"
// @Param        inicio query string false "Instante inicial (RFC3339 UTC)"
// @Param        fim query string false "Instante final (RFC3339 UTC)"
// @Success      200 {object} pagination.Response[AuditoriaItemDto]
// @Failure      400 {object} rest_err.RestErr
// @Failure      403 {object} rest_err.RestErr
// @Failure      404 {object} rest_err.RestErr
// @Failure      503 {object} rest_err.RestErr
// @Router       /api/application/identidade/logs/auditoria [get]
func (ctrl *controllerImpl) Auditoria(c *gin.Context) {
	dto, paginacao, ok := filtroDoPedido(c)
	if !ok {
		return
	}
	filtro, err := dto.ParaFiltro()
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	resp, err := ctrl.service.Auditoria(c.Request.Context(), filtro, paginacao)
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.JSON(http.StatusOK, resp)
}

// @Summary      Consulta a trilha de acesso HTTP
// @Description  Lista paginada das requisições registradas no recorte do chamador (mesmas regras da trilha de auditoria). Filtros fora do recorte respondem 404; ClickHouse ausente responde 503 padronizado
// @Tags         Identidade · Logs
// @Produce      json
// @Security     BearerAuth
// @Security     ApiKeyAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Param        page query int false "Página (default 1)"
// @Param        pageSize query int false "Itens por página (teto 100)"
// @Param        organization_uuid query string false "UUID da organization (só tem efeito para super_admin)"
// @Param        workspace_uuid query string false "UUID do workspace"
// @Param        user_uuid query string false "UUID do usuário autenticado na requisição original"
// @Param        ray_trace query string false "Correlação por requisição"
// @Param        inicio query string false "Instante inicial (RFC3339 UTC)"
// @Param        fim query string false "Instante final (RFC3339 UTC)"
// @Success      200 {object} pagination.Response[AcessoItemDto]
// @Failure      400 {object} rest_err.RestErr
// @Failure      403 {object} rest_err.RestErr
// @Failure      404 {object} rest_err.RestErr
// @Failure      503 {object} rest_err.RestErr
// @Router       /api/application/identidade/logs/acesso [get]
func (ctrl *controllerImpl) Acesso(c *gin.Context) {
	dto, paginacao, ok := filtroDoPedido(c)
	if !ok {
		return
	}
	filtro, err := dto.ParaFiltro()
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	resp, err := ctrl.service.Acesso(c.Request.Context(), filtro, paginacao)
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.JSON(http.StatusOK, resp)
}

// @Summary      Consulta a trilha de erros observados
// @Description  Lista paginada dos erros devolvidos pelos services (código estável + severidade), no recorte do chamador. Filtro acao casa com o código estável do catálogo. Filtros fora do recorte respondem 404; ClickHouse ausente responde 503 padronizado
// @Tags         Identidade · Logs
// @Produce      json
// @Security     BearerAuth
// @Security     ApiKeyAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Param        page query int false "Página (default 1)"
// @Param        pageSize query int false "Itens por página (teto 100)"
// @Param        organization_uuid query string false "UUID da organization (só tem efeito para super_admin)"
// @Param        workspace_uuid query string false "UUID do workspace"
// @Param        user_uuid query string false "UUID do usuário em cujo contexto o erro ocorreu"
// @Param        acao query string false "Código estável do erro (ex.: identidade.workspace.slug_em_uso)"
// @Param        ray_trace query string false "Correlação por requisição"
// @Param        inicio query string false "Instante inicial (RFC3339 UTC)"
// @Param        fim query string false "Instante final (RFC3339 UTC)"
// @Success      200 {object} pagination.Response[ErroItemDto]
// @Failure      400 {object} rest_err.RestErr
// @Failure      403 {object} rest_err.RestErr
// @Failure      404 {object} rest_err.RestErr
// @Failure      503 {object} rest_err.RestErr
// @Router       /api/application/identidade/logs/erros [get]
func (ctrl *controllerImpl) Erros(c *gin.Context) {
	dto, paginacao, ok := filtroDoPedido(c)
	if !ok {
		return
	}
	filtro, err := dto.ParaFiltro()
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	resp, err := ctrl.service.Erros(c.Request.Context(), filtro, paginacao)
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.JSON(http.StatusOK, resp)
}

// traduzir converte a sentinela no rest_err do catálogo da aplicação;
// desconhecido = 500 genérico.
func traduzir(err error) *rest_err.RestErr {
	switch {
	case errors.Is(err, ErrFiltroInvalido):
		return rest_err.DoCatalogo(ErrFiltroInvalido)
	case errors.Is(err, ErrForaDoEscopo):
		return rest_err.DoCatalogo(ErrForaDoEscopo)
	case errors.Is(err, ErrIndisponivel):
		return rest_err.DoCatalogo(ErrIndisponivel)
	default:
		return rest_err.Interno(err)
	}
}
