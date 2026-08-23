package clickhouse

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/stretchr/testify/require"
	tcclickhouse "github.com/testcontainers/testcontainers-go/modules/clickhouse"

	"workspace-api/internal/pkg/config"
	"workspace-api/internal/pkg/errobserve"
	"workspace-api/internal/pkg/log/audit_log"
)

// dockerDisponivel confere o daemon antes de tentar conexão real — sem
// ambiente o teste pula, nunca reprova (mesmo padrão dos pacotes postgres e
// redis).
func dockerDisponivel(t *testing.T) bool {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "docker", "info", "--format", "{{.ServerVersion}}").Run() == nil
}

// aplicarDDL roda os arquivos de db/logs/ no contêiner efêmero — prova que o
// DDL versionado e o gravador conversam (colunas na mesma ordem/tipo). O
// protocolo nativo não aceita multi-statement: cada comando vai separado.
func aplicarDDL(t *testing.T, conn driver.Conn) {
	t.Helper()
	for _, arquivo := range []string{"0001_log_acesso.sql", "0002_log_auditoria.sql", "0003_log_erros.sql", "0004_log_acesso_tenancy.sql"} {
		corpo, err := os.ReadFile(filepath.Join("..", "..", "..", "db", "logs", arquivo))
		require.NoError(t, err, "DDL %s deve existir", arquivo)
		// Comentários primeiro (podem conter ';'), depois split de comandos.
		var linhasCodigo []string
		for _, linha := range strings.Split(string(corpo), "\n") {
			if !strings.HasPrefix(strings.TrimSpace(linha), "--") {
				linhasCodigo = append(linhasCodigo, linha)
			}
		}
		for _, comando := range strings.Split(strings.Join(linhasCodigo, "\n"), ";") {
			comando = strings.TrimSpace(comando)
			if comando == "" {
				continue
			}
			require.NoError(t, conn.Exec(context.Background(), comando),
				"aplicando %s", arquivo)
		}
	}
}

// TestIntegracaoTrilhasGravadasNoClickhouse: DDL versionado + writer em lote
// ponta a ponta contra ClickHouse efêmero — eventos enfileirados aparecem nas
// três tabelas após o drain do shutdown.
func TestIntegracaoTrilhasGravadasNoClickhouse(t *testing.T) {
	if !dockerDisponivel(t) {
		t.Skip("docker indisponível: teste de integração pulado")
	}
	ctx := context.Background()
	container, err := tcclickhouse.Run(ctx, "clickhouse/clickhouse-server:24.3-alpine")
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	host, err := container.Host(ctx)
	require.NoError(t, err)
	porta, err := container.MappedPort(ctx, "9000")
	require.NoError(t, err)

	// O DDL cria o database da config; a conexão de boot fala com o database
	// "default" só para aplicá-lo (o Ping do Connect exige o database vivo).
	cfgBase := config.ClickHouseConfig{
		// Módulo testcontainers sobe com usuário "default" e senha "default".
		Enabled: true, Host: host, Port: porta.Int(),
		User: container.User, Pass: container.Password, Database: "default",
	}
	connDDL := Connect(cfgBase)
	require.NotNil(t, connDDL)
	aplicarDDL(t, connDDL)
	require.NoError(t, connDDL.Close())

	conn := Connect(config.ClickHouseConfig{Enabled: true, Host: host,
		Port: porta.Int(), User: container.User, Pass: container.Password,
		Database: "workspace_logs"})
	require.NotNil(t, conn)
	t.Cleanup(func() { _ = conn.Close() })

	escritor := NovaEscritor(NovoGravador(conn), config.LogsConfig{LoteTamanho: 2, LoteJanelaMs: 60_000})
	escritor.EnfileirarAcesso(eventoAcesso())
	escritor.EnfileirarAuditoria(eventoAuditoria(1))
	escritor.EnfileirarAuditoria(eventoAuditoria(2))
	escritor.EnfileirarErros(eventoErro("identidade.workspace.slug_em_uso"))
	escritor.Fechar() // drain garante a gravação antes das consultas

	require.Equal(t, 1, contarLinhas(t, conn, "workspace_logs.log_acesso"))
	require.Equal(t, 2, contarLinhas(t, conn, "workspace_logs.log_auditoria"))
	require.Equal(t, 1, contarLinhas(t, conn, "workspace_logs.log_erro"))
}

func contarLinhas(t *testing.T, conn driver.Conn, tabela string) int {
	t.Helper()
	var total uint64
	require.NoError(t, conn.QueryRow(context.Background(),
		"SELECT count(*) FROM "+tabela).Scan(&total))
	return int(total)
}

// TestIntegracaoConsultorLeRecortes: a leitura (E5) devolve as linhas do
// recorte pedido — filtro por tenancy, ação e janela — com total sem
// paginação e ordem instante DESC.
func TestIntegracaoConsultorLeRecortes(t *testing.T) {
	if !dockerDisponivel(t) {
		t.Skip("docker indisponível: teste de integração pulado")
	}
	ctx := context.Background()
	container, err := tcclickhouse.Run(ctx, "clickhouse/clickhouse-server:24.3-alpine")
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	host, err := container.Host(ctx)
	require.NoError(t, err)
	porta, err := container.MappedPort(ctx, "9000")
	require.NoError(t, err)

	cfgBase := config.ClickHouseConfig{
		Enabled: true, Host: host, Port: porta.Int(),
		User: container.User, Pass: container.Password, Database: "default",
	}
	connDDL := Connect(cfgBase)
	require.NotNil(t, connDDL)
	aplicarDDL(t, connDDL)
	require.NoError(t, connDDL.Close())

	conn := Connect(config.ClickHouseConfig{Enabled: true, Host: host,
		Port: porta.Int(), User: container.User, Pass: container.Password,
		Database: "workspace_logs"})
	require.NotNil(t, conn)
	t.Cleanup(func() { _ = conn.Close() })

	const (
		orgA = "aaaaaaaa-0000-4000-8000-00000000000a"
		orgB = "bbbbbbbb-0000-4000-8000-00000000000b"
		wsA1 = "cccccccc-0000-4000-8000-0000000000a1"
		wsB1 = "dddddddd-0000-4000-8000-0000000000b1"
	)

	escritor := NovaEscritor(NovoGravador(conn), config.LogsConfig{LoteTamanho: 100, LoteJanelaMs: 60_000})
	evOrgA := eventoAuditoria(1)
	evOrgA.Sucesso = true
	evOrgA.OrganizationUUID, evOrgA.WorkspaceUUID, evOrgA.UserUUID = orgA, wsA1, "user-a"
	evOrgB := eventoAuditoria(2)
	evOrgB.OrganizationUUID, evOrgB.WorkspaceUUID, evOrgB.UserUUID = orgB, wsB1, "user-b"
	acessoOrgA := eventoAcesso()
	acessoOrgA.OrganizationUUID, acessoOrgA.WorkspaceUUID, acessoOrgA.UserUUID = orgA, wsA1, "user-a"
	acessoSemTenancy := eventoAcesso()
	erroOrgA := eventoErro("identidade.workspace.slug_em_uso")
	erroOrgA.OrganizationUUID, erroOrgA.WorkspaceUUID = orgA, wsA1

	escritor.EnfileirarAuditoria(evOrgA)
	escritor.EnfileirarAuditoria(evOrgB)
	escritor.EnfileirarAcesso(acessoOrgA)
	escritor.EnfileirarAcesso(acessoSemTenancy)
	escritor.EnfileirarErros(erroOrgA)
	escritor.Fechar()

	consultor := NovoConsultor(conn)

	itens, total, err := consultor.Auditoria(ctx, FiltroTrilha{OrganizationUUID: orgA, Limite: 10})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, itens, 1)
	require.Equal(t, orgA, itens[0].OrganizationUUID)
	require.True(t, itens[0].Sucesso)
	require.Equal(t, map[string]string{"i": "1"}, itens[0].Detalhes)

	itens, total, err = consultor.Auditoria(ctx, FiltroTrilha{Acao: "criar", Limite: 10})
	require.NoError(t, err)
	require.EqualValues(t, 2, total, "filtro por ação alcança as duas organizations")

	_, total, err = consultor.Auditoria(ctx, FiltroTrilha{Acao: "remover", Limite: 10})
	require.NoError(t, err)
	require.Zero(t, total, "ação inexistente devolve vazio com total zero — nunca erro")

	acessos, total, err := consultor.Acesso(ctx, FiltroTrilha{WorkspaceUUID: wsA1, Limite: 10})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, acessos, 1)
	require.Equal(t, orgA, acessos[0].OrganizationUUID)

	semFiltro, total, err := consultor.Acesso(ctx, FiltroTrilha{Limite: 10})
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Len(t, semFiltro, 2)

	janelaInicio := time.Now().UTC().Add(time.Minute)
	_, total, err = consultor.Auditoria(ctx, FiltroTrilha{InstanteInicio: janelaInicio, InstanteFim: janelaInicio.Add(time.Minute), Limite: 10})
	require.NoError(t, err)
	require.Zero(t, total, "janela futura não pega nada")

	errosLidos, total, err := consultor.Erros(ctx, FiltroTrilha{OrganizationUUID: orgA, Acao: "identidade.workspace.slug_em_uso", Limite: 10})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, errosLidos, 1)
	require.Equal(t, errobserve.SeveridadeWarn, errosLidos[0].Severidade)
}

// TestIntegracaoPaginacaoDoConsultor: LIMIT/OFFSET paginam sobre o total SEM
// paginação — o front monta o paginador com uma chamada só.
func TestIntegracaoPaginacaoDoConsultor(t *testing.T) {
	if !dockerDisponivel(t) {
		t.Skip("docker indisponível: teste de integração pulado")
	}
	ctx := context.Background()
	container, err := tcclickhouse.Run(ctx, "clickhouse/clickhouse-server:24.3-alpine")
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	host, err := container.Host(ctx)
	require.NoError(t, err)
	porta, err := container.MappedPort(ctx, "9000")
	require.NoError(t, err)

	connDDL := Connect(config.ClickHouseConfig{
		Enabled: true, Host: host, Port: porta.Int(),
		User: container.User, Pass: container.Password, Database: "default",
	})
	require.NotNil(t, connDDL)
	aplicarDDL(t, connDDL)
	require.NoError(t, connDDL.Close())

	conn := Connect(config.ClickHouseConfig{Enabled: true, Host: host,
		Port: porta.Int(), User: container.User, Pass: container.Password,
		Database: "workspace_logs"})
	require.NotNil(t, conn)
	t.Cleanup(func() { _ = conn.Close() })

	base := time.Now().UTC().Add(-time.Hour)
	escritor := NovaEscritor(NovoGravador(conn), config.LogsConfig{LoteTamanho: 100, LoteJanelaMs: 60_000})
	for i := 0; i < 5; i++ {
		ev := audit_log.Evento{
			Instante: base.Add(time.Duration(i) * time.Second),
			Dominio:  "identidade", Subdominio: "workspace", Acao: "criar", Sucesso: true,
			RayTrace: fmt.Sprintf("ray-%02d", i),
		}
		escritor.EnfileirarAuditoria(ev)
	}
	escritor.Fechar()

	consultor := NovoConsultor(conn)
	pagina, total, err := consultor.Auditoria(ctx, FiltroTrilha{Limite: 2, Offset: 0})
	require.NoError(t, err)
	require.EqualValues(t, 5, total, "total ignora a paginação")
	require.Len(t, pagina, 2)
	require.True(t, pagina[0].Instante.After(pagina[1].Instante), "ordem instante DESC")

	segunda, _, err := consultor.Auditoria(ctx, FiltroTrilha{Limite: 2, Offset: 2})
	require.NoError(t, err)
	require.Len(t, segunda, 2)
	require.True(t, pagina[1].Instante.After(segunda[0].Instante), "páginas não sobrepõem")
}
