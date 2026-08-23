package bootstrap

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tcclickhouse "github.com/testcontainers/testcontainers-go/modules/clickhouse"

	aplicacaologs "workspace-api/internal/identidade/application/logs"
	"workspace-api/internal/infra/clickhouse"
	"workspace-api/internal/pkg/config"
	"workspace-api/internal/pkg/errobserve"
	"workspace-api/internal/pkg/log/access_log"
	"workspace-api/internal/pkg/log/audit_log"
	"workspace-api/internal/pkg/orgctx"
	"workspace-api/internal/pkg/pagination"
)

// --- Ambiente ClickHouse efêmero (mesmo padrão do pacote infra) -------------

type ambienteClickhouse struct {
	host string
	port int
	user string
	pass string
}

func subirClickhouse(t *testing.T) *ambienteClickhouse {
	t.Helper()
	if !dockerDisponivel(t) {
		t.Skip("docker indisponível — teste de integração pulado")
	}
	ctx := context.Background()
	container, err := tcclickhouse.Run(ctx, "clickhouse/clickhouse-server:24.3-alpine")
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	host, err := container.Host(ctx)
	require.NoError(t, err)
	porta, err := container.MappedPort(ctx, "9000")
	require.NoError(t, err)
	return &ambienteClickhouse{host: host, port: porta.Int(), user: container.User, pass: container.Password}
}

// aplicarDDLEfemero roda os arquivos de db/logs/ no contêiner — prova que o
// DDL versionado (incluindo o 0004 de tenancy do E5) conversa com o consultor
// de leitura. Protocolo nativo não aceita multi-statement: comando a comando.
func aplicarDDLEfemero(t *testing.T, ch *ambienteClickhouse) {
	t.Helper()
	conn := clickhouse.Connect(config.ClickHouseConfig{
		Enabled: true, Host: ch.host, Port: ch.port,
		User: ch.user, Pass: ch.pass, Database: "default",
	})
	require.NotNil(t, conn)
	for _, arquivo := range []string{"0001_log_acesso.sql", "0002_log_auditoria.sql", "0003_log_erros.sql", "0004_log_acesso_tenancy.sql"} {
		corpo, err := os.ReadFile(filepath.Join("..", "..", "db", "logs", arquivo))
		require.NoError(t, err, "DDL %s deve existir", arquivo)
		var semComentario []string
		for _, linha := range strings.Split(string(corpo), "\n") {
			if !strings.HasPrefix(strings.TrimSpace(linha), "--") {
				semComentario = append(semComentario, linha)
			}
		}
		for _, comando := range strings.Split(strings.Join(semComentario, "\n"), ";") {
			comando = strings.TrimSpace(comando)
			if comando == "" {
				continue
			}
			require.NoError(t, conn.Exec(context.Background(), comando), "aplicando %s", arquivo)
		}
	}
	require.NoError(t, conn.Close())
}

// configParaLogsLeitura grava configs.json com o ClickHouse efêmero LIGADO e
// janela de flush curta — a suíte não espera segundos por telemetria.
func configParaLogsLeitura(t *testing.T, ch *ambienteClickhouse) {
	t.Helper()
	exemplo := map[string]any{
		"app":      map[string]any{"name": "workspace-api", "env": "teste", "version": "0.0.0", "base_domain": "plataforma.teste"},
		"server":   map[string]any{"http": map[string]any{"port": 18080, "read_timeout_sec": 15, "write_timeout_sec": 30, "idle_timeout_sec": 60, "shutdown_timeout_sec": 10, "trusted_proxy": []string{}, "cors": map[string]any{"allowed_origins": []string{}}}},
		"security": map[string]any{"jwt_secret": "segredo-de-teste-da-leitura-de-logs-32b", "jwt_ttl_min": 60, "jwt_refresh_ttl_hours": 168},
		"databases": map[string]any{
			"postgres":   map[string]any{"host": "127.0.0.1", "port": 5432, "user": "x", "pass": "x", "name": "x", "ssl_mode": "disable"},
			"migrations": map[string]any{"path": "../../db/migrations", "auto_run": false, "lock_timeout_sec": 5, "statement_timeout_min": 10},
			"redis":      map[string]any{"enabled": false, "host": "localhost", "port": 6379, "pass": "", "db": 0},
			"clickhouse": map[string]any{"enabled": true, "host": ch.host, "port": ch.port, "user": ch.user, "pass": ch.pass, "database": "workspace_logs"},
		},
		"logs": map[string]any{"lote_tamanho": 100, "lote_janela_ms": 200, "fila_tamanho": 1000, "drain_timeout_sec": 5, "alerta_janela_seg": 60},
	}
	conteudo, err := json.Marshal(exemplo)
	require.NoError(t, err)
	caminho := filepath.Join(t.TempDir(), "configs.json")
	require.NoError(t, os.WriteFile(caminho, conteudo, 0o600))
	require.NoError(t, config.Init(caminho))
}

// --- Cenário ----------------------------------------------------------------

var (
	orgLogsB = uuid.MustParse("bbbbbbbb-0000-4000-8000-00000000bb02")
	wsA1Logs = uuid.MustParse("cccccccc-0000-4000-8000-00000000cca1")
	wsA2Logs = uuid.MustParse("cccccccc-0000-4000-8000-00000000cca2")
	userLogs = uuid.MustParse("dddddddd-0000-4000-8000-00000000dd02")
)

// TestLeituraDeLogsPontaAPonta: escrita nas trilhas → flush → leitura pelos
// serviços com o recorte imposto — plataforma vê tudo, organization fica na
// própria org e workspace no próprio recorte; degradado vira ErrIndisponivel.
//
// Usa CONSTRUTORES PUROS (NewService/consultorLogs), nunca os singletons das
// aplicações — sync.Once é do processo e outros testes deste pacote bootam
// seus próprios dublês.
func TestLeituraDeLogsPontaAPonta(t *testing.T) {
	if !dockerDisponivel(t) {
		t.Skip("docker indisponível — teste de integração pulado")
	}
	subirAmbiente(t) // postgres mantém o padrão do pacote; a leitura não o usa
	ch := subirClickhouse(t)
	aplicarDDLEfemero(t, ch)

	// Estado limpo do singleton do CH antes e DEPOIS — a ordem dos testes do
	// pacote não pode depender de quem bootou o once primeiro.
	clickhouse.ResetarParaTeste()
	defer func() {
		clickhouse.ResetarParaTeste()
		errobserve.DefinirSinks() // restaura o padrão slog-only
	}()

	configParaLogsLeitura(t, ch)
	trilhas, err := iniciarTrilhas()
	require.NoError(t, err)
	require.NotNil(t, trilhas.escritor, "clickhouse ligado: writer deve existir")

	const (
		rayA     = "ray-org-a"
		rayB     = "ray-org-b"
		userOrgA = "dddddddd-0000-4000-8000-00000000da01"
	)

	base := time.Now().UTC().Add(-time.Hour)
	auditar := func(org, ws, user, ray string, i int) audit_log.Evento {
		return audit_log.Evento{
			Instante: base.Add(time.Duration(i) * time.Second),
			Dominio:  "identidade", Subdominio: "workspace", Acao: "criar", Sucesso: true,
			OrganizationUUID: org, WorkspaceUUID: ws, UserUUID: user, RayTrace: ray,
			Detalhes: audit_log.Detalhes("ordem", i),
		}
	}
	acessar := func(org, ws, user, ray string) access_log.Evento {
		return access_log.Evento{
			Instante: base.Add(2 * time.Second), Metodo: "GET",
			Path: "/api/status", Rota: "/api/status", Status: 200, DuracaoMS: 3,
			RayTrace: ray, OrganizationUUID: org, WorkspaceUUID: ws, UserUUID: user,
		}
	}
	errar := func(org, ws, codigo string) errobserve.Evento {
		return errobserve.Evento{
			Instante: base.Add(3 * time.Second), Dominio: "identidade",
			Subdominio: "workspace", Codigo: codigo,
			Mensagem: "recusa esperada", Severidade: errobserve.SeveridadeWarn,
			OrganizationUUID: org, WorkspaceUUID: ws,
		}
	}

	trilhas.escritor.EnfileirarAuditoria(auditar(orgA.String(), wsA1Logs.String(), userOrgA, rayA, 1))
	trilhas.escritor.EnfileirarAuditoria(auditar(orgA.String(), wsA2Logs.String(), userOrgA, rayA+"-2", 2))
	trilhas.escritor.EnfileirarAuditoria(auditar(orgLogsB.String(), "", "", rayB, 3))
	trilhas.escritor.EnfileirarAcesso(acessar(orgA.String(), wsA1Logs.String(), userOrgA, rayA))
	trilhas.escritor.EnfileirarErros(errar(orgA.String(), wsA1Logs.String(), "identidade.workspace.slug_em_uso"))

	svc := aplicacaologs.NewService(consultorLogs{})
	ctxPlataforma := orgctx.WithPermissoes(
		orgctx.WithUser(context.Background(), userLogs),
		[]string{"*:*"})
	ctxAdminOrg := orgctx.WithPermissoes(
		orgctx.WithWorkspace(
			orgctx.WithOrganization(
				orgctx.WithUser(context.Background(), userLogs), orgA), wsA1Logs),
		[]string{aplicacaologs.PermLer, aplicacaologs.PermLerOrganization})
	ctxUsuarioWs := orgctx.WithPermissoes(
		orgctx.WithWorkspace(
			orgctx.WithOrganization(
				orgctx.WithUser(context.Background(), userLogs), orgA), wsA1Logs),
		[]string{aplicacaologs.PermLer})

	pagina := pagination.Pagination{Page: 1, PageSize: 10}

	// Espera o flush da janela (config com 200ms) gravar tudo.
	var totalAuditoriaGeral int64
	require.Eventually(t, func() bool {
		_, totalAuditoriaGeral, err = consultorLogs{}.Auditoria(
			context.Background(), clickhouse.FiltroTrilha{Limite: 100})
		return err == nil && totalAuditoriaGeral == 3
	}, 10*time.Second, 50*time.Millisecond, "as 3 auditorias deveriam estar consultáveis após o flush")

	// PLATAFORMA: vê as três organizations, filtro opcional honrado.
	resp, err := svc.Auditoria(ctxPlataforma, clickhouse.FiltroTrilha{}, pagina)
	require.NoError(t, err)
	assert.EqualValues(t, 3, resp.Total)
	resp, err = svc.Auditoria(ctxPlataforma, clickhouse.FiltroTrilha{OrganizationUUID: orgLogsB.String()}, pagina)
	require.NoError(t, err)
	assert.EqualValues(t, 1, resp.Total, "plataforma filtra por organization alheia")

	// ORGANIZATION: presa à própria org — vê os DOIS workspaces dela, nunca a alheia.
	resp, err = svc.Auditoria(ctxAdminOrg, clickhouse.FiltroTrilha{}, pagina)
	require.NoError(t, err)
	assert.EqualValues(t, 2, resp.Total, "admin_organization lê todos os workspaces da própria")
	respFiltrada, err := svc.Auditoria(ctxAdminOrg, clickhouse.FiltroTrilha{WorkspaceUUID: wsA2Logs.String()}, pagina)
	require.NoError(t, err)
	assert.EqualValues(t, 1, respFiltrada.Total, "filtro de workspace DENTRO da org é honrado")
	assert.Equal(t, wsA2Logs.String(), respFiltrada.Items[0].WorkspaceUUID)
	_, err = svc.Auditoria(ctxAdminOrg, clickhouse.FiltroTrilha{OrganizationUUID: orgLogsB.String()}, pagina)
	require.ErrorIs(t, err, aplicacaologs.ErrForaDoEscopo, "organization_uuid alheio = 404")

	// WORKSPACE: preso ao par (org, workspace) resolvido.
	resp, err = svc.Auditoria(ctxUsuarioWs, clickhouse.FiltroTrilha{}, pagina)
	require.NoError(t, err)
	assert.EqualValues(t, 1, resp.Total, "usuário de workspace vê só o próprio recorte")
	_, err = svc.Auditoria(ctxUsuarioWs, clickhouse.FiltroTrilha{WorkspaceUUID: wsA2Logs.String()}, pagina)
	require.ErrorIs(t, err, aplicacaologs.ErrForaDoEscopo, "irmão da mesma org é fora do recorte")

	// Trilha de acesso: tenancy gravada pelo E5 é recortável.
	acessos, err := svc.Acesso(ctxAdminOrg, clickhouse.FiltroTrilha{}, pagina)
	require.NoError(t, err)
	require.EqualValues(t, 1, acessos.Total)
	require.Len(t, acessos.Items, 1)
	assert.Equal(t, orgA.String(), acessos.Items[0].OrganizationUUID)
	assert.Equal(t, rayA, acessos.Items[0].RayTrace)

	// Trilha de erros: filtro por código estável dentro do recorte.
	errosLidos, err := svc.Erros(ctxAdminOrg, clickhouse.FiltroTrilha{Acao: "identidade.workspace.slug_em_uso"}, pagina)
	require.NoError(t, err)
	require.EqualValues(t, 1, errosLidos.Total)
	require.Len(t, errosLidos.Items, 1)
	assert.Equal(t, "identidade.workspace.slug_em_uso", errosLidos.Items[0].Codigo)

	// Filtros complementares: ray_trace (correspondência EXATA) e janela futura vazia.
	_, totalRay, err := consultorLogs{}.Auditoria(context.Background(),
		clickhouse.FiltroTrilha{RayTrace: rayA, Limite: 100})
	require.NoError(t, err)
	assert.EqualValues(t, 1, totalRay, "ray_trace é filtro exato — rayA não casa com rayA-2")
	futuro := time.Now().UTC().Add(30 * time.Minute)
	_, totalJanelaVazia, err := consultorLogs{}.Auditoria(context.Background(),
		clickhouse.FiltroTrilha{InstanteInicio: futuro, Limite: 100})
	require.NoError(t, err)
	assert.Zero(t, totalJanelaVazia, "janela futura devolve vazio com total zero")

	// DEGRADAÇÃO: sem ClickHouse a consulta falha fechado com a sentinela do
	// 503 padronizado — nunca lista vazia silenciosa.
	clickhouse.Close()
	_, _, err = consultorLogs{}.Auditoria(context.Background(), clickhouse.FiltroTrilha{Limite: 10})
	require.ErrorIs(t, err, aplicacaologs.ErrIndisponivel)
	_, err = svc.Erros(ctxAdminOrg, clickhouse.FiltroTrilha{}, pagina)
	require.ErrorIs(t, err, aplicacaologs.ErrIndisponivel)
}

// TestSeedCobrePermissoesDeLogs: o seed declara as permissões novas nos
// papéis certos — divergência seed × constantes é bug de contrato.
func TestSeedCobrePermissoesDeLogs(t *testing.T) {
	esperadas := map[string][]string{
		papelSuperAdmin:        {"*:*"},
		papelAdminOrganization: {"identidade:logs:ler", "identidade:logs:ler_organization"},
		papelAdminWorkspace:    {"identidade:logs:ler"},
		papelSomenteLeitura:    {"identidade:logs:ler"},
		// usuario_workspace não recebe permissão de logs (auditoria é função
		// de administração) — ausência na mapa é a asserção.
	}
	encontradas := map[string][]string{}
	for _, papel := range papeisSeed {
		for _, permissao := range papel.permissoes {
			switch permissao {
			case "*:*", "identidade:logs:ler", "identidade:logs:ler_organization":
				encontradas[papel.nome] = append(encontradas[papel.nome], permissao)
			}
		}
	}
	require.Equal(t, esperadas, encontradas)
}
