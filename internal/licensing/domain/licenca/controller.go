package licenca

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"workspace-api/internal/middleware"
	"workspace-api/internal/pkg/rest_err"
)

// PrefixoRotas é o path do subdomínio dentro do grupo /api/domain. A
// organization ALVO vem do path: atribuir/revogar cruzam organizations
// (super_admin); leitura respeita a fronteira própria/alheia no service.
const PrefixoRotas = "/licensing/organizations/:organization_uuid/licencas"

type Controller interface {
	Routes(routes gin.IRouter)
	Atribuir(c *gin.Context)
	Listar(c *gin.Context)
	Revogar(c *gin.Context)
}

type controllerImpl struct{ service Service }

func NewController(service Service) Controller { return &controllerImpl{service: service} }

// Routes declara a cadeia ROTA A ROTA — nunca no grupo (doc 03).
func (ctrl *controllerImpl) Routes(routes gin.IRouter) {
	g := routes.Group(PrefixoRotas)
	g.POST("", middleware.SetContextAuthorization(), middleware.ResolveWorkspace(), middleware.RequirePermission(PermAtribuir), ctrl.Atribuir)
	g.GET("", middleware.SetContextAuthorization(), middleware.ResolveWorkspace(), middleware.RequirePermission(PermLer), ctrl.Listar)
	g.DELETE("/:uuid", middleware.SetContextAuthorization(), middleware.ResolveWorkspace(), middleware.RequirePermission(PermRemover), ctrl.Revogar)
}

// organizationAlvo parseia e devolve a organization do path; malformada = 400.
func organizationAlvo(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("organization_uuid"))
	if err != nil || id == uuid.Nil {
		rest_err.WriteError(c, traduzir(ErrInvalidInput))
		return uuid.Nil, false
	}
	return id, true
}

// @Summary      Atribui uma licença de módulo
// @Description  Concede ao módulo informado uma licença viva para a organization alvo (super_admin)
// @Tags         Licensing · Licenças
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        organization_uuid path string true "UUID da organization alvo"
// @Param        request body AtribuirLicencaRequestDto true "Módulo a licenciar"
// @Success      201 {object} LicencaResponseDto
// @Failure      400 {object} rest_err.RestErr
// @Failure      403 {object} rest_err.RestErr
// @Failure      404 {object} rest_err.RestErr "Organization ou módulo inexistente"
// @Failure      409 {object} rest_err.RestErr "Licença já concedida"
// @Router       /api/domain/licensing/organizations/{organization_uuid}/licencas [post]
func (ctrl *controllerImpl) Atribuir(c *gin.Context) {
	alvo, ok := organizationAlvo(c)
	if !ok {
		return
	}
	var dto AtribuirLicencaRequestDto
	if err := c.ShouldBindJSON(&dto); err != nil {
		rest_err.WriteError(c, traduzir(ErrInvalidInput))
		return
	}
	l, err := ctrl.service.Atribuir(c.Request.Context(), alvo, dto.ModuloSlug)
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.JSON(http.StatusCreated, NovoLicencaResponseDto(l))
}

// @Summary      Lista as licenças de uma organization
// @Description  Própria organization exige licensing:licenca:ler; alheia exige ler_plataforma (super_admin)
// @Tags         Licensing · Licenças
// @Produce      json
// @Security     BearerAuth
// @Param        organization_uuid path string true "UUID da organization"
// @Success      200 {array}  LicencaResponseDto
// @Failure      400 {object} rest_err.RestErr
// @Failure      403 {object} rest_err.RestErr "Organization alheia sem permissão de plataforma"
// @Router       /api/domain/licensing/organizations/{organization_uuid}/licencas [get]
func (ctrl *controllerImpl) Listar(c *gin.Context) {
	alvo, ok := organizationAlvo(c)
	if !ok {
		return
	}
	items, err := ctrl.service.Listar(c.Request.Context(), alvo)
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.JSON(http.StatusOK, NovoLicencaListaResponseDto(items))
}

// @Summary      Revoga uma licença
// @Description  Remove a licença viva (super_admin); ativações do módulo ficam inertes até nova concessão
// @Tags         Licensing · Licenças
// @Produce      json
// @Security     BearerAuth
// @Param        organization_uuid path string true "UUID da organization alvo"
// @Param        uuid path string true "UUID da licença"
// @Success      204
// @Failure      400 {object} rest_err.RestErr
// @Failure      403 {object} rest_err.RestErr
// @Failure      404 {object} rest_err.RestErr
// @Router       /api/domain/licensing/organizations/{organization_uuid}/licencas/{uuid} [delete]
func (ctrl *controllerImpl) Revogar(c *gin.Context) {
	alvo, ok := organizationAlvo(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("uuid"))
	if err != nil {
		rest_err.WriteError(c, traduzir(ErrInvalidInput))
		return
	}
	if err := ctrl.service.Revogar(c.Request.Context(), alvo, id); err != nil {
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
	case errors.Is(err, ErrJaConcedida):
		return rest_err.DoCatalogo(ErrJaConcedida)
	case errors.Is(err, ErrSemAcesso):
		return rest_err.DoCatalogo(ErrSemAcesso)
	case errors.Is(err, ErrModuloInvalido):
		return rest_err.DoCatalogo(ErrModuloInvalido)
	default:
		return rest_err.Interno(err)
	}
}
