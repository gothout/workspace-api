package clickhouse

import (
	"context"
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
	for _, arquivo := range []string{"0001_log_acesso.sql", "0002_log_auditoria.sql", "0003_log_erros.sql"} {
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
