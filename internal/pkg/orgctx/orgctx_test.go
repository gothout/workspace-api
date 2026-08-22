package orgctx

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var (
	orgID = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	wsID  = uuid.MustParse("00000000-0000-0000-0000-000000000002")
	usrID = uuid.MustParse("00000000-0000-0000-0000-000000000003")
)

// bancoDryRun abre um handle gorm sem conexão real — só monta SQL.
func bancoDryRun(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=orgctx-teste"}), &gorm.Config{
		DisableAutomaticPing: true,
	})
	require.NoError(t, err)
	return db
}

// registroTeste dá uma tabela ao gorm montar o SQL no modo DryRun.
type registroTeste struct {
	OrganizationUUID uuid.UUID
	WorkspaceUUID    uuid.UUID
}

func (registroTeste) TableName() string { return "tabela_teste" }

func contextoCompleto() context.Context {
	ctx := context.Background()
	ctx = WithOrganization(ctx, orgID)
	ctx = WithWorkspace(ctx, wsID)
	ctx = WithUser(ctx, usrID)
	ctx = WithRayTrace(ctx, "ray-abc")
	ctx = WithPermissoes(ctx, []string{"identidade:workspace:ler"})
	return ctx
}

func TestGettersDevolvemValoresInjetados(t *testing.T) {
	ctx := contextoCompleto()
	assert.Equal(t, orgID, OrganizationUUID(ctx))
	assert.Equal(t, wsID, WorkspaceUUID(ctx))
	assert.Equal(t, usrID, UserUUID(ctx))
	assert.Equal(t, "ray-abc", RayTrace(ctx))
	assert.True(t, TemPermissao(ctx, "identidade:workspace:ler"))
}

func TestContextoVazioDevolveZeroValues(t *testing.T) {
	ctx := context.Background()
	assert.Equal(t, uuid.Nil, OrganizationUUID(ctx))
	assert.Equal(t, uuid.Nil, WorkspaceUUID(ctx))
	assert.False(t, TemPermissao(ctx, "identidade:workspace:ler"))
}

func TestScopeSemEscopoFalhaAoExecutar(t *testing.T) {
	db := bancoDryRun(t).Session(&gorm.Session{DryRun: true})

	q := Scope(db, context.Background()).Find(&[]registroTeste{})
	require.Error(t, q.Error)
	assert.ErrorIs(t, q.Error, ErrEscopoAusente)
}

func TestScopeOrganizationSemOrganizationFalha(t *testing.T) {
	db := bancoDryRun(t).Session(&gorm.Session{DryRun: true})
	ctx := WithWorkspace(context.Background(), wsID) // workspace sem organization

	q := ScopeOrganization(db, ctx).Find(&[]registroTeste{})
	require.Error(t, q.Error)
	assert.True(t, errors.Is(q.Error, ErrEscopoAusente))
}

func TestScopeGeraFiltroComExatamenteAsColunasDeEscopo(t *testing.T) {
	db := bancoDryRun(t).Session(&gorm.Session{DryRun: true})

	q := Scope(db, contextoCompleto()).Find(&[]registroTeste{})
	require.NoError(t, q.Error)
	sql := q.Statement.SQL.String()
	assert.Contains(t, sql, "organization_uuid")
	assert.Contains(t, sql, "workspace_uuid")
	assert.Len(t, q.Statement.Vars, 2)
	assert.Equal(t, orgID, q.Statement.Vars[0])
	assert.Equal(t, wsID, q.Statement.Vars[1])
}

func TestScopeOrganizationGeraFiltroSoComOrganization(t *testing.T) {
	db := bancoDryRun(t).Session(&gorm.Session{DryRun: true})
	ctx := WithOrganization(context.Background(), orgID)

	q := ScopeOrganization(db, ctx).Find(&[]registroTeste{})
	require.NoError(t, q.Error)
	sql := q.Statement.SQL.String()
	assert.Contains(t, sql, "organization_uuid")
	assert.NotContains(t, sql, "workspace_uuid")
}
