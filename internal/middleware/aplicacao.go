package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"

	"workspace-api/internal/pkg/orgctx"
	"workspace-api/internal/pkg/rest_err"
)

// --- Passo 2.5: RequireAplicacao --------------------------------------------

// CabecalhoAplicacao é o header que carrega o módulo selecionado pelo
// cliente — escolhido no login/seletor e repetido em TODA requisição de
// módulo. Sem ele, rota de módulo nega (fail-closed).
const CabecalhoAplicacao = "Application"

// RequireAplicacao exige que a requisição esteja rodando DENTRO do módulo
// declarado na rota: o header Application precisa CASAR com o slug exigido E
// constar nos módulos liberados do par (organization, workspace) resolvido.
// Header ausente/divergente, slug não liberado ou peça faltando no boot =
// o MESMO 403 genérico — não vaza existência de módulo. Sucesso injeta o
// slug no ctx (orgctx.WithAplicacao) para as camadas abaixo.
func (c *Cadeia) RequireAplicacao(slugExigido string) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if c.deps.Aplicacoes == nil {
			responderFechada(ctx, "aplicacao.cadeia_nao_inicializada")
			return
		}
		exigido := strings.TrimSpace(slugExigido)
		informado := strings.TrimSpace(ctx.GetHeader(CabecalhoAplicacao))
		if exigido == "" || !strings.EqualFold(informado, exigido) {
			negarAplicacao(ctx, "aplicacao.header_divergente", exigido, informado)
			return
		}
		organizationUUID := orgctx.OrganizationUUID(ctx.Request.Context())
		workspaceUUID := orgctx.WorkspaceUUID(ctx.Request.Context())
		liberadas, err := c.deps.Aplicacoes.Liberadas(ctx.Request.Context(), organizationUUID, workspaceUUID)
		if err != nil {
			// Falha de infra não vira negativa: sobe como 500 — um 403 aqui
			// mandaria o usuário procurar o administrador para resolver uma
			// indisponibilidade.
			slogErro(ctx, "aplicacao.resolucao_falhou", err)
			rest_err.WriteError(ctx, rest_err.Interno(err))
			return
		}
		if !contemSlug(liberadas, exigido) {
			negarAplicacao(ctx, "aplicacao.sem_licenca", exigido, informado)
			return
		}
		novo := orgctx.WithAplicacao(ctx.Request.Context(), exigido)
		ctx.Request = ctx.Request.WithContext(novo)
		ctx.Next()
	}
}

// negarAplicacao responde o 403 genérico com o vocabulário fechado no log —
// o corpo é idêntico para header divergente e módulo sem licença (não vaza
// qual das duas aconteceu).
func negarAplicacao(ctx *gin.Context, evento, exigido, informado string) {
	slogRecusado(ctx, evento,
		"dominio", "licensing", "acao", "aplicacao", "success", false,
		"aplicacao_exigida", exigido, "aplicacao_informada", informado,
		"organization_uuid", orgctx.OrganizationUUID(ctx.Request.Context()).String(),
		"workspace_uuid", orgctx.WorkspaceUUID(ctx.Request.Context()).String())
	rest_err.WriteError(ctx, rest_err.NewForbiddenError("Aplicação indisponível para este workspace."))
}

// contemSlug casa por igualdade exata — o slug do header precisa estar na
// lista liberada pelo resolvedor (nunca prefixo/sufixo).
func contemSlug(liberadas []string, exigido string) bool {
	for _, slug := range liberadas {
		if slug == exigido {
			return true
		}
	}
	return false
}
