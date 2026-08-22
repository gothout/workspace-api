package bootstrap

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dominioOrganizacao "workspace-api/internal/identidade/domain/organization"
	dominioWorkspace "workspace-api/internal/identidade/domain/workspace"
	orgmodel "workspace-api/internal/identidade/model/organization"
	modelworkspace "workspace-api/internal/identidade/model/workspace"
	"workspace-api/internal/middleware"
	"workspace-api/internal/pkg/config"
	"workspace-api/internal/pkg/orgctx"
	"workspace-api/internal/pkg/pagination"
)

// configParaOrganization grava um configs.json com base_domain próprio e roda
// o config.Init real — o serviço lê o base_domain por ele para validar o VO.
func configParaOrganization(t *testing.T) {
	t.Helper()
	exemplo := map[string]any{
		"app":      map[string]any{"name": "workspace-api", "env": "teste", "version": "0.0.0", "base_domain": "plataforma.teste"},
		"server":   map[string]any{"http": map[string]any{"port": 18080, "read_timeout_sec": 15, "write_timeout_sec": 30, "idle_timeout_sec": 60, "shutdown_timeout_sec": 10, "trusted_proxy": []string{}, "cors": map[string]any{"allowed_origins": []string{}}}},
		"security": map[string]any{"jwt_secret": "segredo-de-teste-da-organization", "jwt_ttl_min": 60, "jwt_refresh_ttl_hours": 168},
		"databases": map[string]any{
			"postgres":   map[string]any{"host": "127.0.0.1", "port": 5432, "user": "x", "pass": "x", "name": "x", "ssl_mode": "disable"},
			"migrations": map[string]any{"path": "../../db/migrations", "auto_run": false, "lock_timeout_sec": 5, "statement_timeout_min": 10},
		},
	}
	conteudo, err := json.Marshal(exemplo)
	require.NoError(t, err)
	caminho := filepath.Join(t.TempDir(), "configs.json")
	require.NoError(t, os.WriteFile(caminho, conteudo, 0o600))
	require.NoError(t, config.Init(caminho))
}

// Seed da organization raiz: uuid determinístico, idempotente.
func TestSeedOrganizacaoRaizIdempotente(t *testing.T) {
	if !dockerDisponivel(t) {
		t.Skip("docker indisponível — teste de integração pulado")
	}
	amb := subirAmbiente(t)

	require.NoError(t, semearOrganizacaoRaiz(amb.ctx, amb.db))
	require.NoError(t, semearOrganizacaoRaiz(amb.ctx, amb.db), "segunda execução não falha")

	var total int64
	require.NoError(t, amb.db.Table("identidade_organization_organization").Count(&total).Error)
	assert.EqualValues(t, 1, total, "organization raiz existe exatamente uma vez")

	var linha struct {
		UUID   uuid.UUID
		Nome   string
		Status string
	}
	require.NoError(t, amb.db.Raw(`SELECT uuid::text AS uuid, nome, status FROM identidade_organization_organization`).
		Scan(&linha).Error)
	assert.Equal(t, "Plataforma", linha.Nome)
	assert.Equal(t, "ativo", linha.Status)
}

// Ponta a ponta sobre o esquema migrado: CRUD de organization, unicidade
// TOTAL do domínio custom, chaves de API, adaptadores do middleware (provedor
// de domínios custom + resolvedor X-Api-Key), subdomínio workspace (slug
// global, reservados, resolução por Host) e a cascata REAL organization →
// workspace.
//
// O singleton é POR PROCESSO (sync.Once): este é o ÚNICO teste do pacote que
// boota os contêineres — amarrados ao banco efêmero desta suíte; os demais
// usam funções puras direto.
func TestSubdominioOrganizationPontaAPonta(t *testing.T) {
	if !dockerDisponivel(t) {
		t.Skip("docker indisponível — teste de integração pulado")
	}
	amb := subirAmbiente(t)
	configParaOrganization(t)
	require.NoError(t, semearOrganizacaoRaiz(amb.ctx, amb.db))

	_, err := dominioWorkspace.New(amb.db, nil)
	require.NoError(t, err)
	ctrl, err := dominioOrganizacao.New(amb.db, suspendedorWorkspaces{})
	require.NoError(t, err)
	require.NotNil(t, ctrl)

	svc := dominioOrganizacao.MustUse().Service
	ctxFundo := context.Background()

	// --- Ciclo de vida básico -------------------------------------------------
	orgA, err := svc.Create(ctxFundo, orgmodel.CreateInput{Nome: "Parceiro A"})
	require.NoError(t, err)
	ctxA := orgctx.WithOrganization(ctxFundo, orgA.UUID)

	vista, err := svc.Read(ctxA, orgA.UUID)
	require.NoError(t, err)
	assert.Equal(t, orgA.Nome, vista.Nome)

	// Leitura sem escopo / de organization alheia NÃO vaza existência.
	_, err = svc.Read(ctxFundo, orgA.UUID)
	assert.ErrorIs(t, err, dominioOrganizacao.ErrNotFound)
	_, err = svc.Read(orgctx.WithOrganization(ctxFundo, uuid.New()), orgA.UUID)
	assert.ErrorIs(t, err, dominioOrganizacao.ErrNotFound)

	// --- Domínio custom white-label --------------------------------------------
	_, err = svc.DefinirDominio(ctxA, orgA.UUID, "Parceiro.COM")
	require.NoError(t, err)

	registrados, err := svc.ListarDominiosAtivos(ctxFundo)
	require.NoError(t, err)
	require.Len(t, registrados, 1)
	assert.Equal(t, "parceiro.com", registrados[0].Valor)
	assert.Equal(t, orgA.UUID, registrados[0].OrganizationUUID)

	listados, err := (provedorDominiosCustom{}).Listar(ctxFundo)
	require.NoError(t, err)
	require.Len(t, listados, 1, "provedor do middleware enxerga o domínio ativo")
	assert.Equal(t, orgA.UUID, listados[0].OrganizationUUID)

	recusados := []string{
		"plataforma.teste",     // igual ao base_domain
		"sub.plataforma.teste", // descendente do base_domain
		"co.uk",                // public suffix
		"localhost",            // rótulo único
	}
	for _, dominio := range recusados {
		_, err := svc.DefinirDominio(ctxA, orgA.UUID, dominio)
		assert.ErrorIs(t, err, orgmodel.ErrDominioInvalido, "domínio %q deveria ser recusado pelo VO", dominio)
	}

	// Unicidade GLOBAL: outra organization não registra o mesmo domínio.
	orgB, err := svc.Create(ctxFundo, orgmodel.CreateInput{Nome: "Concorrente B"})
	require.NoError(t, err)
	ctxB := orgctx.WithOrganization(ctxFundo, orgB.UUID)
	_, err = svc.DefinirDominio(ctxB, orgB.UUID, "parceiro.com")
	assert.ErrorIs(t, err, dominioOrganizacao.ErrDominioEmUso, "conflito SQLSTATE 23505 traduzido")

	// --- Chaves de API -----------------------------------------------------------
	chaveCriada, chaveClara, err := svc.CriarApiKey(ctxA, orgA.UUID, dominioOrganizacao.ApiKeyEntrada{
		Nome:               "integração",
		EscopoOrganization: true,
		Permissoes:         []string{"identidade:workspace:ler"},
	})
	require.NoError(t, err)
	assert.NotEqual(t, chaveClara, chaveCriada.KeyHash)

	resolvida, err := (resolvedorApiKeys{}).BuscarPorChave(ctxFundo, chaveClara)
	require.NoError(t, err)
	assert.Equal(t, orgA.UUID, resolvida.OrganizationUUID)
	assert.True(t, resolvida.EscopoOrganization)
	assert.ElementsMatch(t, []string{"identidade:workspace:ler"}, resolvida.Permissoes)

	_, err = (resolvedorApiKeys{}).BuscarPorChave(ctxFundo, "wka_"+uuid.NewString())
	assert.ErrorIs(t, err, middleware.ErrNaoEncontrado, "chave desconhecida falha fechada")

	expira := time.Now().UTC().Add(-time.Hour)
	_, chaveExpiradaClara, err := svc.CriarApiKey(ctxA, orgA.UUID, dominioOrganizacao.ApiKeyEntrada{
		Nome:               "antiga",
		EscopoOrganization: true,
		Permissoes:         []string{"*:*"},
		ExpiresAt:          &expira,
	})
	require.NoError(t, err)
	_, err = (resolvedorApiKeys{}).BuscarPorChave(ctxFundo, chaveExpiradaClara)
	assert.ErrorIs(t, err, middleware.ErrNaoEncontrado, "chave expirada não se distingue de inexistente")

	require.NoError(t, svc.RevogarApiKey(ctxA, orgA.UUID, chaveCriada.UUID))
	_, err = (resolvedorApiKeys{}).BuscarPorChave(ctxFundo, chaveClara)
	assert.ErrorIs(t, err, middleware.ErrNaoEncontrado, "chave revogada para de validar imediatamente")

	// --- Remoção com índice único TOTAL no domínio ---------------------------------
	require.NoError(t, svc.Delete(ctxA, orgA.UUID))

	registrados, err = svc.ListarDominiosAtivos(ctxFundo)
	require.NoError(t, err)
	assert.Empty(t, registrados, "remover/inativar desativa a resolução imediatamente")

	orgC, err := svc.Create(ctxFundo, orgmodel.CreateInput{Nome: "Oportunista C"})
	require.NoError(t, err)
	ctxC := orgctx.WithOrganization(ctxFundo, orgC.UUID)
	_, err = svc.DefinirDominio(ctxC, orgC.UUID, "parceiro.com")
	assert.ErrorIs(t, err, dominioOrganizacao.ErrDominioEmUso,
		"índice único TOTAL: domínio removido NÃO se libera (anti-takeover)")

	// --- Subdomínio workspace (F3) sobre o mesmo banco ------------------------------
	// Slug único GLOBAL, reservados vindos DO SUBDOMÍNIO e resolução pelos
	// adaptadores do middleware.
	svcWs := dominioWorkspace.MustUse().Service
	wsB1, err := svcWs.Create(ctxB, modelworkspace.CreateInput{Nome: "Filial Sul", Slug: "filial-sul"})
	require.NoError(t, err)
	assert.Equal(t, orgB.UUID, wsB1.OrganizationUUID, "escopo vem do ctx, nunca do corpo")

	reservados := []string{"www", "api", "app", "admin", "docs", "status", "mail", "suporte", "painel"}
	for _, fixo := range reservados {
		_, err := svcWs.Create(ctxB, modelworkspace.CreateInput{Nome: "Fixo " + fixo, Slug: fixo})
		assert.ErrorIs(t, err, dominioWorkspace.ErrSlugReservado, "reservado %q nunca é criável", fixo)
	}
	assert.ElementsMatch(t, reservados, (resolvedorWorkspaces{}).Fixos(),
		"lista de rótulos fixos vem do SUBDOMÍNIO, não do adaptador")

	_, err = svcWs.Create(ctxC, modelworkspace.CreateInput{Nome: "Concorrente", Slug: "filial-sul"})
	assert.ErrorIs(t, err, dominioWorkspace.ErrSlugEmUso,
		"outra organization NÃO cria o mesmo slug — unicidade é global")

	peloHost, err := (resolvedorWorkspaces{}).BuscarPorSlug(ctxFundo, "filial-sul")
	require.NoError(t, err)
	require.NotNil(t, peloHost)
	assert.True(t, peloHost.Ativo)
	assert.Equal(t, orgB.UUID, peloHost.OrganizationUUID)

	peloHeader, err := (resolvedorWorkspaces{}).BuscarPorUUID(ctxFundo, wsB1.UUID)
	require.NoError(t, err)
	require.NotNil(t, peloHeader)
	assert.Equal(t, "filial-sul", peloHeader.Slug)

	_, err = (resolvedorWorkspaces{}).BuscarPorSlug(ctxFundo, "slug-fantasma")
	assert.ErrorIs(t, err, middleware.ErrNaoEncontrado, "inexistente falha fechada")

	// Leitura de administração por organization alheia não vaza existência.
	_, err = svcWs.Read(ctxC, wsB1.UUID)
	assert.ErrorIs(t, err, dominioWorkspace.ErrNotFound)

	// --- Inativação com cascata REAL (desde a F3 o lado workspace está ligado) ---
	inativo := orgmodel.StatusInativo
	_, err = svc.Update(ctxB, orgB.UUID, orgmodel.UpdateInput{Status: &inativo})
	require.NoError(t, err, "cascata suspende os workspaces da organization")
	_, err = svc.Update(ctxB, orgB.UUID, orgmodel.UpdateInput{Status: &inativo})
	assert.ErrorIs(t, err, orgmodel.ErrJaInativo)

	peloHost, err = (resolvedorWorkspaces{}).BuscarPorSlug(ctxFundo, "filial-sul")
	require.NoError(t, err)
	require.NotNil(t, peloHost)
	assert.False(t, peloHost.Ativo, "cascata suspendeu o workspace → resolução devolve Ativo=false (404)")

	_, err = svc.Reativar(ctxB, orgB.UUID)
	require.NoError(t, err)
	_, err = svc.Reativar(ctxB, orgB.UUID)
	assert.ErrorIs(t, err, orgmodel.ErrJaAtivo)

	removidos, _, err := svc.ListarApiKeys(ctxC, orgC.UUID, pagination.Pagination{})
	require.NoError(t, err)
	assert.Empty(t, removidos)
}
