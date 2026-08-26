package workspace

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	modelworkspace "workspace-api/internal/identidade/model/workspace"
	"workspace-api/internal/middleware"
	"workspace-api/internal/pkg/pagination"
	"workspace-api/internal/pkg/rest_err"
)

// PrefixoRotas é o path do subdomínio dentro do grupo /api/domain.
const PrefixoRotas = "/identidade/workspaces"

type Controller interface {
	Routes(routes gin.IRouter)
	Create(c *gin.Context)
	Read(c *gin.Context)
	List(c *gin.Context)
	Update(c *gin.Context)
	Reativar(c *gin.Context)
	Delete(c *gin.Context)
}

type controllerImpl struct{ service Service }

func NewController(service Service) Controller { return &controllerImpl{service: service} }

// Routes declara a cadeia ROTA A ROTA — nunca no grupo (doc 03).
// As funções de PACOTE do middleware falham FECHADAS (403) quando a cadeia
// não foi inicializada no boot — Routes() nunca panica; MustUse() é do bootstrap.
func (ctrl *controllerImpl) Routes(routes gin.IRouter) {
	g := routes.Group(PrefixoRotas)
	g.POST("", middleware.SetContextAuthorization(), middleware.ResolveWorkspace(), middleware.RequirePermission(PermCriar), ctrl.Create)
	g.GET("", middleware.SetContextAuthorization(), middleware.ResolveWorkspace(), middleware.RequirePermission(PermLer), ctrl.List)
	g.GET("/:uuid", middleware.SetContextAuthorization(), middleware.ResolveWorkspace(), middleware.RequirePermission(PermLer), ctrl.Read)
	g.PATCH("/:uuid", middleware.SetContextAuthorization(), middleware.ResolveWorkspace(), middleware.RequirePermission(PermEditar), ctrl.Update)
	g.POST("/:uuid/acoes/reativar", middleware.SetContextAuthorization(), middleware.ResolveWorkspace(), middleware.RequirePermission(PermEditar), ctrl.Reativar)
	g.DELETE("/:uuid", middleware.SetContextAuthorization(), middleware.ResolveWorkspace(), middleware.RequirePermission(PermRemover), ctrl.Delete)
}

// Handlers finos: bind → service → c.JSON. Erro sai SÓ por rest_err.WriteError.

// @Summary      Cria um workspace
// @Description  Cria workspace na organization autenticada, validando slug único global e reservados. Chamador PLATAFORMA (super_admin) pode apontar organization_uuid explícito para criar o primeiro workspace de outra organization — criação cross-tenant auditada; para os demais, organization alheia recusa com 404
// @Tags         Identidade · Workspace
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Param        request body CreateWorkspaceRequestDto true "Dados do workspace"
// @Success      201 {object} WorkspaceResponseDto
// @Failure      400 {object} rest_err.RestErr
// @Failure      403 {object} rest_err.RestErr
// @Failure      404 {object} rest_err.RestErr "Organization pedida não encontrada ou fora do escopo"
// @Failure      409 {object} rest_err.RestErr "Slug em uso"
// @Failure      422 {object} rest_err.RestErr "Slug reservado ou organization alvo inativa"
// @Router       /api/domain/identidade/workspaces [post]
func (ctrl *controllerImpl) Create(c *gin.Context) {
	var dto CreateWorkspaceRequestDto
	if err := c.ShouldBindJSON(&dto); err != nil {
		rest_err.WriteError(c, traduzir(ErrInvalidInput))
		return
	}
	w, err := ctrl.service.Create(c.Request.Context(), dto.ParaEntrada())
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.JSON(http.StatusCreated, NovoWorkspaceResponseDto(w))
}

// @Summary      Consulta um workspace
// @Description  Devolve o workspace no escopo da organization resolvida — workspace de organization alheia não se distingue de inexistente
// @Tags         Identidade · Workspace
// @Produce      json
// @Security     BearerAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Param        uuid path string true "UUID do workspace"
// @Success      200 {object} WorkspaceResponseDto
// @Failure      400 {object} rest_err.RestErr "UUID malformado"
// @Failure      403 {object} rest_err.RestErr
// @Failure      404 {object} rest_err.RestErr
// @Router       /api/domain/identidade/workspaces/{uuid} [get]
func (ctrl *controllerImpl) Read(c *gin.Context) {
	id, ok := uuidDoPath(c, "uuid")
	if !ok {
		return
	}
	w, err := ctrl.service.Read(c.Request.Context(), id)
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.JSON(http.StatusOK, NovoWorkspaceResponseDto(w))
}

// @Summary      Lista workspaces
// @Description  Lista paginada dos workspaces da organization, com filtros. A PLATAFORMA (super_admin) pode filtrar por organization_uuid para listar os workspaces de qualquer organization; para os demais chamadores o filtro apontando organization alheia recusa com 404
// @Tags         Identidade · Workspace
// @Produce      json
// @Security     BearerAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Param        page query int false "Página (default 1)"
// @Param        pageSize query int false "Itens por página (teto 100)"
// @Param        nome query string false "Filtro por nome"
// @Param        status query string false "Filtro por status (ativo|inativo)"
// @Param        organization_uuid query string false "UUID da organization (SÓ a plataforma; demais recusam alheia com 404)"
// @Success      200 {object} pagination.Response[WorkspaceResponseDto]
// @Failure      400 {object} rest_err.RestErr
// @Failure      403 {object} rest_err.RestErr
// @Failure      404 {object} rest_err.RestErr "organization_uuid alheia ao chamador"
// @Router       /api/domain/identidade/workspaces [get]
func (ctrl *controllerImpl) List(c *gin.Context) {
	var f modelworkspace.ListFilter
	if err := c.ShouldBindQuery(&f); err != nil {
		rest_err.WriteError(c, traduzir(ErrInvalidInput))
		return
	}
	f.Pagination = pagination.DoQuery(c) // lê page/pageSize aplicando o teto de 100 (doc 04)
	// UX4: filtro de plataforma — parse manual porque o binding de form não
	// cobre uuid; inválido = 400 (doc 04), nunca ignorado silenciosamente.
	if bruto := c.Query("organization_uuid"); bruto != "" {
		id, err := uuid.Parse(bruto)
		if err != nil {
			rest_err.WriteError(c, traduzir(ErrInvalidInput))
			return
		}
		f.OrganizationUUID = &id
	}
	items, total, err := ctrl.service.List(c.Request.Context(), f)
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.JSON(http.StatusOK, NovoWorkspaceListaResponseDto(items, total, f.Pagination))
}

// @Summary      Atualiza um workspace
// @Description  Atualização parcial; inativar via PATCH suspende a resolução pelo Host; reativação é ação própria
// @Tags         Identidade · Workspace
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Param        uuid path string true "UUID do workspace"
// @Param        request body UpdateWorkspaceRequestDto true "Campos a atualizar"
// @Success      200 {object} WorkspaceResponseDto
// @Failure      400 {object} rest_err.RestErr
// @Failure      403 {object} rest_err.RestErr
// @Failure      404 {object} rest_err.RestErr
// @Failure      422 {object} rest_err.RestErr "Transição de estado proibida"
// @Router       /api/domain/identidade/workspaces/{uuid} [patch]
func (ctrl *controllerImpl) Update(c *gin.Context) {
	id, ok := uuidDoPath(c, "uuid")
	if !ok {
		return
	}
	var dto UpdateWorkspaceRequestDto
	if err := c.ShouldBindJSON(&dto); err != nil {
		rest_err.WriteError(c, traduzir(ErrInvalidInput))
		return
	}
	in, err := dto.ParaEntrada()
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	w, err := ctrl.service.Update(c.Request.Context(), id, in)
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.JSON(http.StatusOK, NovoWorkspaceResponseDto(w))
}

// @Summary      Reativa um workspace
// @Description  Ação de negócio própria: devolve o workspace ao ar e a resolução pelo Host volta imediatamente
// @Tags         Identidade · Workspace
// @Produce      json
// @Security     BearerAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Param        uuid path string true "UUID do workspace"
// @Success      200 {object} WorkspaceResponseDto
// @Failure      400 {object} rest_err.RestErr "UUID malformado"
// @Failure      403 {object} rest_err.RestErr
// @Failure      404 {object} rest_err.RestErr
// @Failure      422 {object} rest_err.RestErr "Já está ativo"
// @Router       /api/domain/identidade/workspaces/{uuid}/acoes/reativar [post]
func (ctrl *controllerImpl) Reativar(c *gin.Context) {
	id, ok := uuidDoPath(c, "uuid")
	if !ok {
		return
	}
	w, err := ctrl.service.Reativar(c.Request.Context(), id)
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.JSON(http.StatusOK, NovoWorkspaceResponseDto(w))
}

// @Summary      Remove um workspace
// @Description  Remoção lógica do workspace no escopo da organization; o slug removido NÃO se libera (índice único total — anti-takeover)
// @Tags         Identidade · Workspace
// @Produce      json
// @Security     BearerAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Param        uuid path string true "UUID do workspace"
// @Success      204
// @Failure      400 {object} rest_err.RestErr "UUID malformado"
// @Failure      403 {object} rest_err.RestErr
// @Failure      404 {object} rest_err.RestErr
// @Router       /api/domain/identidade/workspaces/{uuid} [delete]
func (ctrl *controllerImpl) Delete(c *gin.Context) {
	id, ok := uuidDoPath(c, "uuid")
	if !ok {
		return
	}
	if err := ctrl.service.Delete(c.Request.Context(), id); err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.Status(http.StatusNoContent)
}

// uuidDoPath faz o parse do parâmetro; inválido = 400 (doc 04).
func uuidDoPath(c *gin.Context, nome string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(nome))
	if err != nil {
		rest_err.WriteError(c, traduzir(ErrInvalidInput))
		return uuid.Nil, false
	}
	return id, true
}

// traduzir converte a sentinela no rest_err do catálogo do subdomínio;
// desconhecido = 500. As sentinelas do modelo são resolvidas pelo mesmo
// registro global (errors.Is atravessa os pacotes).
func traduzir(err error) *rest_err.RestErr {
	switch {
	case errors.Is(err, ErrNotFound),
		errors.Is(err, ErrInvalidInput),
		errors.Is(err, ErrSlugEmUso),
		errors.Is(err, ErrSlugReservado),
		errors.Is(err, ErrForaDoEscopo),
		errors.Is(err, ErrOrganizacaoNaoEncontrada),
		errors.Is(err, ErrOrganizacaoInativa),
		errors.Is(err, modelworkspace.ErrSlugInvalido),
		errors.Is(err, modelworkspace.ErrNomeInvalido),
		errors.Is(err, modelworkspace.ErrJaInativo),
		errors.Is(err, modelworkspace.ErrJaAtivo):
		return rest_err.DoCatalogo(err)
	default:
		return rest_err.Interno(err)
	}
}
