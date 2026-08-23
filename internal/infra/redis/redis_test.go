package redis

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"

	"workspace-api/internal/pkg/config"
)

// dockerDisponivel confere o daemon antes de tentar conexão real — sem
// ambiente o teste pula, nunca reprova (mesmo padrão do pacote postgres).
func dockerDisponivel(t *testing.T) bool {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "docker", "info", "--format", "{{.ServerVersion}}").Run() == nil
}

// subirRedisEfemero sobe um Redis descartável e devolve a config de conexão
// já com host/porta mapeados pelo docker.
func subirRedisEfemero(t *testing.T) config.RedisConfig {
	t.Helper()
	ctx := context.Background()
	container, err := redis.Run(ctx, "redis:7-alpine",
		testcontainers.WithWaitStrategy(
			wait.ForListeningPort("6379/tcp").WithStartupTimeout(60*time.Second)),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	host, err := container.Host(ctx)
	require.NoError(t, err)
	porta, err := container.MappedPort(ctx, "6379")
	require.NoError(t, err)
	return config.RedisConfig{Enabled: true, Host: host, Port: porta.Int()}
}

// TestConnectDesabilitadoDevolveClienteNulo prova o contrato degradável sem
// rede alguma: enabled=false → nil SEM erro e sem pânico.
func TestConnectDesabilitadoDevolveClienteNulo(t *testing.T) {
	cliente := Connect(config.RedisConfig{Enabled: false})
	require.Nil(t, cliente)
}

// TestConnectInacessivelDevolveClienteNulo: servidor configurado mas fora do
// ar no boot = [DEGRADADO] com cliente nulo — NUNCA erro que derrube o boot.
func TestConnectInacessivelDevolveClienteNulo(t *testing.T) {
	inicio := time.Now()
	cliente := Connect(config.RedisConfig{Enabled: true, Host: "127.0.0.1", Port: portaMorta()})
	duracao := time.Since(inicio)
	require.Nil(t, cliente)
	// Timeout curto de dial: degradação não pode travar o boot.
	require.Less(t, duracao, 10*time.Second)
}

// TestDenylistCacheSomentePositivo exercita a política da denylist contra
// Redis real: negativo nunca é cacheado; positivo persiste até o TTL.
func TestDenylistCacheSomentePositivo(t *testing.T) {
	if !dockerDisponivel(t) {
		t.Skip("docker indisponível: teste de integração pulado")
	}
	cfg := subirRedisEfemero(t)
	cliente := Connect(cfg)
	require.NotNil(t, cliente)
	t.Cleanup(func() { _ = cliente.Close() })

	denylist := NovaDenylist(cliente, 2*time.Second)

	revogado, err := denylist.Revogado("jti-inexistente")
	require.NoError(t, err)
	require.False(t, revogado) // ausente ≠ revogado

	require.NoError(t, denylist.MarcarRevogado("jti-revogado"))
	revogado, err = denylist.Revogado("jti-revogado")
	require.NoError(t, err)
	require.True(t, revogado)

	// TTL expira e a entrada some — depois disso a fonte decide de novo.
	time.Sleep(2200 * time.Millisecond)
	revogado, err = denylist.Revogado("jti-revogado")
	require.NoError(t, err)
	require.False(t, revogado)
}

// TestDenylistDegradadaEhNoOp: cliente nil devolve falso SEM erro — o
// consumidor composto cai à fonte sem nem saber.
func TestDenylistDegradadaEhNoOp(t *testing.T) {
	denylist := NovaDenylist(nil, time.Minute)
	revogado, err := denylist.Revogado("qualquer")
	require.NoError(t, err)
	require.False(t, revogado)
	require.NoError(t, denylist.MarcarRevogado("qualquer"))
}

// TestLimitadorLoginFluxoCompleto cobre janela/teto/bloqueio/limpeza contra
// Redis real: falhas abaixo do teto não prendem; teto estourado prende pelo
// bloqueio; login bom zera o contador; cumprir a pena liberta.
func TestLimitadorLoginFluxoCompleto(t *testing.T) {
	if !dockerDisponivel(t) {
		t.Skip("docker indisponível: teste de integração pulado")
	}
	cfg := subirRedisEfemero(t)
	cliente := Connect(cfg)
	require.NotNil(t, cliente)
	t.Cleanup(func() { _ = cliente.Close() })

	ctx := context.Background()
	limitador := NovoLimitadorLogin(cliente, 3, 5*time.Second, 1*time.Second)

	for i := 0; i < 2; i++ {
		bloqueado, _, err := limitador.Bloqueado(ctx, "Ana@Exemplo.com", "10.0.0.1")
		require.NoError(t, err)
		require.False(t, bloqueado)
		require.NoError(t, limitador.RegistrarFalha(ctx, "ana@exemplo.com", "10.0.0.1"))
	}
	// Abaixo do teto: livre — e caixa alta/IP espaçado é O MESMO contador.
	bloqueado, _, err := limitador.Bloqueado(ctx, " ANA@EXEMPLO.COM ", "10.0.0.1")
	require.NoError(t, err)
	require.False(t, bloqueado)

	require.NoError(t, limitador.RegistrarFalha(ctx, "ana@exemplo.com", "10.0.0.1"))
	bloqueado, espera, err := limitador.Bloqueado(ctx, "ana@exemplo.com", "10.0.0.1")
	require.NoError(t, err)
	require.True(t, bloqueado)
	require.Greater(t, espera, time.Duration(0))

	// Sucesso limpa contador pendente, mas NÃO descumpre bloqueio ativo.
	require.NoError(t, limitador.RegistrarSucesso(ctx, "ana@exemplo.com", "10.0.0.1"))
	bloqueado, _, err = limitador.Bloqueado(ctx, "ana@exemplo.com", "10.0.0.1")
	require.NoError(t, err)
	require.True(t, bloqueado)

	// Outro par tem histórico próprio.
	bloqueado, _, err = limitador.Bloqueado(ctx, "bruno@exemplo.com", "10.0.0.2")
	require.NoError(t, err)
	require.False(t, bloqueado)

	// Cumprida a pena, o par volta a logar.
	time.Sleep(1100 * time.Millisecond)
	bloqueado, _, err = limitador.Bloqueado(ctx, "ana@exemplo.com", "10.0.0.1")
	require.NoError(t, err)
	require.False(t, bloqueado)
}

// TestLimitadorDegradadoEhNoOp: sem Redis não existe lockout e nada falha.
func TestLimitadorDegradadoEhNoOp(t *testing.T) {
	limitador := NovoLimitadorLogin(nil, 3, time.Minute, time.Minute)
	ctx := context.Background()
	bloqueado, espera, err := limitador.Bloqueado(ctx, "a@b.c", "1.2.3.4")
	require.NoError(t, err)
	require.False(t, bloqueado)
	require.Zero(t, espera)
	require.NoError(t, limitador.RegistrarFalha(ctx, "a@b.c", "1.2.3.4"))
	require.NoError(t, limitador.RegistrarSucesso(ctx, "a@b.c", "1.2.3.4"))
}

// TestChavesFixas guarda o formato das chaves — mudança aqui é QUEBRA de
// contrato de namespace compartilhado e exige migration de chave/documento.
func TestChavesFixas(t *testing.T) {
	require.Equal(t, "perm:o:u:w", ChavePermissoes("o", "u", "w"))
	require.Equal(t, "perm:o:u:*", ChavePermissoesUsuario("o", "u"))
	require.Equal(t, "workspace:slug:filial", ChaveResolucaoSlug("filial"))
	require.Equal(t, "jwt:deny:x", ChaveDenylistJWT("x"))
	require.Len(t, ChaveLockContador("a@b.c", "1.2.3.4"), len("lock:c:")+64) // sha256 hex
	require.Contains(t, ChaveLockContador("a@b.c", "1.2.3.4"), PrefixoLock+"c:")
	require.Contains(t, ChaveLockBloqueio("a@b.c", "1.2.3.4"), PrefixoLock+"b:")
	// Mesmo par normalizado → mesma chave; PII não repousa crua na chave.
	require.Equal(t, ChaveLockContador("A@B.C ", "1.2.3.4"), ChaveLockContador("a@b.c", "1.2.3.4"))
}

// TestCicloSingletonUnico: ÚNICO teste do ciclo de vida do singleton (o
// sync.Once não se desfaz entre casos) — InitRedis sem config erra; após
// ResetarParaTeste + config válida, segunda chamada devolve a MESMA instância.
func TestCicloSingletonUnico(t *testing.T) {
	ResetarParaTeste()
	t.Cleanup(ResetarParaTeste)

	_, err := Get()
	require.ErrorIs(t, err, ErrNaoInicializado) // antes do boot

	iniciarConfigDeTeste(t, config.RedisConfig{Enabled: false})
	_, err = InitRedis()
	require.NoError(t, err) // desabilitado na config = degradado, NUNCA erro
	cliente, err := Get()
	require.NoError(t, err)
	require.Nil(t, cliente) // degradado
	require.False(t, Disponivel())

	ResetarParaTeste()
	cfgReal := subirRedisEfemeroSkipSemDocker(t)
	iniciarConfigDeTeste(t, cfgReal)
	primeira, err := InitRedis()
	require.NoError(t, err)
	segunda, err := InitRedis()
	require.NoError(t, err)
	require.Same(t, primeira, segunda)
	require.True(t, Disponivel())
	Close()
	require.False(t, Disponivel())
	Close() // idempotente
}

// --- auxiliares -------------------------------------------------------------

func subirRedisEfemeroSkipSemDocker(t *testing.T) config.RedisConfig {
	t.Helper()
	if !dockerDisponivel(t) {
		t.Skip("docker indisponível: parte do ciclo do singleton com Redis real pulada")
	}
	return subirRedisEfemero(t)
}

// iniciarConfigDeTeste grava uma config válida mínima com a seção redis
// informada e monta o singleton da pkg/config para o boot do teste.
func iniciarConfigDeTeste(t *testing.T, redisCfg config.RedisConfig) {
	t.Helper()
	conteudo := map[string]any{
		"app":  map[string]any{"name": "teste", "env": "dev", "version": "0", "base_domain": "localhost"},
		"server": map[string]any{
			"http": map[string]any{"port": 8081, "shutdown_timeout_sec": 5},
		},
		"security":  map[string]any{"jwt_secret": "segredo-de-teste-com-mais-de-32-bytes!!", "jwt_ttl_min": 60, "jwt_refresh_ttl_hours": 24},
		"databases": map[string]any{"postgres": map[string]any{"host": "localhost", "port": 5432, "name": "teste"}, "migrations": map[string]any{"path": "db/migrations"}, "redis": redisCfg},
	}
	corpo, err := json.Marshal(conteudo)
	require.NoError(t, err)
	arquivo := filepath.Join(t.TempDir(), "configs_teste.json")
	require.NoError(t, os.WriteFile(arquivo, corpo, 0o600))
	config.ResetarParaTeste()
	require.NoError(t, config.Init(arquivo))
}

// portaMorta devolve uma porta de loopback sem listener (dial recusa rápido).
func portaMorta() int {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 1 // porta reservada sem listener conhecido
	}
	porta := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	return porta
}
