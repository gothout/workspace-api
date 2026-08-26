package bootstrap

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dominioUsuario "workspace-api/internal/identidade/domain/user"
	aplicacaologs "workspace-api/internal/identidade/application/logs"
	modeluser "workspace-api/internal/identidade/model/user"
	"workspace-api/internal/infra/clickhouse"
	"workspace-api/internal/pkg/errobserve"
	"workspace-api/internal/pkg/log/access_log"
	"workspace-api/internal/pkg/log/audit_log"
	"workspace-api/internal/pkg/orgctx"
	"workspace-api/internal/pkg/pagination"
)

// --- Cenário UX2 (issue #29): enriquecimento user_nome/user_email ------------

// trilhasComUsuarios devolve linhas de auditoria referenciando usuários reais
// e uma fantasma (uuid sem linha) — dublê mínimo do contrato ConsultaTrilhas.
type trilhasComUsuarios struct {
	linhas []audit_log.Evento
}

func (f trilhasComUsuarios) Auditoria(context.Context, clickhouse.FiltroTrilha) ([]audit_log.Evento, int64, error) {
	return f.linhas, int64(len(f.linhas)), nil
}

func (trilhasComUsuarios) Acesso(context.Context, clickhouse.FiltroTrilha) ([]access_log.Evento, int64, error) {
	return nil, 0, nil
}

func (trilhasComUsuarios) Erros(context.Context, clickhouse.FiltroTrilha) ([]errobserve.Evento, int64, error) {
	return nil, 0, nil
}

// TestEnriquecimentoDeUsuariosPontaAPonta: o resolvedor ligado ao REPOSITÓRIO
// PURO do user sobre o postgres efêmero devolve nome/e-mail das linhas já
// recortadas — uuid fantasma fica vazio; usuário de OUTRA organization não é
// resolvido quando o ctx carrega escopo, mas a plataforma (ctx sem org)
// resolve qualquer um. Prova o join em duas bases: trilha (dublê) + Postgres.
func TestEnriquecimentoDeUsuariosPontaAPonta(t *testing.T) {
	if !dockerDisponivel(t) {
		t.Skip("docker indisponível — teste de integração pulado")
	}
	amb := subirAmbiente(t)
	semearOrganization(t, amb, orgA)
	semearOrganization(t, amb, orgForasteira)

	svcUsuario := dominioUsuario.NewService(
		dominioUsuario.NewRepository(amb.db),
		dominioUsuario.NewRepositorioAtribuicoes(amb.db),
		validadorSemprePertence{}, dominioUsuario.NovasCredenciaisBcrypt())

	criar := func(org uuid.UUID, nome, email string) modeluser.User {
		u, err := svcUsuario.Create(orgctx.WithOrganization(amb.ctx, org), dominioUsuario.EntradaCriacao{
			Dados: modeluser.CreateInput{Nome: nome, Email: email},
			Senha: "senha-segura-123",
		})
		require.NoError(t, err)
		return *u
	}
	ana := criar(orgA, "Ana Lima", "ana@orga.com")
	bruno := criar(orgA, "Bruno Reis", "bruno@orga.com")
	alheia := criar(orgForasteira, "Carla Alheia", "carla@fora.com")

	resolvedor := resolvedorUsuariosLogs{
		repositorio: func() dominioUsuario.Repository { return dominioUsuario.NewRepository(amb.db) },
	}

	// ESCOPO: ctx da organization NÃO resolve usuário de organization alheia;
	// ctx sem organization (caminho da plataforma) resolve qualquer um.
	ctxOrg := orgctx.WithOrganization(amb.ctx, orgA)
	resolvidos, err := resolvedor.Resolver(ctxOrg, []string{
		ana.UUID.String(), alheia.UUID.String(), "nao-e-uuid",
	})
	require.NoError(t, err)
	require.Len(t, resolvidos, 1, "alheio e lixo ficam fora do mapa escopado")
	assert.Equal(t, "Ana Lima", resolvidos[ana.UUID.String()].Nome)
	assert.Equal(t, "ana@orga.com", resolvidos[ana.UUID.String()].Email)

	resolvidosPlataforma, err := resolvedor.Resolver(amb.ctx, []string{
		ana.UUID.String(), bruno.UUID.String(), alheia.UUID.String(),
	})
	require.NoError(t, err)
	require.Len(t, resolvidosPlataforma, 3, "plataforma atravessa organizations")
	assert.Equal(t, "Carla Alheia", resolvidosPlataforma[alheia.UUID.String()].Nome)

	// PONTA A PONTA pela aplicação (recorte de workspace no ctx, como na
	// cadeia HTTP): linhas com uuids reais + fantasma saem enriquecidas;
	// fantasma sai com os campos vazios.
	fantasma := uuid.MustParse("dddddddd-0000-4000-8000-00000000dd09")
	wsCenario := uuid.MustParse("cccccccc-0000-4000-8000-00000000cca1")
	ctxWorkspace := orgctx.WithPermissoes(
		orgctx.WithWorkspace(
			orgctx.WithOrganization(amb.ctx, orgA), wsCenario),
		[]string{aplicacaologs.PermLer})
	svcLogs := aplicacaologs.NewService(
		trilhasComUsuarios{linhas: []audit_log.Evento{
			{Dominio: "identidade", Subdominio: "user", Acao: "criar", UserUUID: ana.UUID.String()},
			{Dominio: "identidade", Subdominio: "user", Acao: "editar", UserUUID: fantasma.String()},
		}},
		aplicacaologs.ComResolvedorUsuarios(resolvedor))
	resp, err := svcLogs.Auditoria(ctxWorkspace, clickhouse.FiltroTrilha{}, pagination.Pagination{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Len(t, resp.Items, 2)
	assert.Equal(t, "Ana Lima", resp.Items[0].UserNome)
	assert.Equal(t, "ana@orga.com", resp.Items[0].UserEmail)
	assert.Equal(t, fantasma.String(), resp.Items[1].UserUUID)
	assert.Empty(t, resp.Items[1].UserNome)
	assert.Empty(t, resp.Items[1].UserEmail)
}
