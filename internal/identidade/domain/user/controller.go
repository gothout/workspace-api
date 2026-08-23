package user

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	modeluser "workspace-api/internal/identidade/model/user"
	"workspace-api/internal/middleware"
	"workspace-api/internal/pkg/pagination"
	"workspace-api/internal/pkg/rest_err"
)

// PrefixoRotas é o path do subdomínio dentro do grupo /api/domain.
const PrefixoRotas = "/identidade/users"

// PrefixoRotasPapeis é o path da listagem de papéis globais — recurso de
// referência do subdomínio fora do CRUD de usuários (contrato da issue #28:
// /api/domain/identidade/user/papeis).
const PrefixoRotasPapeis = "/identidade/user/papeis"

type Controller interface {
	Routes(routes gin.IRouter)
	Create(c *gin.Context)
	Read(c *gin.Context)
	List(c *gin.Context)
	Update(c *gin.Context)
	Delete(c *gin.Context)
	Atribuicoes(c *gin.Context)
	AtribuirPapel(c *gin.Context)
	RemoverAtribuicao(c *gin.Context)
	Papeis(c *gin.Context)
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
	g.DELETE("/:uuid", middleware.SetContextAuthorization(), middleware.ResolveWorkspace(), middleware.RequirePermission(PermRemover), ctrl.Delete)
	g.GET("/:uuid/atribuicoes", middleware.SetContextAuthorization(), middleware.ResolveWorkspace(), middleware.RequirePermission(PermLer), ctrl.Atribuicoes)
	g.POST("/:uuid/atribuicoes", middleware.SetContextAuthorization(), middleware.ResolveWorkspace(), middleware.RequirePermission(PermAtribuirPapel), ctrl.AtribuirPapel)
	g.DELETE("/:uuid/atribuicoes/:atribuicaoUuid", middleware.SetContextAuthorization(), middleware.ResolveWorkspace(), middleware.RequirePermission(PermAtribuirPapel), ctrl.RemoverAtribuicao)
	routes.GET(PrefixoRotasPapeis, middleware.SetContextAuthorization(), middleware.ResolveWorkspace(), middleware.RequirePermission(PermAtribuirPapel), ctrl.Papeis)
}

// Handlers finos: bind → service → c.JSON. Erro sai SÓ por rest_err.WriteError.

// @Summary      Cria um usuário
// @Description  Cria usuário na organization autenticada; a senha nunca volta em resposta
// @Tags         Identidade · Usuário
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Param        request body CreateUserRequestDto true "Dados do usuário"
// @Success      201 {object} UserResponseDto
// @Failure      400 {object} rest_err.RestErr
// @Failure      403 {object} rest_err.RestErr
// @Failure      409 {object} rest_err.RestErr "E-mail já cadastrado nesta organization"
// @Router       /api/domain/identidade/users [post]
func (ctrl *controllerImpl) Create(c *gin.Context) {
	var dto CreateUserRequestDto
	if err := c.ShouldBindJSON(&dto); err != nil {
		rest_err.WriteError(c, traduzir(ErrInvalidInput))
		return
	}
	u, err := ctrl.service.Create(c.Request.Context(), dto.ParaEntrada())
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.JSON(http.StatusCreated, NovoUserResponseDto(u))
}

// @Summary      Consulta um usuário
// @Description  Devolve o usuário no escopo da organization resolvida — usuário de organization alheia não se distingue de inexistente
// @Tags         Identidade · Usuário
// @Produce      json
// @Security     BearerAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Param        uuid path string true "UUID do usuário"
// @Success      200 {object} UserResponseDto
// @Failure      400 {object} rest_err.RestErr "UUID malformado"
// @Failure      403 {object} rest_err.RestErr
// @Failure      404 {object} rest_err.RestErr
// @Router       /api/domain/identidade/users/{uuid} [get]
func (ctrl *controllerImpl) Read(c *gin.Context) {
	id, ok := uuidDoPath(c, "uuid")
	if !ok {
		return
	}
	u, err := ctrl.service.Read(c.Request.Context(), id)
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.JSON(http.StatusOK, NovoUserResponseDto(u))
}

// @Summary      Lista usuários
// @Description  Lista paginada dos usuários da organization; com workspace_uuid lista só quem tem atribuição nele (via atribuição — a tabela de usuário não tem workspace)
// @Tags         Identidade · Usuário
// @Produce      json
// @Security     BearerAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Param        page query int false "Página (default 1)"
// @Param        pageSize query int false "Itens por página (teto 100)"
// @Param        nome query string false "Filtro por nome"
// @Param        email query string false "Filtro por e-mail"
// @Param        status query string false "Filtro por status (ativo|inativo)"
// @Param        workspace_uuid query string false "Lista só usuários atribuídos a este workspace"
// @Success      200 {object} pagination.Response[UserResponseDto]
// @Failure      400 {object} rest_err.RestErr
// @Failure      403 {object} rest_err.RestErr
// @Router       /api/domain/identidade/users [get]
func (ctrl *controllerImpl) List(c *gin.Context) {
	var f modeluser.ListFilter
	if err := c.ShouldBindQuery(&f); err != nil {
		rest_err.WriteError(c, traduzir(ErrInvalidInput))
		return
	}
	f.Pagination = pagination.DoQuery(c) // lê page/pageSize aplicando o teto de 100 (doc 04)
	items, total, err := ctrl.service.List(c.Request.Context(), f)
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.JSON(http.StatusOK, NovoUserListaResponseDto(items, total, f.Pagination))
}

// @Summary      Atualiza um usuário
// @Description  Atualização parcial; inativar encerra imediatamente as sessões abertas do usuário (revogação persistida dos refresh tokens)
// @Tags         Identidade · Usuário
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Param        uuid path string true "UUID do usuário"
// @Param        request body UpdateUserRequestDto true "Campos a atualizar"
// @Success      200 {object} UserResponseDto
// @Failure      400 {object} rest_err.RestErr
// @Failure      403 {object} rest_err.RestErr
// @Failure      404 {object} rest_err.RestErr
// @Failure      422 {object} rest_err.RestErr "Transição de estado proibida"
// @Router       /api/domain/identidade/users/{uuid} [patch]
func (ctrl *controllerImpl) Update(c *gin.Context) {
	id, ok := uuidDoPath(c, "uuid")
	if !ok {
		return
	}
	var dto UpdateUserRequestDto
	if err := c.ShouldBindJSON(&dto); err != nil {
		rest_err.WriteError(c, traduzir(ErrInvalidInput))
		return
	}
	in, err := dto.ParaEntrada()
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	u, err := ctrl.service.Update(c.Request.Context(), id, in)
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.JSON(http.StatusOK, NovoUserResponseDto(u))
}

// @Summary      Remove um usuário
// @Description  Remoção lógica no escopo da organization; sessões abertas são revogadas na hora
// @Tags         Identidade · Usuário
// @Produce      json
// @Security     BearerAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Param        uuid path string true "UUID do usuário"
// @Success      204
// @Failure      400 {object} rest_err.RestErr "UUID malformado"
// @Failure      403 {object} rest_err.RestErr
// @Failure      404 {object} rest_err.RestErr
// @Router       /api/domain/identidade/users/{uuid} [delete]
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

// @Summary      Lista as atribuições do usuário
// @Description  Vínculos user × workspace × papel do usuário no escopo da organization, com o nome canônico de cada papel
// @Tags         Identidade · Usuário
// @Produce      json
// @Security     BearerAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Param        uuid path string true "UUID do usuário"
// @Success      200 {array}  AtribuicaoResponseDto
// @Failure      400 {object} rest_err.RestErr "UUID malformado"
// @Failure      403 {object} rest_err.RestErr
// @Failure      404 {object} rest_err.RestErr
// @Router       /api/domain/identidade/users/{uuid}/atribuicoes [get]
func (ctrl *controllerImpl) Atribuicoes(c *gin.Context) {
	id, ok := uuidDoPath(c, "uuid")
	if !ok {
		return
	}
	itens, err := ctrl.service.Atribuicoes(c.Request.Context(), id)
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.JSON(http.StatusOK, NovasAtribuicoesResponseDto(itens))
}

// @Summary      Atribui um papel ao usuário em um workspace
// @Description  Papéis são POR workspace: dar poder a alguém é operação própria, separada de editar. O workspace precisa pertencer à organization e estar ativo
// @Tags         Identidade · Usuário
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Param        uuid path string true "UUID do usuário"
// @Param        request body AtribuirPapelRequestDto true "Workspace e papel da atribuição"
// @Success      201 {object} AtribuicaoResponseDto
// @Failure      400 {object} rest_err.RestErr
// @Failure      403 {object} rest_err.RestErr
// @Failure      404 {object} rest_err.RestErr "Usuário ou papel não encontrado"
// @Failure      409 {object} rest_err.RestErr "Atribuição duplicada"
// @Failure      422 {object} rest_err.RestErr "Workspace inválido para esta organization"
// @Router       /api/domain/identidade/users/{uuid}/atribuicoes [post]
func (ctrl *controllerImpl) AtribuirPapel(c *gin.Context) {
	id, ok := uuidDoPath(c, "uuid")
	if !ok {
		return
	}
	var dto AtribuirPapelRequestDto
	if err := c.ShouldBindJSON(&dto); err != nil {
		rest_err.WriteError(c, traduzir(ErrInvalidInput))
		return
	}
	workspaceUUID, err := uuid.Parse(dto.WorkspaceUUID)
	if err != nil {
		rest_err.WriteError(c, traduzir(ErrInvalidInput))
		return
	}
	papelUUID, err := uuid.Parse(dto.PapelUUID)
	if err != nil {
		rest_err.WriteError(c, traduzir(ErrInvalidInput))
		return
	}
	a, err := ctrl.service.AtribuirPapel(c.Request.Context(), id, workspaceUUID, papelUUID)
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.JSON(http.StatusCreated, NovoAtribuicaoResponseDto(a))
}

// @Summary      Remove uma atribuição do usuário
// @Description  Retira do usuário o papel exercido no workspace identificado pela atribuição
// @Tags         Identidade · Usuário
// @Produce      json
// @Security     BearerAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Param        uuid path string true "UUID do usuário"
// @Param        atribuicaoUuid path string true "UUID da atribuição"
// @Success      204
// @Failure      400 {object} rest_err.RestErr "UUID malformado"
// @Failure      403 {object} rest_err.RestErr
// @Failure      404 {object} rest_err.RestErr
// @Router       /api/domain/identidade/users/{uuid}/atribuicoes/{atribuicaoUuid} [delete]
func (ctrl *controllerImpl) RemoverAtribuicao(c *gin.Context) {
	id, ok := uuidDoPath(c, "uuid")
	if !ok {
		return
	}
	atribuicaoID, ok := uuidDoPath(c, "atribuicaoUuid")
	if !ok {
		return
	}
	if err := ctrl.service.RemoverAtribuicao(c.Request.Context(), id, atribuicaoID); err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.Status(http.StatusNoContent)
}

// @Summary      Lista os papéis globais da plataforma
// @Description  Referência para o painel montar o Select de atribuição de papéis: uuid, nome e descrição dos papéis seed (super_admin, admin_organization, admin_workspace, usuario_workspace, somente_leitura)
// @Tags         Identidade · Usuário
// @Produce      json
// @Security     BearerAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Success      200 {array}  PapelResponseDto
// @Failure      403 {object} rest_err.RestErr
// @Router       /api/domain/identidade/user/papeis [get]
func (ctrl *controllerImpl) Papeis(c *gin.Context) {
	papeis, err := ctrl.service.Papeis(c.Request.Context())
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.JSON(http.StatusOK, NovosPapeisResponseDto(papeis))
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
		errors.Is(err, ErrEmailEmUso),
		errors.Is(err, ErrCredenciaisInvalidas),
		errors.Is(err, ErrPapelNaoEncontrado),
		errors.Is(err, ErrAtribuicaoDuplicada),
		errors.Is(err, ErrAtribuicaoNaoEncontrada),
		errors.Is(err, ErrWorkspaceInvalido),
		errors.Is(err, ErrRefreshTokenInvalido),
		errors.Is(err, modeluser.ErrEmailInvalido),
		errors.Is(err, modeluser.ErrNomeInvalido),
		errors.Is(err, modeluser.ErrSenhaInvalida),
		errors.Is(err, modeluser.ErrHashAusente),
		errors.Is(err, modeluser.ErrJaInativo),
		errors.Is(err, modeluser.ErrJaAtivo),
		errors.Is(err, modeluser.ErrAtribuicaoInvalida),
		errors.Is(err, modeluser.ErrRefreshInvalido):
		return rest_err.DoCatalogo(err)
	default:
		return rest_err.Interno(err)
	}
}
