// Ligação da cadeia de middleware com o negócio: adaptadores que implementam
// os contratos de internal/middleware.
//
// O ResolvedorWorkspaces delega ao subdomínio workspace desde a F3 — vive em
// workspace.go. O ResolvedorPermissoes delega ao service do user desde a F4
// (o provisório da F1, que consultava as tabelas direto, saiu) e, desde a
// evolução Redis (#8), vem decorado com o cache perm:{org}:{user}:{wks} de
// cache_redis.go — sem Redis o decorador vira passagem direta. Os contratos
// de organization (domínios custom e X-Api-Key) delegam ao subdomínio —
// organizacao.go.
package bootstrap

import (
	"workspace-api/internal/infra/jwt"
	"workspace-api/internal/middleware"
)

// ligarMiddleware monta a cadeia com adaptadores que resolvem o pool NA
// CHAMADA (regra do AGENTS.md do bootstrap). Chamada UMA vez no boot,
// depois das migrations e antes do registro de rotas.
func ligarMiddleware(gerenciador *jwt.Manager) error {
	return middleware.New(middleware.Dependencias{
		JWT:            gerenciador,
		Workspaces:     resolvedorWorkspaces{},
		DominiosCustom: provedorDominiosCustom{},
		Permissoes:     novoResolvedorPermissoesComCache(resolvedorPermissoesUser{}),
		ApiKeys:        resolvedorApiKeys{},
		Aplicacoes:     novoResolvedorAplicacoesComCache(nil),
	})
}
