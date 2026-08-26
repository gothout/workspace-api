package modulo

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	modelmodulo "workspace-api/internal/licensing/model/modulo"
	"workspace-api/internal/middleware"
	"workspace-api/internal/pkg/pagination"
	"workspace-api/internal/pkg/rest_err"
)

// PrefixoRotas é o path do subdomínio dentro do grupo /api/domain.
const PrefixoRotas = "/licensing/modulos"

type Controller interface {
	Routes(routes gin.IRouter)
	Create(c *gin.Context)
	Read(c *gin.Context)
	List(c *gin.Context)
	Update(c *gin.Context)
	Delete(c *gin.Context)
}

type controllerImpl struct{ service Service }

func NewController(service Service) Controller { return &controllerImpl{service: service} }

// Routes declara a cadeia ROTA A ROTA — nunca no grupo (doc 03). Escrita é
// exclusiva do super_admin; leitura vai aos papéis de administração no seed.
func (ctrl *controllerImpl) Routes(routes gin.IRouter) {
	g := routes.Group(PrefixoRotas)
	g.POST("", middleware.SetContextAuthorization(), middleware.ResolveWorkspace(), middleware.RequirePermission(PermCriar), ctrl.Create)
	g.GET("", middleware.SetContextAuthorization(), middleware.ResolveWorkspace(), middleware.RequirePermission(PermLer), ctrl.List)
	g.GET("/:uuid", middleware.SetContextAuthorization(), middleware.ResolveWorkspace(), middleware.RequirePermission(PermLer), ctrl.Read)
	g.PATCH("/:uuid", middleware.SetContextAuthorization(), middleware.ResolveWorkspace(), middleware.RequirePermission(PermEditar), ctrl.Update)
	g.DELETE("/:uuid", middleware.SetContextAuthorization(), middleware.ResolveWorkspace(), middleware.RequirePermission(PermRemover), ctrl.Delete)
}

// Handlers finos: bind → service → c.JSON. Erro sai SÓ por rest_err.WriteError.

// @Summary      Cria um módulo
// @Description  Registra uma aplicação no catálogo global da plataforma (super_admin)
// @Tags         Licensing · Módulos
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body CreateModuloRequestDto true "Dados do módulo"
// @Success      201 {object} ModuloResponseDto
// @Failure      400 {object} rest_err.RestErr
// @Failure      403 {object} rest_err.RestErr
// @Failure      409 {object} rest_err.RestErr "Slug em uso"
// @Router       /api/domain/licensing/modulos [post]
func (ctrl *controllerImpl) Create(c *gin.Context) {
	var dto CreateModuloRequestDto
	if err := c.ShouldBindJSON(&dto); err != nil {
		rest_err.WriteError(c, traduzir(ErrInvalidInput))
		return
	}
	m, err := ctrl.service.Create(c.Request.Context(), dto.ParaEntrada())
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.JSON(http.StatusCreated, NovoModuloResponseDto(m))
}

// @Summary      Consulta um módulo
// @Description  Devolve o módulo do catálogo pelo uuid
// @Tags         Licensing · Módulos
// @Produce      json
// @Security     BearerAuth
// @Param        uuid path string true "UUID do módulo"
// @Success      200 {object} ModuloResponseDto
// @Failure      400 {object} rest_err.RestErr "UUID malformado"
// @Failure      403 {object} rest_err.RestErr
// @Failure      404 {object} rest_err.RestErr
// @Router       /api/domain/licensing/modulos/{uuid} [get]
func (ctrl *controllerImpl) Read(c *gin.Context) {
	id, err := uuid.Parse(c.Param("uuid"))
	if err != nil {
		rest_err.WriteError(c, traduzir(ErrInvalidInput))
		return
	}
	m, err := ctrl.service.Read(c.Request.Context(), id)
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.JSON(http.StatusOK, NovoModuloResponseDto(m))
}

// @Summary      Lista módulos
// @Description  Lista paginada do catálogo de aplicações da plataforma, com filtros
// @Tags         Licensing · Módulos
// @Produce      json
// @Security     BearerAuth
// @Param        page query int false "Página (default 1)"
// @Param        pageSize query int false "Itens por página (teto 100)"
// @Param        nome query string false "Filtro por nome"
// @Success      200 {object} pagination.Response[ModuloResponseDto]
// @Failure      403 {object} rest_err.RestErr
// @Router       /api/domain/licensing/modulos [get]
func (ctrl *controllerImpl) List(c *gin.Context) {
	var f modelmodulo.ListFilter
	if err := c.ShouldBindQuery(&f); err != nil {
		rest_err.WriteError(c, traduzir(ErrInvalidInput))
		return
	}
	f.Pagination = pagination.DoQuery(c)
	items, total, err := ctrl.service.List(c.Request.Context(), f)
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.JSON(http.StatusOK, NovoModuloListaResponseDto(items, total, f.Pagination))
}

// @Summary      Atualiza um módulo
// @Description  Atualização parcial; ativo/inativo passa pelos métodos de comportamento do agregado
// @Tags         Licensing · Módulos
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        uuid path string true "UUID do módulo"
// @Param        request body UpdateModuloRequestDto true "Campos a atualizar"
// @Success      200 {object} ModuloResponseDto
// @Failure      400 {object} rest_err.RestErr
// @Failure      403 {object} rest_err.RestErr
// @Failure      404 {object} rest_err.RestErr
// @Router       /api/domain/licensing/modulos/{uuid} [patch]
func (ctrl *controllerImpl) Update(c *gin.Context) {
	id, err := uuid.Parse(c.Param("uuid"))
	if err != nil {
		rest_err.WriteError(c, traduzir(ErrInvalidInput))
		return
	}
	var dto UpdateModuloRequestDto
	if err := c.ShouldBindJSON(&dto); err != nil {
		rest_err.WriteError(c, traduzir(ErrInvalidInput))
		return
	}
	in := dto.ParaEntrada()
	m, err := ctrl.service.Update(c.Request.Context(), id, in)
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.JSON(http.StatusOK, NovoModuloResponseDto(m))
}

// @Summary      Remove um módulo
// @Description  Remoção lógica; recusada enquanto houver licenças vivas (desative em vez de remover)
// @Tags         Licensing · Módulos
// @Produce      json
// @Security     BearerAuth
// @Param        uuid path string true "UUID do módulo"
// @Success      204
// @Failure      400 {object} rest_err.RestErr "UUID malformado"
// @Failure      403 {object} rest_err.RestErr
// @Failure      404 {object} rest_err.RestErr
// @Failure      409 {object} rest_err.RestErr "Módulo com licenças vivas"
// @Router       /api/domain/licensing/modulos/{uuid} [delete]
func (ctrl *controllerImpl) Delete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("uuid"))
	if err != nil {
		rest_err.WriteError(c, traduzir(ErrInvalidInput))
		return
	}
	if err := ctrl.service.Delete(c.Request.Context(), id); err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.Status(http.StatusNoContent)
}

// traduzir converte a sentinela no rest_err do catálogo do subdomínio; desconhecido = 500.
func traduzir(err error) *rest_err.RestErr {
	switch {
	case errors.Is(err, ErrNotFound):
		return rest_err.DoCatalogo(ErrNotFound)
	case errors.Is(err, ErrInvalidInput):
		return rest_err.DoCatalogo(ErrInvalidInput)
	case errors.Is(err, ErrSlugEmUso):
		return rest_err.DoCatalogo(ErrSlugEmUso)
	case errors.Is(err, ErrModuloEmUso):
		return rest_err.DoCatalogo(ErrModuloEmUso)
	default:
		return rest_err.Interno(err)
	}
}
