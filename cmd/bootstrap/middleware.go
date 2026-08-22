// Ligação da cadeia de middleware com o negócio: adaptadores que implementam
// os contratos de internal/middleware.
//
// O ResolvedorWorkspaces delega ao subdomínio workspace desde a F3 — vive em
// workspace.go, resolvendo o singleton NA CHAMADA. O resolvedor de
// permissões segue PROVISÓRIO DOCUMENTADO (issue #2 / agents/03): consulta o
// SQL canônico das tabelas de autorização direto; na F4 passa a delegar ao
// service do user. Os contratos do middleware NÃO mudam. Os contratos de
// organization (domínios custom e X-Api-Key) delegam ao subdomínio —
// organizacao.go.
package bootstrap

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"workspace-api/internal/infra/database/postgres"
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
		Permissoes:     resolvedorPermissoes{},
		ApiKeys:        resolvedorApiKeys{},
	})
}

// --- Contrato ResolvedorPermissoes -------------------------------------------

// resolvedorPermissoes cruza user × atribuição × papel × permissão direto no
// SQL canônico (provisório — na F4 delega ao service do user).
type resolvedorPermissoes struct{}

// TemVinculo confere atribuição direta no workspace OU concessão de suporte:
// super_admin em qualquer organization, admin_organization na própria.
// A consulta roda só aqui — nunca no caminho quente de permissões.
func (resolvedorPermissoes) TemVinculo(ctx context.Context, usuarioUUID, organizationUUID, workspaceUUID uuid.UUID) (bool, error) {
	db, err := postgres.GetDB()
	if err != nil {
		return false, err
	}
	return temVinculoNo(ctx, db, usuarioUUID, organizationUUID, workspaceUUID)
}

// temVinculoNo é a FUNÇÃO PURA por trás do adaptador — testada direto com
// banco efêmero, sem passar pelo singleton do processo.
func temVinculoNo(ctx context.Context, db *gorm.DB, usuarioUUID, organizationUUID, workspaceUUID uuid.UUID) (bool, error) {
	var atribuicoes int64
	err := db.WithContext(ctx).
		Table("identidade_user_atribuicao").
		Where("user_uuid = ? AND organization_uuid = ? AND workspace_uuid = ? AND deleted_at IS NULL",
			usuarioUUID, organizationUUID, workspaceUUID).
		Count(&atribuicoes).Error
	if err != nil {
		return false, err
	}
	if atribuicoes > 0 {
		return true, nil
	}

	// Suporte: super_admin atravessa qualquer organization.
	var superAdmin int64
	err = db.WithContext(ctx).
		Table("identidade_user_atribuicao a").
		Joins("JOIN identidade_user_papel p ON p.uuid = a.papel_uuid").
		Where("a.user_uuid = ? AND p.nome = ? AND a.deleted_at IS NULL", usuarioUUID, papelSuperAdmin).
		Count(&superAdmin).Error
	if err != nil {
		return false, err
	}
	if superAdmin > 0 {
		registrarSuporte(ctx, usuarioUUID, organizationUUID, workspaceUUID, papelSuperAdmin)
		return true, nil
	}

	// Suporte: admin_organization entra nos workspaces da PRÓPRIA organization.
	var adminOrg int64
	err = db.WithContext(ctx).
		Table("identidade_user_atribuicao a").
		Joins("JOIN identidade_user_papel p ON p.uuid = a.papel_uuid").
		Where("a.user_uuid = ? AND a.organization_uuid = ? AND p.nome = ? AND a.deleted_at IS NULL",
			usuarioUUID, organizationUUID, papelAdminOrganization).
		Count(&adminOrg).Error
	if err != nil {
		return false, err
	}
	if adminOrg > 0 {
		registrarSuporte(ctx, usuarioUUID, organizationUUID, workspaceUUID, papelAdminOrganization)
		return true, nil
	}
	return false, nil
}

// PermissoesEfetivas devolve a UNIÃO das permissões dos papéis do usuário no
// workspace (atribuição direta) mais as dos papéis de suporte que ele exerce
// — curingas inclusos. Sem cache no núcleo (Redis é evolução futura).
func (resolvedorPermissoes) PermissoesEfetivas(ctx context.Context, usuarioUUID, organizationUUID, workspaceUUID uuid.UUID) ([]string, error) {
	db, err := postgres.GetDB()
	if err != nil {
		return nil, err
	}
	return permissoesEfetivasNo(ctx, db, usuarioUUID, organizationUUID, workspaceUUID)
}

// permissoesEfetivasNo é a FUNÇÃO PURA por trás do adaptador.
func permissoesEfetivasNo(ctx context.Context, db *gorm.DB, usuarioUUID, organizationUUID, workspaceUUID uuid.UUID) ([]string, error) {
	var permissoes []string
	err := db.WithContext(ctx).Raw(`
SELECT DISTINCT pp.permissao
FROM identidade_user_atribuicao a
JOIN identidade_user_papel_permissao pp ON pp.papel_uuid = a.papel_uuid
WHERE a.user_uuid = ? AND a.workspace_uuid = ? AND a.deleted_at IS NULL

UNION

SELECT DISTINCT pp.permissao
FROM identidade_user_papel p
JOIN identidade_user_papel_permissao pp ON pp.papel_uuid = p.uuid
WHERE p.nome = ?
  AND EXISTS (
    SELECT 1 FROM identidade_user_atribuicao sa
    WHERE sa.user_uuid = ? AND sa.papel_uuid = p.uuid AND sa.deleted_at IS NULL)

UNION

SELECT DISTINCT pp.permissao
FROM identidade_user_papel p
JOIN identidade_user_papel_permissao pp ON pp.papel_uuid = p.uuid
WHERE p.nome = ?
  AND EXISTS (
    SELECT 1 FROM identidade_user_atribuicao sa
    WHERE sa.user_uuid = ? AND sa.organization_uuid = ? AND sa.papel_uuid = p.uuid
      AND sa.deleted_at IS NULL)
`, usuarioUUID, workspaceUUID, papelSuperAdmin, usuarioUUID,
		papelAdminOrganization, usuarioUUID, organizationUUID).Scan(&permissoes).Error
	if err != nil {
		return nil, err
	}
	if permissoes == nil {
		permissoes = []string{}
	}
	return permissoes, nil
}

// registrarSuporte audita a concessão (quem entrou, onde, com qual papel) —
// payload montado à mão com vocabulário fechado (agents/03/04).
func registrarSuporte(ctx context.Context, usuario, organizacao, workspace uuid.UUID, papel string) {
	slog.InfoContext(ctx, "[SUPORTE] acesso concedido sem atribuição direta",
		"dominio", "identidade", "acao", "suporte_concedido",
		"user_uuid", usuario.String(),
		"organization_uuid", organizacao.String(),
		"workspace_uuid", workspace.String(),
		"papel", papel)
}
