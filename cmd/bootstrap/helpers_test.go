package bootstrap

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	dominioUsuario "workspace-api/internal/identidade/domain/user"
	modeluser "workspace-api/internal/identidade/model/user"
	"workspace-api/internal/pkg/orgctx"
)

// criarSuperAdminRaw cria um usuário real na organization informada e lhe
// atribui o papel super_admin diretamente no banco, sem passar pelas regras
// de hierarquia do service. Útil em testes de integração que precisam de um
// operador com poder total para exercer AtribuirPapel/RemoverAtribuicao.
func criarSuperAdminRaw(t *testing.T, amb *ambienteBanco, organizationUUID uuid.UUID, email string) uuid.UUID {
	t.Helper()

	svc := dominioUsuario.NewService(
		dominioUsuario.NewRepository(amb.db),
		dominioUsuario.NewRepositorioAtribuicoes(amb.db),
		validadorSemprePertence{},
		dominioUsuario.NovasCredenciaisBcrypt(),
	)

	u, err := svc.Create(orgctx.WithOrganization(amb.ctx, organizationUUID), dominioUsuario.EntradaCriacao{
		Dados: modeluser.CreateInput{Nome: "Operador " + email, Email: email},
		Senha: "senha-segura-123",
	})
	require.NoError(t, err)

	var papelTexto string
	require.NoError(t, amb.db.Raw(
		`SELECT uuid::text FROM identidade_user_papel WHERE nome = ?`, papelSuperAdmin,
	).Scan(&papelTexto).Error)
	papelUUID := uuid.MustParse(papelTexto)

	wsUUID := uuid.New()
	require.NoError(t, amb.db.Exec(
		`INSERT INTO identidade_workspace_workspace (uuid, organization_uuid, nome, slug, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 'ativo', ?, ?)`,
		wsUUID, organizationUUID, "WS Operador", "op-"+uuid.NewString()[:8], time.Now(), time.Now(),
	).Error)

	require.NoError(t, amb.db.Exec(
		`INSERT INTO identidade_user_atribuicao (uuid, organization_uuid, workspace_uuid, user_uuid, papel_uuid, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		uuid.New(), organizationUUID, wsUUID, u.UUID, papelUUID, time.Now(), time.Now(),
	).Error)

	return u.UUID
}

// ctxComOperador retorna o contexto base enriquecido com o UUID do operador.
func ctxComOperador(ctx context.Context, operadorUUID uuid.UUID) context.Context {
	return orgctx.WithUser(ctx, operadorUUID)
}
