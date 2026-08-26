package tarefa

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	modeltarefa "workspace-api/internal/todolist/model/tarefa"
	"workspace-api/internal/middleware"
	"workspace-api/internal/pkg/pagination"
	"workspace-api/internal/pkg/rest_err"
)

// PrefixoRotas é o path do subdomínio dentro do grupo /api/domain.
const PrefixoRotas = "/todolist/tasks"

// SlugDoModulo é o rótulo do módulo na cadeia de acesso — a rota SÓ abre com
// o header Application: todolist E o par (org, ws) licenciado/ativado.
const SlugDoModulo = "todolist"

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

// Routes declara a cadeia ROTA A ROTA — nunca no grupo (doc 03). Toda rota
// do módulo exige RequireAplicacao("todolist") DEPOIS do workspace resolvido
// e ANTES da permissão granular.
func (ctrl *controllerImpl) Routes(routes gin.IRouter) {
	g := routes.Group(PrefixoRotas)
	g.POST("", middleware.SetContextAuthorization(), middleware.ResolveWorkspace(), middleware.RequireAplicacao(SlugDoModulo), middleware.RequirePermission(PermCriar), ctrl.Create)
	g.GET("", middleware.SetContextAuthorization(), middleware.ResolveWorkspace(), middleware.RequireAplicacao(SlugDoModulo), middleware.RequirePermission(PermLer), ctrl.List)
	g.GET("/:uuid", middleware.SetContextAuthorization(), middleware.ResolveWorkspace(), middleware.RequireAplicacao(SlugDoModulo), middleware.RequirePermission(PermLer), ctrl.Read)
	g.PATCH("/:uuid", middleware.SetContextAuthorization(), middleware.ResolveWorkspace(), middleware.RequireAplicacao(SlugDoModulo), middleware.RequirePermission(PermEditar), ctrl.Update)
	g.DELETE("/:uuid", middleware.SetContextAuthorization(), middleware.ResolveWorkspace(), middleware.RequireAplicacao(SlugDoModulo), middleware.RequirePermission(PermRemover), ctrl.Delete)
}

// Handlers finos: bind → service → c.JSON. Erro sai SÓ por rest_err.WriteError.

// @Summary      Cria uma tarefa
// @Description  Cria tarefa no todolist do workspace resolvido
// @Tags         Todolist
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        Application header string true "Módulo ativo (todolist)"
// @Param        request body CreateTarefaRequestDto true "Dados da tarefa"
// @Success      201 {object} TarefaResponseDto
// @Failure      400 {object} rest_err.RestErr
// @Failure      403 {object} rest_err.RestErr
// @Router       /api/domain/todolist/tasks [post]
func (ctrl *controllerImpl) Create(c *gin.Context) {
	var dto CreateTarefaRequestDto
	if err := c.ShouldBindJSON(&dto); err != nil {
		rest_err.WriteError(c, traduzir(ErrInvalidInput))
		return
	}
	t, err := ctrl.service.Create(c.Request.Context(), dto.ParaEntrada())
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.JSON(http.StatusCreated, NovaTarefaResponseDto(t))
}

// @Summary      Consulta uma tarefa
// @Description  Devolve a tarefa do todolist no escopo resolvido
// @Tags         Todolist
// @Produce      json
// @Security     BearerAuth
// @Param        Application header string true "Módulo ativo (todolist)"
// @Param        uuid path string true "UUID da tarefa"
// @Success      200 {object} TarefaResponseDto
// @Failure      400 {object} rest_err.RestErr "UUID malformado"
// @Failure      403 {object} rest_err.RestErr
// @Failure      404 {object} rest_err.RestErr
// @Router       /api/domain/todolist/tasks/{uuid} [get]
func (ctrl *controllerImpl) Read(c *gin.Context) {
	id, err := uuid.Parse(c.Param("uuid"))
	if err != nil {
		rest_err.WriteError(c, traduzir(ErrInvalidInput))
		return
	}
	t, err := ctrl.service.Read(c.Request.Context(), id)
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.JSON(http.StatusOK, NovaTarefaResponseDto(t))
}

// @Summary      Lista tarefas
// @Description  Lista paginada das tarefas do workspace, com filtro de conclusão
// @Tags         Todolist
// @Produce      json
// @Security     BearerAuth
// @Param        Application header string true "Módulo ativo (todolist)"
// @Param        page query int false "Página (default 1)"
// @Param        pageSize query int false "Itens por página (teto 100)"
// @Param        concluida query bool false "Filtro por conclusão"
// @Success      200 {object} pagination.Response[TarefaResponseDto]
// @Failure      403 {object} rest_err.RestErr
// @Router       /api/domain/todolist/tasks [get]
func (ctrl *controllerImpl) List(c *gin.Context) {
	var f modeltarefa.ListFilter
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
	c.JSON(http.StatusOK, NovaTarefaListaResponseDto(items, total, f.Pagination))
}

// @Summary      Atualiza uma tarefa
// @Description  Atualização parcial; concluir/reabrir passam pelos métodos de comportamento do agregado
// @Tags         Todolist
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        Application header string true "Módulo ativo (todolist)"
// @Param        uuid path string true "UUID da tarefa"
// @Param        request body UpdateTarefaRequestDto true "Campos a atualizar"
// @Success      200 {object} TarefaResponseDto
// @Failure      400 {object} rest_err.RestErr
// @Failure      403 {object} rest_err.RestErr
// @Failure      404 {object} rest_err.RestErr
// @Failure      422 {object} rest_err.RestErr "Transição de estado proibida"
// @Router       /api/domain/todolist/tasks/{uuid} [patch]
func (ctrl *controllerImpl) Update(c *gin.Context) {
	id, err := uuid.Parse(c.Param("uuid"))
	if err != nil {
		rest_err.WriteError(c, traduzir(ErrInvalidInput))
		return
	}
	var dto UpdateTarefaRequestDto
	if err := c.ShouldBindJSON(&dto); err != nil {
		rest_err.WriteError(c, traduzir(ErrInvalidInput))
		return
	}
	t, err := ctrl.service.Update(c.Request.Context(), id, dto.ParaEntrada())
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.JSON(http.StatusOK, NovaTarefaResponseDto(t))
}

// @Summary      Remove uma tarefa
// @Description  Remoção lógica da tarefa no escopo resolvido
// @Tags         Todolist
// @Produce      json
// @Security     BearerAuth
// @Param        Application header string true "Módulo ativo (todolist)"
// @Param        uuid path string true "UUID da tarefa"
// @Success      204
// @Failure      400 {object} rest_err.RestErr "UUID malformado"
// @Failure      403 {object} rest_err.RestErr
// @Failure      404 {object} rest_err.RestErr
// @Router       /api/domain/todolist/tasks/{uuid} [delete]
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
	default:
		return rest_err.Interno(err)
	}
}
