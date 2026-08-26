package ativacao

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"workspace-api/internal/middleware"
	"workspace-api/internal/pkg/rest_err"
)

// PrefixoRotas é o path do subdomínio dentro do grupo /api/domain. O
// workspace ALVO vem do path: o painel da organization administra qualquer
// workspace dela — o service valida pertencimento e vitalidade.
const PrefixoRotas = "/licensing/workspaces/:workspace_uuid/modulos"

type Controller interface {
	Routes(routes gin.IRouter)
	Ativar(c *gin.Context)
	Listar(c *gin.Context)
	Desativar(c *gin.Context)
}

type controllerImpl struct{ service Service }

func NewController(service Service) Controller { return &controllerImpl{service: service} }

// Routes declara a cadeia ROTA A ROTA — nunca no grupo (doc 03).
func (ctrl *controllerImpl) Routes(routes gin.IRouter) {
	g := routes.Group(PrefixoRotas)
	g.POST("", middleware.SetContextAuthorization(), middleware.ResolveWorkspace(), middleware.RequirePermission(PermCriar), ctrl.Ativar)
	g.GET("", middleware.SetContextAuthorization(), middleware.ResolveWorkspace(), middleware.RequirePermission(PermLer), ctrl.Listar)
	g.DELETE("/:uuid", middleware.SetContextAuthorization(), middleware.ResolveWorkspace(), middleware.RequirePermission(PermRemover), ctrl.Desativar)
}

// workspaceAlvo parseia e devolve o workspace do path; malformado = 400.
func workspaceAlvo(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("workspace_uuid"))
	if err != nil || id == uuid.Nil {
		rest_err.WriteError(c, traduzir(ErrInvalidInput))
		return uuid.Nil, false
	}
	return id, true
}

// @Summary      Ativa um módulo no workspace
// @Description  Aplica um módulo licenciado da organization ao workspace alvo (exige licença viva)
// @Tags         Licensing · Ativações
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Param        workspace_uuid path string true "UUID do workspace alvo"
// @Param        request body AtivarModuloRequestDto true "Módulo a ativar"
// @Success      201 {object} AtivacaoResponseDto
// @Failure      400 {object} rest_err.RestErr
// @Failure      403 {object} rest_err.RestErr
// @Failure      404 {object} rest_err.RestErr "Workspace ou módulo inexistente"
// @Failure      409 {object} rest_err.RestErr "Já ativado"
// @Failure      422 {object} rest_err.RestErr "Sem licença do módulo"
// @Router       /api/domain/licensing/workspaces/{workspace_uuid}/modulos [post]
func (ctrl *controllerImpl) Ativar(c *gin.Context) {
	alvo, ok := workspaceAlvo(c)
	if !ok {
		return
	}
	var dto AtivarModuloRequestDto
	if err := c.ShouldBindJSON(&dto); err != nil {
		rest_err.WriteError(c, traduzir(ErrInvalidInput))
		return
	}
	a, err := ctrl.service.Ativar(c.Request.Context(), alvo, dto.ModuloSlug)
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.JSON(http.StatusCreated, NovaAtivacaoResponseDto(a))
}

// @Summary      Lista os módulos ativados no workspace
// @Description  Ativações vivas do workspace alvo com o resumo de cada módulo
// @Tags         Licensing · Ativações
// @Produce      json
// @Security     BearerAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Param        workspace_uuid path string true "UUID do workspace alvo"
// @Success      200 {array} AtivacaoResponseDto
// @Failure      400 {object} rest_err.RestErr
// @Failure      403 {object} rest_err.RestErr
// @Failure      404 {object} rest_err.RestErr "Workspace fora da organization"
// @Router       /api/domain/licensing/workspaces/{workspace_uuid}/modulos [get]
func (ctrl *controllerImpl) Listar(c *gin.Context) {
	alvo, ok := workspaceAlvo(c)
	if !ok {
		return
	}
	items, err := ctrl.service.Listar(c.Request.Context(), alvo)
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.JSON(http.StatusOK, NovaAtivacaoListaResponseDto(items))
}

// @Summary      Desativa um módulo no workspace
// @Description  Remove a ativação viva; usuários perdem o acesso pela resolução na hora
// @Tags         Licensing · Ativações
// @Produce      json
// @Security     BearerAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Param        workspace_uuid path string true "UUID do workspace alvo"
// @Param        uuid path string true "UUID da ativação"
// @Success      204
// @Failure      400 {object} rest_err.RestErr
// @Failure      403 {object} rest_err.RestErr
// @Failure      404 {object} rest_err.RestErr
// @Router       /api/domain/licensing/workspaces/{workspace_uuid}/modulos/{uuid} [delete]
func (ctrl *controllerImpl) Desativar(c *gin.Context) {
	alvo, ok := workspaceAlvo(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("uuid"))
	if err != nil {
		rest_err.WriteError(c, traduzir(ErrInvalidInput))
		return
	}
	if err := ctrl.service.Desativar(c.Request.Context(), alvo, id); err != nil {
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
	case errors.Is(err, ErrJaAtivada):
		return rest_err.DoCatalogo(ErrJaAtivada)
	case errors.Is(err, ErrSemLicenca):
		return rest_err.DoCatalogo(ErrSemLicenca)
	case errors.Is(err, ErrWorkspaceInvalido):
		return rest_err.DoCatalogo(ErrWorkspaceInvalido)
	case errors.Is(err, ErrModuloInvalido):
		return rest_err.DoCatalogo(ErrModuloInvalido)
	default:
		return rest_err.Interno(err)
	}
}
