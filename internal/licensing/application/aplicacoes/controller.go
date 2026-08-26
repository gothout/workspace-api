package aplicacoes

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"workspace-api/internal/middleware"
	"workspace-api/internal/pkg/rest_err"
)

// PrefixoRotas é o path da aplicação dentro do grupo /api/application.
const PrefixoRotas = "/licensing/minhas-aplicacoes"

type Controller interface {
	Routes(routes gin.IRouter)
	MinhasAplicacoes(c *gin.Context)
}

type controllerImpl struct{ service Service }

func NewController(service Service) Controller { return &controllerImpl{service: service} }

// Routes declara a cadeia ROTA A ROTA — nunca no grupo (doc 03). A rota roda
// SEM RequireAplicacao (não há app selecionado ainda: é o próprio seletor) e
// SEM RequirePermission (qualquer usuário com vínculo no workspace vê a
// lista — é a porta de entrada dos módulos).
func (ctrl *controllerImpl) Routes(routes gin.IRouter) {
	g := routes.Group(PrefixoRotas)
	g.GET("", middleware.SetContextAuthorization(), middleware.ResolveWorkspace(), ctrl.MinhasAplicacoes)
}

// @Summary      Lista as aplicações liberadas
// @Description  Módulos usáveis no workspace resolvido (licença ∩ ativação ∩ módulo ativo) — alimenta o seletor de aplicações do front
// @Tags         Licensing · Aplicações
// @Produce      json
// @Security     BearerAuth
// @Success      200 {object} MinhasAplicacoesResponseDto
// @Failure      400 {object} rest_err.RestErr "Escopo ausente"
// @Failure      401 {object} rest_err.RestErr
// @Failure      403 {object} rest_err.RestErr
// @Router       /api/application/licensing/minhas-aplicacoes [get]
func (ctrl *controllerImpl) MinhasAplicacoes(c *gin.Context) {
	itens, err := ctrl.service.MinhasAplicacoes(c.Request.Context())
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.JSON(http.StatusOK, MinhasAplicacoesResponseDto{Aplicacoes: itens})
}

// traduzir converte a sentinela no rest_err do catálogo; desconhecido = 500.
func traduzir(err error) *rest_err.RestErr {
	switch {
	case errors.Is(err, ErrInvalidInput):
		return rest_err.DoCatalogo(ErrInvalidInput)
	default:
		return rest_err.Interno(err)
	}
}
