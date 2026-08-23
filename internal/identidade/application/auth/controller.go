package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"workspace-api/internal/pkg/rest_err"
)

// PrefixoRotas é o path da aplicação dentro do grupo /api/application.
const PrefixoRotas = "/identidade/auth"

type Controller interface {
	Routes(routes gin.IRouter)
	Login(c *gin.Context)
	Refresh(c *gin.Context)
	Logout(c *gin.Context)
}

type controllerImpl struct{ service Service }

func NewController(service Service) Controller { return &controllerImpl{service: service} }

// Routes NÃO usa a cadeia completa (doc 03): login é COMO se consegue a
// identidade — não há token para validar nem workspace/permissão a exigir.
// A resolução da organization pelo Host acontece DENTRO do service, pelo
// contrato ResolvedorOrganization; host sem organization resolvível vira o
// MESMO 401 genérico de credenciais inválidas. Logout/refresh validam o
// refresh token do corpo, não um Bearer.
func (ctrl *controllerImpl) Routes(routes gin.IRouter) {
	g := routes.Group(PrefixoRotas)
	g.POST("/login", ctrl.Login)
	g.POST("/refresh", ctrl.Refresh)
	g.POST("/logout", ctrl.Logout)
}

// Handlers finos: bind → service → c.JSON. Erro sai SÓ por rest_err.WriteError.

// @Summary      Autentica e abre uma sessão
// @Description  Login com e-mail/senha contra a organization resolvida pelo Host (subdomínio de workspace ou domínio custom). Falhas são indistinguíveis: usuário inexistente, senha errada e host sem organization devolvem o mesmo 401 genérico. Com Redis ligado, repetidas falhas do par e-mail+IP aplicam lockout temporário (429)
// @Tags         Identidade · Auth
// @Accept       json
// @Produce      json
// @Param        request body LoginRequestDto true "Credenciais"
// @Success      200 {object} SessaoResponseDto
// @Failure      400 {object} rest_err.RestErr "Corpo malformado"
// @Failure      401 {object} rest_err.RestErr "Credenciais inválidas (genérico)"
// @Failure      429 {object} rest_err.RestErr "Muitas tentativas — login bloqueado temporariamente"
// @Router       /api/application/identidade/auth/login [post]
func (ctrl *controllerImpl) Login(c *gin.Context) {
	var dto LoginRequestDto
	if err := c.ShouldBindJSON(&dto); err != nil {
		rest_err.WriteError(c, traduzir(ErrInvalidInput))
		return
	}
	sessao, err := ctrl.service.Login(c.Request.Context(), c.Request.Host, LoginEntrada{
		Email: dto.Email,
		Senha: dto.Senha,
		IP:    c.ClientIP(),
	})
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.JSON(http.StatusOK, sessao)
}

// @Summary      Renova a sessão
// @Description  Troca um refresh token válido por um novo par de tokens; o jti precisa seguir ativo no Postgres (revogação persistida)
// @Tags         Identidade · Auth
// @Accept       json
// @Produce      json
// @Param        request body TokenRequestDto true "Refresh token vigente"
// @Success      200 {object} SessaoResponseDto
// @Failure      400 {object} rest_err.RestErr "Corpo malformado"
// @Failure      401 {object} rest_err.RestErr "Sessão inválida ou expirada"
// @Router       /api/application/identidade/auth/refresh [post]
func (ctrl *controllerImpl) Refresh(c *gin.Context) {
	var dto TokenRequestDto
	if err := c.ShouldBindJSON(&dto); err != nil {
		rest_err.WriteError(c, traduzir(ErrInvalidInput))
		return
	}
	sessao, err := ctrl.service.Refresh(c.Request.Context(), dto.RefreshToken)
	if err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.JSON(http.StatusOK, sessao)
}

// @Summary      Encerra a sessão
// @Description  Revoga o refresh token no Postgres (marca revogado_em; a linha nunca é removida). Idempotente: repetir com token já revogado é sucesso — só assinatura/tipo/claims inválidos recusam.
// @Tags         Identidade · Auth
// @Accept       json
// @Produce      json
// @Param        request body TokenRequestDto true "Refresh token a revogar"
// @Success      204
// @Failure      400 {object} rest_err.RestErr "Corpo malformado"
// @Failure      401 {object} rest_err.RestErr "Sessão inválida ou expirada"
// @Router       /api/application/identidade/auth/logout [post]
func (ctrl *controllerImpl) Logout(c *gin.Context) {
	var dto TokenRequestDto
	if err := c.ShouldBindJSON(&dto); err != nil {
		rest_err.WriteError(c, traduzir(ErrInvalidInput))
		return
	}
	if err := ctrl.service.Logout(c.Request.Context(), dto.RefreshToken); err != nil {
		rest_err.WriteError(c, traduzir(err))
		return
	}
	c.Status(http.StatusNoContent)
}

// traduzir converte a sentinela no rest_err do catálogo da aplicação;
// desconhecido = 500.
func traduzir(err error) *rest_err.RestErr {
	switch {
	case err == nil:
		return rest_err.Interno(nil)
	case err == ErrCredenciaisInvalidas,
		err == ErrSessaoInvalida,
		err == ErrLoginBloqueado,
		err == ErrInvalidInput:
		return rest_err.DoCatalogo(err)
	default:
		return rest_err.Interno(err)
	}
}
