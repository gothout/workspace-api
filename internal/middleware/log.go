package middleware

import (
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"workspace-api/internal/pkg/orgctx"
)

// slogRecusado emite o evento de recusa com payload montado à mão —
// identificadores e vocabulário fechado, nunca texto livre (agents/04).
func slogRecusado(ctx *gin.Context, evento string, pares ...any) {
	args := append([]any{"evento", evento, "ray_trace", orgctx.RayTrace(ctx.Request.Context())}, pares...)
	slog.WarnContext(ctx.Request.Context(), "middleware.recusou", args...)
}

// slogErro registra falha de infraestrutura — sobe intacta como 500, nunca
// vira negativa de acesso (indisponibilidade não é culpa do usuário).
func slogErro(ctx *gin.Context, evento string, causa error) {
	slog.ErrorContext(ctx.Request.Context(), "middleware.falhou",
		"evento", evento,
		"ray_trace", orgctx.RayTrace(ctx.Request.Context()),
		"causa", causa.Error())
}

// rayTraceDaRequisicao reaproveita o X-Request-Id quando presente; sem ele
// gera um uuid novo — correlação de log da ponta da cadeia para baixo.
func rayTraceDaRequisicao(ctx *gin.Context) string {
	if id := ctx.GetHeader("X-Request-Id"); id != "" {
		return id
	}
	return uuid.NewString()
}
