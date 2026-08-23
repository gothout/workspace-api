package organization

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	orgmodel "workspace-api/internal/identidade/model/organization"
	"workspace-api/internal/middleware"
	"workspace-api/internal/pkg/pagination"
	"workspace-api/internal/pkg/rest_err"
)

// PrefixoRotas é o path do subdomínio dentro do grupo /api/domain.
const PrefixoRotas = "/identidade/organizations"

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

	// gerenciar_dominio é permissão SEPARADA de editar: apontar domínio muda
	// onde a plataforma responde — não é "editar um campo" (doc 03).
	g.PUT("/:uuid/dominio", middleware.SetContextAuthorization(), middleware.ResolveWorkspace(), middleware.RequirePermission(PermGerenciarDominio), ctrl.DefinirDominio)
	g.DELETE("/:uuid/dominio", middleware.SetContextAuthorization(), middleware.ResolveWorkspace(), middleware.RequirePermission(PermGerenciarDominio), ctrl.RemoverDominio)

	chaves := g.Group("/:uuid/api-keys")
	chaves.POST("", middleware.SetContextAuthorization(), middleware.ResolveWorkspace(), middleware.RequirePermission(PermGerenciarApikeys), ctrl.CriarApiKey)
	chaves.GET("", middleware.SetContextAuthorization(), middleware.ResolveWorkspace(), middleware.RequirePermission(PermGerenciarApikeys), ctrl.ListarApiKeys)
	chaves.DELETE("/:apikey_uuid", middleware.SetContextAuthorization(), middleware.ResolveWorkspace(), middleware.RequirePermission(PermGerenciarApikeys), ctrl.RevogarApiKey)
}

// Handlers finos: bind → service → c.JSON. Erro sai SÓ por rest_err.WriteError.

// @Summary      Cria uma organization
// @Description  Cria a raiz da hierarquia (dona do contrato); acesso restrito a super_admin
// @Tags         Identidade · Organization
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Param        request body CreateOrganizationRequestDto true "Dados da organization"
// @Success      201 {object} OrganizationResponseDto
// @Failure      400 {object} rest_err.RestErr
// @Failure      403 {object} rest_err.RestErr
// @Router       /api/domain/identidade/organizations [post]
func (ctrl *controllerImpl) Create(c *gin.Context) {
	var dto CreateOrganizationRequestDto
	if err := c.ShouldBindJSON(&dto); err != nil {
		rest_err.WriteError(c, traduzir(ErrInvalidInput))
		return
	}
	o, err := ctrl.service.Create(c.Request.Context(), dto.ParaEntrada())
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.JSON(http.StatusCreated, NovoOrganizationResponseDto(o))
}

// @Summary      Consulta uma organization
// @Description  Devolve a organization cujo uuid é o do caminho, se pertencer à organization resolvida na requisição
// @Tags         Identidade · Organization
// @Produce      json
// @Security     BearerAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Param        uuid path string true "UUID da organization"
// @Success      200 {object} OrganizationResponseDto
// @Failure      400 {object} rest_err.RestErr "UUID malformado"
// @Failure      403 {object} rest_err.RestErr
// @Failure      404 {object} rest_err.RestErr
// @Router       /api/domain/identidade/organizations/{uuid} [get]
func (ctrl *controllerImpl) Read(c *gin.Context) {
	id, ok := uuidDoPath(c, "uuid")
	if !ok {
		return
	}
	o, err := ctrl.service.Read(c.Request.Context(), id)
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.JSON(http.StatusOK, NovoOrganizationResponseDto(o))
}

// @Summary      Lista organizations
// @Description  Listagem paginada GLOBAL das organizations (restrita a super_admin/suporte auditado)
// @Tags         Identidade · Organization
// @Produce      json
// @Security     BearerAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Param        page query int false "Página (default 1)"
// @Param        pageSize query int false "Itens por página (teto 100)"
// @Param        nome query string false "Filtro por nome"
// @Success      200 {object} pagination.Response[OrganizationResponseDto]
// @Failure      403 {object} rest_err.RestErr
// @Router       /api/domain/identidade/organizations [get]
func (ctrl *controllerImpl) List(c *gin.Context) {
	var f orgmodel.ListFilter
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
	c.JSON(http.StatusOK, NovoOrganizationListaResponseDto(items, total, f.Pagination))
}

// @Summary      Atualiza uma organization
// @Description  Atualização parcial; inativar via PATCH suspende os workspaces e desativa o domínio custom (cascata auditada)
// @Tags         Identidade · Organization
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Param        uuid path string true "UUID da organization"
// @Param        request body UpdateOrganizationRequestDto true "Campos a atualizar"
// @Success      200 {object} OrganizationResponseDto
// @Failure      400 {object} rest_err.RestErr
// @Failure      403 {object} rest_err.RestErr
// @Failure      404 {object} rest_err.RestErr
// @Failure      422 {object} rest_err.RestErr "Transição de estado proibida"
// @Router       /api/domain/identidade/organizations/{uuid} [patch]
func (ctrl *controllerImpl) Update(c *gin.Context) {
	id, ok := uuidDoPath(c, "uuid")
	if !ok {
		return
	}
	var dto UpdateOrganizationRequestDto
	if err := c.ShouldBindJSON(&dto); err != nil {
		rest_err.WriteError(c, traduzir(ErrInvalidInput))
		return
	}
	in, err := dto.ParaEntrada()
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	o, err := ctrl.service.Update(c.Request.Context(), id, in)
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.JSON(http.StatusOK, NovoOrganizationResponseDto(o))
}

// @Summary      Reativa uma organization
// @Description  Ação de negócio própria: devolve a organization ao ar; workspaces suspensos continuam suspensos até ação explícita
// @Tags         Identidade · Organization
// @Produce      json
// @Security     BearerAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Param        uuid path string true "UUID da organization"
// @Success      200 {object} OrganizationResponseDto
// @Failure      400 {object} rest_err.RestErr "UUID malformado"
// @Failure      403 {object} rest_err.RestErr
// @Failure      404 {object} rest_err.RestErr
// @Failure      422 {object} rest_err.RestErr "Já está ativa"
// @Router       /api/domain/identidade/organizations/{uuid}/acoes/reativar [post]
func (ctrl *controllerImpl) Reativar(c *gin.Context) {
	id, ok := uuidDoPath(c, "uuid")
	if !ok {
		return
	}
	o, err := ctrl.service.Reativar(c.Request.Context(), id)
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.JSON(http.StatusOK, NovoOrganizationResponseDto(o))
}

// @Summary      Remove uma organization
// @Description  Remoção lógica com cascata: suspende os workspaces e desativa a resolução do domínio custom; o valor removido NÃO se libera
// @Tags         Identidade · Organization
// @Produce      json
// @Security     BearerAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Param        uuid path string true "UUID da organization"
// @Success      204
// @Failure      400 {object} rest_err.RestErr "UUID malformado"
// @Failure      403 {object} rest_err.RestErr
// @Failure      404 {object} rest_err.RestErr
// @Router       /api/domain/identidade/organizations/{uuid} [delete]
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

// @Summary      Aponta o domínio custom white-label
// @Description  Registra `*.{dominio}` para os workspaces da organization; valida formato DNS, base_domain e public suffix; único global
// @Tags         Identidade · Organization
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Param        uuid path string true "UUID da organization"
// @Param        request body DefinirDominioRequestDto true "Domínio custom"
// @Success      200 {object} OrganizationResponseDto
// @Failure      400 {object} rest_err.RestErr "Domínio inválido"
// @Failure      403 {object} rest_err.RestErr
// @Failure      404 {object} rest_err.RestErr
// @Failure      409 {object} rest_err.RestErr "Domínio em uso"
// @Router       /api/domain/identidade/organizations/{uuid}/dominio [put]
func (ctrl *controllerImpl) DefinirDominio(c *gin.Context) {
	id, ok := uuidDoPath(c, "uuid")
	if !ok {
		return
	}
	var dto DefinirDominioRequestDto
	if err := c.ShouldBindJSON(&dto); err != nil {
		rest_err.WriteError(c, traduzir(ErrInvalidInput))
		return
	}
	o, err := ctrl.service.DefinirDominio(c.Request.Context(), id, dto.Dominio)
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.JSON(http.StatusOK, NovoOrganizationResponseDto(o))
}

// @Summary      Remove o domínio custom white-label
// @Description  Desliga a resolução `*.{dominio}`; o valor removido não volta a ser registrável (índice único total)
// @Tags         Identidade · Organization
// @Produce      json
// @Security     BearerAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Param        uuid path string true "UUID da organization"
// @Success      200 {object} OrganizationResponseDto
// @Failure      400 {object} rest_err.RestErr "UUID malformado"
// @Failure      403 {object} rest_err.RestErr
// @Failure      404 {object} rest_err.RestErr "Sem domínio definido"
// @Router       /api/domain/identidade/organizations/{uuid}/dominio [delete]
func (ctrl *controllerImpl) RemoverDominio(c *gin.Context) {
	id, ok := uuidDoPath(c, "uuid")
	if !ok {
		return
	}
	o, err := ctrl.service.RemoverDominio(c.Request.Context(), id)
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.JSON(http.StatusOK, NovoOrganizationResponseDto(o))
}

// @Summary      Cria uma chave de API
// @Description  Gera a chave de API da organization; o token em claro vem nesta resposta UMA ÚNICA VEZ — só o SHA-256 é persistido
// @Tags         Identidade · Organization
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Param        uuid path string true "UUID da organization"
// @Param        request body CreateApiKeyRequestDto true "Dados da chave"
// @Success      201 {object} ApiKeyCriadaResponseDto
// @Failure      400 {object} rest_err.RestErr "Entrada inválida (permissão fora do formato, escopo vazio...)"
// @Failure      403 {object} rest_err.RestErr "Sem permissão na rota ou chave pedindo permissão que o criador não possui"
// @Failure      404 {object} rest_err.RestErr
// @Router       /api/domain/identidade/organizations/{uuid}/api-keys [post]
func (ctrl *controllerImpl) CriarApiKey(c *gin.Context) {
	id, ok := uuidDoPath(c, "uuid")
	if !ok {
		return
	}
	var dto CreateApiKeyRequestDto
	if err := c.ShouldBindJSON(&dto); err != nil {
		rest_err.WriteError(c, traduzir(ErrInvalidInput))
		return
	}
	k, chave, err := ctrl.service.CriarApiKey(c.Request.Context(), id, dto.ParaEntrada())
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.JSON(http.StatusCreated, NovoApiKeyCriadaResponseDto(k, chave))
}

// @Summary      Lista as chaves de API
// @Description  Lista paginada dos metadados das chaves da organization — nunca expõe hash nem segredo
// @Tags         Identidade · Organization
// @Produce      json
// @Security     BearerAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Param        uuid path string true "UUID da organization"
// @Param        page query int false "Página (default 1)"
// @Param        pageSize query int false "Itens por página (teto 100)"
// @Success      200 {object} pagination.Response[ApiKeyResponseDto]
// @Failure      403 {object} rest_err.RestErr
// @Failure      404 {object} rest_err.RestErr
// @Router       /api/domain/identidade/organizations/{uuid}/api-keys [get]
func (ctrl *controllerImpl) ListarApiKeys(c *gin.Context) {
	id, ok := uuidDoPath(c, "uuid")
	if !ok {
		return
	}
	p := pagination.DoQuery(c)
	items, total, err := ctrl.service.ListarApiKeys(c.Request.Context(), id, p)
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.JSON(http.StatusOK, NovoApiKeyListaResponseDto(items, total, p))
}

// @Summary      Revoga uma chave de API
// @Description  Remoção lógica da chave; requisições com ela passam a falhar fechadas imediatamente
// @Tags         Identidade · Organization
// @Produce      json
// @Security     BearerAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Param        uuid path string true "UUID da organization"
// @Param        apikey_uuid path string true "UUID da chave"
// @Success      204
// @Failure      400 {object} rest_err.RestErr "UUID malformado"
// @Failure      403 {object} rest_err.RestErr
// @Failure      404 {object} rest_err.RestErr
// @Router       /api/domain/identidade/organizations/{uuid}/api-keys/{apikey_uuid} [delete]
func (ctrl *controllerImpl) RevogarApiKey(c *gin.Context) {
	id, ok := uuidDoPath(c, "uuid")
	if !ok {
		return
	}
	chaveID, ok := uuidDoPath(c, "apikey_uuid")
	if !ok {
		return
	}
	if err := ctrl.service.RevogarApiKey(c.Request.Context(), id, chaveID); err != nil {
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
		errors.Is(err, ErrDominioEmUso),
		errors.Is(err, ErrChaveEmUso),
		errors.Is(err, ErrApiKeyNaoEncontrada),
		errors.Is(err, ErrPermissaoNaoPossuida),
		errors.Is(err, orgmodel.ErrNomeInvalido),
		errors.Is(err, orgmodel.ErrDominioInvalido),
		errors.Is(err, orgmodel.ErrDominioNaoDefinido),
		errors.Is(err, orgmodel.ErrJaInativo),
		errors.Is(err, orgmodel.ErrJaAtivo),
		errors.Is(err, orgmodel.ErrApiKeyInvalida),
		errors.Is(err, orgmodel.ErrPermissaoInvalida),
		errors.Is(err, orgmodel.ErrEscopoApiKeyInvalido):
		return rest_err.DoCatalogo(err)
	default:
		return rest_err.Interno(err)
	}
}
