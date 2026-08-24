package provisionamento

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	modelworkspace "workspace-api/internal/identidade/model/workspace"
	"workspace-api/internal/middleware"
	"workspace-api/internal/pkg/rest_err"
)

// PrefixoRotas reaproveita o path da raiz organization: o provisionamento é
// uma AÇÃO sobre a organization (doc 04 — ação de negócio no recurso), mas o
// controller é da aplicação porque a orquestração atravessa os três
// subdomínios. O parâmetro é :uuid — MESMO nome do grupo do subdomínio
// organization para não colidir no gin.
const PrefixoRotas = "/identidade/organizations"

type Controller interface {
	Routes(routes gin.IRouter)
	Provisionar(c *gin.Context)
}

type controllerImpl struct{ service Service }

func NewController(service Service) Controller { return &controllerImpl{service: service} }

// Routes declara a cadeia ROTA A ROTA — nunca no grupo (doc 03). No console
// master (painel.{base_domain}) não há workspace a resolver: ResolveWorkspace
// segue sem escopo e a permissão rota a rota decide quem passa.
func (ctrl *controllerImpl) Routes(routes gin.IRouter) {
	g := routes.Group(PrefixoRotas)
	g.POST("/:uuid/provisionamento",
		middleware.SetContextAuthorization(),
		middleware.ResolveWorkspace(),
		middleware.RequirePermission(PermExecutar),
		ctrl.Provisionar)
}

// @Summary      Provisiona o admin inicial e o workspace inicial de uma organization
// @Description  Função da PLATAFORMA (super_admin): cria o admin inicial (nome, e-mail e senha definidos pelo chamador; a senha nunca é gerada nem devolvida em claro) e o workspace inicial com o slug informado, atribuindo o papel admin_organization. Idempotente: organization que já possui workspace responde 409 ja_provisionado; peças de tentativa anterior interrompida são reconhecidas, não duplicadas
// @Tags         Identidade · Provisionamento
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Param        uuid path string true "UUID da organization"
// @Param        request body ProvisionamentoRequestDto true "Dados do admin e do workspace inicial"
// @Success      201 {object} ProvisionamentoResponseDto
// @Failure      400 {object} rest_err.RestErr "Entrada inválida"
// @Failure      403 {object} rest_err.RestErr "Restrito à plataforma"
// @Failure      404 {object} rest_err.RestErr "Organization não encontrada"
// @Failure      409 {object} rest_err.RestErr "Já provisionada / slug ou e-mail em uso"
// @Failure      422 {object} rest_err.RestErr "Organization inativa"
// @Router       /api/domain/identidade/organizations/{uuid}/provisionamento [post]
func (ctrl *controllerImpl) Provisionar(c *gin.Context) {
	alvo, err := uuid.Parse(c.Param("uuid"))
	if err != nil {
		rest_err.WriteError(c, traduzir(ErrInvalidInput))
		return
	}
	var dto ProvisionamentoRequestDto
	if err := c.ShouldBindJSON(&dto); err != nil {
		rest_err.WriteError(c, traduzir(ErrInvalidInput))
		return
	}
	resp, err := ctrl.service.Provisionar(c.Request.Context(), alvo, dto)
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.JSON(http.StatusCreated, resp)
}

// traduzir converte a sentinela no rest_err do catálogo da aplicação;
// desconhecido = 500. As sentinelas dos VOs dos modelos são resolvidas pelo
// MESMO registro global (errors.Is atravessa os pacotes).
func traduzir(err error) *rest_err.RestErr {
	switch {
	case errors.Is(err, ErrInvalidInput),
		errors.Is(err, ErrOrganizacaoNaoEncontrada),
		errors.Is(err, ErrOrganizacaoInativa),
		errors.Is(err, ErrJaProvisionado),
		errors.Is(err, ErrSlugIndisponivel),
		errors.Is(err, ErrEmailEmUso),
		errors.Is(err, ErrSemPoderPlataforma),
		errors.Is(err, ErrPapelAusente),
		errors.Is(err, modelworkspace.ErrSlugInvalido),
		errors.Is(err, modelworkspace.ErrNomeInvalido):
		return rest_err.DoCatalogo(err)
	default:
		return rest_err.Interno(err)
	}
}
