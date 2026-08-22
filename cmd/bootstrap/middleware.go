// Ligação da cadeia de middleware com o banco: adaptadores que implementam os
// contratos de internal/middleware consultando as tabelas de autorização DIRETAMENTE.
//
// PROVISÓRIO DOCUMENTADO (issue #2 / agents/03): na F1 não há subdomínio para
// delegar — a consulta vai direta ao SQL canônico. Na F3 (workspace) e na F4
// (user) estes adaptadores passam a resolver o singleton do subdomínio NA
// CHAMADA; os contratos do middleware NÃO mudam. O domínio custom white-label
// (ProvedorDominiosCustom) e a validação de X-Api-Key (ResolvedorApiKeys)
// entram na F2 junto da tabela da organization — até lá ficam DESLIGADOS:
// resolução cobre só o domínio-base e toda X-Api-Key falha 401 (fail-closed).
package bootstrap

import (
	"context"
	"errors"
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
		JWT:        gerenciador,
		Workspaces: resolvedorWorkspaces{},
		Permissoes: resolvedorPermissoes{},
	})
}

// --- Contrato ResolvedorWorkspaces ------------------------------------------

// resolvedorWorkspaces resolve workspace por slug (Host) ou uuid
// (X-Workspace-Id) na tabela canônica do subdomínio workspace. A busca é a
// EXCEÇÃO global documentada (agents/03): acontece antes de existir escopo;
// o resultado nunca vaza para rotas de administração.
type resolvedorWorkspaces struct{}

// linhaWorkspace é a projeção mínima da tabela identidade_workspace_workspace.
type linhaWorkspace struct {
	UUID             uuid.UUID
	OrganizationUUID uuid.UUID
	Slug             string
	Status           string
}

func (resolvedorWorkspaces) BuscarPorSlug(ctx context.Context, slug string) (*middleware.WorkspaceResolvido, error) {
	db, err := postgres.GetDB()
	if err != nil {
		return nil, err
	}
	return buscarWorkspacePorSlug(ctx, db, slug)
}

func (resolvedorWorkspaces) BuscarPorUUID(ctx context.Context, id uuid.UUID) (*middleware.WorkspaceResolvido, error) {
	db, err := postgres.GetDB()
	if err != nil {
		return nil, err
	}
	return buscarWorkspacePorUUID(ctx, db, id)
}

// Fixos devolve os rótulos de endereço fixo da plataforma (doc 03, regra 8) —
// Host com um deles NUNCA resolve workspace. Quando o subdomínio workspace
// nascer (F3), esta lista passa a vir dele; os valores são os mesmos.
func (resolvedorWorkspaces) Fixos() []string {
	return []string{"www", "api", "app", "admin", "docs", "status", "mail", "suporte", "painel"}
}

// buscarWorkspacePorSlug é a FUNÇÃO PURA por trás do adaptador — os testes
// a exercem direto com o banco efêmero, nunca pelo singleton do processo.
func buscarWorkspacePorSlug(ctx context.Context, db *gorm.DB, slug string) (*middleware.WorkspaceResolvido, error) {
	var linha linhaWorkspace
	err := db.WithContext(ctx).
		Table("identidade_workspace_workspace").
		Select("uuid, organization_uuid, slug, status").
		Where("slug = ?", slug).
		First(&linha).Error
	return paraResolvido(&linha), traduzirNaoEncontrado(err)
}

func buscarWorkspacePorUUID(ctx context.Context, db *gorm.DB, id uuid.UUID) (*middleware.WorkspaceResolvido, error) {
	var linha linhaWorkspace
	err := db.WithContext(ctx).
		Table("identidade_workspace_workspace").
		Select("uuid, organization_uuid, slug, status").
		Where("uuid = ?", id).
		First(&linha).Error
	return paraResolvido(&linha), traduzirNaoEncontrado(err)
}

func paraResolvido(linha *linhaWorkspace) *middleware.WorkspaceResolvido {
	if linha == nil || linha.UUID == uuid.Nil {
		return nil
	}
	return &middleware.WorkspaceResolvido{
		UUID:             linha.UUID,
		OrganizationUUID: linha.OrganizationUUID,
		Slug:             linha.Slug,
		Ativo:            linha.Status == "ativo",
	}
}

func traduzirNaoEncontrado(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return middleware.ErrNaoEncontrado
	}
	return err
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

// --- Provedor de domínios custom para o CORS --------------------------------

// dominiosCustomParaCors expõe ao engine os domínios white-label registrados
// pelas organizations. PROVISÓRIO: consulta identidade_organization_organization
// direto; na F2 passa a delegar ao subdomínio organization — o contrato do
// CORS não muda. Enquanto a tabela não existe, origem fora do domínio-base é
// recusada com log de erro (fail-closed), nunca aceita.
func dominiosCustomParaCors(ctx context.Context) ([]string, error) {
	db, err := postgres.GetDB()
	if err != nil {
		return nil, err
	}
	var dominios []string
	err = db.WithContext(ctx).
		Table("identidade_organization_organization").
		Where("dominio IS NOT NULL AND dominio <> '' AND status = 'ativo'").
		Distinct().
		Order("dominio").
		Pluck("dominio", &dominios).Error
	if err != nil && errors.Is(err, gorm.ErrRecordNotFound) {
		return []string{}, nil
	}
	return dominios, err
}
