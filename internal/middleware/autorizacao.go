package middleware

import (
	"context"
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"workspace-api/internal/infra/jwt"
	"workspace-api/internal/pkg/orgctx"
	"workspace-api/internal/pkg/rest_err"
)

// --- Identidade da requisição (ctx interno do pacote) ----------------------

type chave int

const chaveIdentidade chave = iota

type tipoIdentidade string

const (
	identidadeHumana tipoIdentidade = "humana" // JWT Bearer
	identidadeChave  tipoIdentidade = "chave"  // X-Api-Key
)

type identidade struct {
	tipo   tipoIdentidade
	claims *jwt.Claims      // quando humana
	chave  *IdentidadeChave // quando chave
}

func (i *identidade) usuarioUUID() uuid.UUID {
	if i == nil || i.claims == nil {
		return uuid.Nil
	}
	id, _ := uuid.Parse(i.claims.UserUUID)
	return id
}

func identidadeDo(ctx context.Context) *identidade {
	v, _ := ctx.Value(chaveIdentidade).(*identidade)
	return v
}

// --- Passo 1: SetContextAuthorization ---------------------------------------

// SetContextAuthorization responde "quem está falando?": valida o JWT Bearer
// ou a X-Api-Key e injeta a identidade no ctx. Falha de credencial é 401
// genérico — o motivo exato (expirado, assinatura, formato) fica no log.
func (c *Cadeia) SetContextAuthorization() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if c.deps.JWT == nil {
			responderFechada(ctx, "auth.cadeia_nao_inicializada")
			return
		}
		ray := rayTraceDaRequisicao(ctx)
		ctx.Request = ctx.Request.WithContext(orgctx.WithRayTrace(ctx.Request.Context(), ray))

		if token := bearerToken(ctx); token != "" {
			c.autorizarHumana(ctx, token)
			return
		}
		if chave := ctx.GetHeader("X-Api-Key"); chave != "" {
			c.autorizarPorChave(ctx, chave)
			return
		}
		slogRecusado(ctx, "auth.sem_credencial", "dominio", "identidade", "acao", "autenticar", "success", false)
		rest_err.WriteError(ctx, rest_err.NewUnauthorizedError("Credenciais ausentes ou inválidas."))
	}
}

// autorizarHumana valida o access token e injeta user/org no escopo do ctx.
func (c *Cadeia) autorizarHumana(ctx *gin.Context, token string) {
	claims, err := c.deps.JWT.Validar(token)
	if err != nil || claims.Tipo != jwt.ClaimTipoAccess {
		slogRecusado(ctx, "auth.token_recusado", "dominio", "identidade", "acao", "autenticar",
			"success", false, "motivo_interno", motivoInterno(err))
		rest_err.WriteError(ctx, rest_err.NewUnauthorizedError("Credenciais ausentes ou inválidas."))
		return
	}
	injetarIdentidade(ctx, &identidade{tipo: identidadeHumana, claims: claims})
}

// autorizarPorChave valida a X-Api-Key pelo contrato; sem resolvedor ligado
// (até a F2) TODA chave falha 401 — fail-closed.
func (c *Cadeia) autorizarPorChave(ctx *gin.Context, chave string) {
	if c.deps.ApiKeys == nil {
		slogRecusado(ctx, "auth.apikey_sem_resolvedor", "dominio", "identidade", "acao", "autenticar", "success", false)
		rest_err.WriteError(ctx, rest_err.NewUnauthorizedError("Credenciais ausentes ou inválidas."))
		return
	}
	achada, err := c.deps.ApiKeys.BuscarPorChave(ctx.Request.Context(), chave)
	if err != nil && !errors.Is(err, ErrNaoEncontrado) {
		slogErro(ctx, "auth.apikey_falhou", err)
		rest_err.WriteError(ctx, rest_err.Interno(err))
		return
	}
	if err != nil || achada == nil { // não registrada = credencial inválida
		slogRecusado(ctx, "auth.apikey_invalida", "dominio", "identidade", "acao", "autenticar", "success", false)
		rest_err.WriteError(ctx, rest_err.NewUnauthorizedError("Credenciais ausentes ou inválidas."))
		return
	}
	injetarIdentidade(ctx, &identidade{tipo: identidadeChave, chave: achada})
}

func injetarIdentidade(ctx *gin.Context, id *identidade) {
	novo := ctx.Request.Context()
	switch id.tipo {
	case identidadeHumana:
		novo = orgctx.WithUser(novo, id.usuarioUUID())
		if org, err := uuid.Parse(id.claims.OrganizationUUID); err == nil && org != uuid.Nil {
			novo = orgctx.WithOrganization(novo, org)
		}
	case identidadeChave:
		if id.chave.EscopoOrganization || len(id.chave.WorkspacesPermitidos) > 0 {
			novo = orgctx.WithOrganization(novo, id.chave.OrganizationUUID)
		}
	}
	ctx.Request = ctx.Request.WithContext(context.WithValue(novo, chaveIdentidade, id))
}

// --- Passo 3: RequirePermission ---------------------------------------------

// RequirePermission exige a permissão granular EXATA declarada NA ROTA
// (string dominio:subdominio:acao). Exigência vazia NEGA — exigência que não
// sabe o que exigir nunca vira liberação. Curingas só existem nos papéis
// admin (*:* e dominio:subdominio:*), casados pelo matcher abaixo.
func (c *Cadeia) RequirePermission(permissaoExigida string) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if c.deps.Permissoes == nil {
			responderFechada(ctx, "permissao.cadeia_nao_inicializada")
			return
		}
		if strings.TrimSpace(permissaoExigida) == "" {
			negarPermissao(ctx, permissaoExigida)
			return
		}
		if !permissaoAtende(orgctx.Permissoes(ctx.Request.Context()), permissaoExigida) {
			negarPermissao(ctx, permissaoExigida)
			return
		}
		ctx.Next()
	}
}

func negarPermissao(ctx *gin.Context, exigida string) {
	slogRecusado(ctx, "permissao.negada",
		"dominio", "identidade", "acao", "autorizar", "success", false,
		"permissao_exigida", exigida,
		"user_uuid", orgctx.UserUUID(ctx.Request.Context()).String(),
		"organization_uuid", orgctx.OrganizationUUID(ctx.Request.Context()).String(),
		"workspace_uuid", orgctx.WorkspaceUUID(ctx.Request.Context()).String())
	rest_err.WriteError(ctx, rest_err.NewForbiddenError("Sem permissão para esta operação."))
}

// responderFechada fecha a rota com 403 quando a cadeia não sobeu completa —
// nunca pânico, nunca rota aberta (AGENTS.md do pacote).
func responderFechada(ctx *gin.Context, evento string) {
	slogRecusado(ctx, evento, "dominio", "identidade", "acao", "autorizar", "success", false)
	rest_err.WriteError(ctx, rest_err.NewForbiddenError("Operação indisponível nesta instância."))
}

// permissaoAtende casa a exigida contra as efetivas: igualdade exata,
// curinga por segmento (`identidade:user:*` libera as ações de user) ou o
// curinga global do super_admin (`*:*`). Exigida malformada não é atendida.
func permissaoAtende(efetivas []string, exigida string) bool {
	const curingaGlobal = "*:*"
	partesExigida := strings.Split(exigida, ":")
	if len(partesExigida) != 3 {
		return false
	}
	for _, efetiva := range efetivas {
		if efetiva == curingaGlobal {
			return true
		}
		parts := strings.Split(efetiva, ":")
		if len(parts) != 3 {
			continue
		}
		if atendeSegmento(parts[0], partesExigida[0]) &&
			atendeSegmento(parts[1], partesExigida[1]) &&
			atendeSegmento(parts[2], partesExigida[2]) {
			return true
		}
	}
	return false
}

func atendeSegmento(efetivo, exigido string) bool {
	return efetivo == "*" || efetivo == exigido
}

// --- Helpers ----------------------------------------------------------------

func bearerToken(ctx *gin.Context) string {
	const prefixo = "Bearer "
	bruto := ctx.GetHeader("Authorization")
	if len(bruto) <= len(prefixo) || !strings.EqualFold(bruto[:len(prefixo)], prefixo) {
		return ""
	}
	return strings.TrimSpace(bruto[len(prefixo):])
}

// motivoInterno distingue expirado/inválido SÓ para o log estruturado.
func motivoInterno(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
