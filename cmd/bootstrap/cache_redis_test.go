// Cenário E1 (issue #8) sobre Postgres + Redis efêmeros: os adaptadores de
// cache_redis.go — denylist composta (cache jwt:deny: + fonte persistida),
// cache de resolução por slug (contrato CacheResolucao), decorador de
// permissões com invalidação via ObservadorAtribuicoes e o lockout de login
// por (e-mail, IP). Montagem por construtores PUROS; o singleton do Redis é
// bootado aqui e SEMPRE resetado na saída (sync.Once é POR PROCESSO — os
// demais testes do pacote precisam encontrá-lo degradado).
package bootstrap

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	testredis "github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"

	dominioOrganizacao "workspace-api/internal/identidade/domain/organization"
	dominioUsuario "workspace-api/internal/identidade/domain/user"
	dominioWorkspace "workspace-api/internal/identidade/domain/workspace"
	orgmodel "workspace-api/internal/identidade/model/organization"
	modeluser "workspace-api/internal/identidade/model/user"
	modelworkspace "workspace-api/internal/identidade/model/workspace"
	rediscache "workspace-api/internal/infra/redis"
	"workspace-api/internal/middleware"
	"workspace-api/internal/pkg/config"
	"workspace-api/internal/pkg/orgctx"
)

// subirRedisEfemero sobe um Redis descartável e devolve a config mapeada.
func subirRedisEfemero(t *testing.T) config.RedisConfig {
	t.Helper()
	ctx := context.Background()
	container, err := testredis.Run(ctx, "redis:7-alpine",
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

// semearOrganizationPura cria uma organization REAL pelo construtor+repo
// (usuários/workspaces têm FK para ela).
func semearOrganizationPura(t *testing.T, amb *ambienteBanco, nome string) (*orgmodel.Organization, error) {
	t.Helper()
	o, err := orgmodel.NewOrganization(orgmodel.CreateInput{Nome: nome})
	if err != nil {
		return nil, err
	}
	if err := dominioOrganizacao.NewRepository(amb.db).Criar(amb.ctx, o); err != nil {
		return nil, err
	}
	return o, nil
}

// iniciarRedisDeTeste grava config válida com a seção redis/cache informadas
// e boota o singleton do Redis. TUDO é restaurado na saída (config e
// singleton) — nenhum outro teste deste pacote pode herdar estado daqui.
func iniciarRedisDeTeste(t *testing.T, cfgRedis config.RedisConfig) {
	t.Helper()
	exemplo := map[string]any{
		"app":      map[string]any{"name": "workspace-api", "env": "teste", "version": "0.0.0", "base_domain": "plataforma.teste"},
		"server":   map[string]any{"http": map[string]any{"port": 18080, "shutdown_timeout_sec": 10}},
		"security": map[string]any{"jwt_secret": "segredo-de-teste-do-cache-redis-32b!!", "jwt_ttl_min": 60, "jwt_refresh_ttl_hours": 168},
		"databases": map[string]any{
			"postgres":   map[string]any{"host": "127.0.0.1", "port": 5432, "user": "x", "pass": "x", "name": "x", "ssl_mode": "disable"},
			"migrations": map[string]any{"path": "../../db/migrations", "auto_run": false},
			"redis":      cfgRedis,
		},
		"cache": map[string]any{
			"ttl_resolucao_seg": 30, "ttl_permissoes_seg": 60,
			"login_lockout": map[string]any{"max_tentativas": 3, "janela_seg": 300, "bloqueio_seg": 2},
		},
	}
	conteudo, err := json.Marshal(exemplo)
	require.NoError(t, err)
	caminho := filepath.Join(t.TempDir(), "configs.json")
	require.NoError(t, os.WriteFile(caminho, conteudo, 0o600))

	config.ResetarParaTeste()
	require.NoError(t, config.Init(caminho))
	rediscache.ResetarParaTeste()
	_, err = rediscache.InitRedis()
	require.NoError(t, err)

	t.Cleanup(func() {
		rediscache.ResetarParaTeste() // demais testes veem o adaptador DEGRADADO
		config.ResetarParaTeste()
	})
}

// --- Denylist composta --------------------------------------------------------

func TestRevogadorComCacheHitMissEEscritaDoPositivo(t *testing.T) {
	if !dockerDisponivel(t) {
		t.Skip("docker indisponível — teste de integração pulado")
	}
	iniciarRedisDeTeste(t, subirRedisEfemero(t))
	cliente, err := rediscache.Get()
	require.NoError(t, err)
	require.NotNil(t, cliente)
	ctx := context.Background()

	// Fonte FAKE: conta consultas — prova quem respondeu cada pergunta.
	fonte := &fonteRevogadorContadora{resposta: map[string]bool{}}
	composto := novoRevogadorComCache(fonte)

	// 1. Hit positivo no cache responde ANTES da fonte (jti que a fonte nem
	// conhece): revogação confirmada só pelo cache — nunca ao contrário.
	require.NoError(t, cliente.Set(ctx, rediscache.ChaveDenylistJWT("jti-so-no-cache"), "1", time.Minute).Err())
	revogado, err := composto.Revogado("jti-so-no-cache")
	require.NoError(t, err)
	require.True(t, revogado)
	require.Zero(t, fonte.consultas["jti-so-no-cache"])

	// 2. Miss cai à fonte; positivo da fonte é GRAVADO no cache (best-effort).
	fonte.resposta["jti-revogado-na-fonte"] = true
	revogado, err = composto.Revogado("jti-revogado-na-fonte")
	require.NoError(t, err)
	require.True(t, revogado)
	require.Equal(t, 1, fonte.consultas["jti-revogado-na-fonte"])
	valor, err := cliente.Get(ctx, rediscache.ChaveDenylistJWT("jti-revogado-na-fonte")).Result()
	require.NoError(t, err)
	require.Equal(t, "1", valor)

	// 3. Segunda consulta do mesmo jti NÃO volta à fonte: cache serviu.
	_, err = composto.Revogado("jti-revogado-na-fonte")
	require.NoError(t, err)
	require.Equal(t, 1, fonte.consultas["jti-revogado-na-fonte"])

	// 4. Negativo NUNCA é cacheado: jti limpo consulta a fonte toda vez —
	// logout/rotação valem NA HORA porque não existe "não revogado" em cache.
	_, err = composto.Revogado("jti-limpo")
	require.NoError(t, err)
	_, err = composto.Revogado("jti-limpo")
	require.NoError(t, err)
	require.Equal(t, 2, fonte.consultas["jti-limpo"])

	// 5. Sem Redis (degradado), tudo vai à fonte — comportamento pré-evolução.
	rediscache.ResetarParaTeste()
	revogado, err = composto.Revogado("jti-revogado-na-fonte")
	require.NoError(t, err)
	require.True(t, revogado)
	require.Equal(t, 2, fonte.consultas["jti-revogado-na-fonte"])
}

type fonteRevogadorContadora struct {
	resposta  map[string]bool
	consultas map[string]int
}

func (f *fonteRevogadorContadora) Revogado(jti string) (bool, error) {
	if f.consultas == nil {
		f.consultas = map[string]int{}
	}
	f.consultas[jti]++
	return f.resposta[jti], nil
}

// --- Cache de resolução por slug ----------------------------------------------

func TestCacheResolucaoSlugHitInvalidacaoECascataDaOrganization(t *testing.T) {
	if !dockerDisponivel(t) {
		t.Skip("docker indisponível — teste de integração pulado")
	}
	iniciarRedisDeTeste(t, subirRedisEfemero(t))
	cliente, err := rediscache.Get()
	require.NoError(t, err)
	ctx := context.Background()

	cache := cacheResolucaoRedis{}
	entrada := dominioWorkspace.EntradaResolucao{
		WorkspaceUUID:    uuid.NewString(),
		OrganizationUUID: uuid.NewString(),
		Status:           string(modelworkspace.StatusAtivo),
	}

	// Miss → Guardar → Hit → Invalidar → Miss.
	_, ok := cache.Buscar(ctx, "filial-sul")
	require.False(t, ok)
	cache.Guardar(ctx, "filial-sul", entrada)
	visto, ok := cache.Buscar(ctx, "filial-sul")
	require.True(t, ok)
	require.Equal(t, entrada.WorkspaceUUID, visto.WorkspaceUUID)
	_, err = cliente.Get(ctx, rediscache.ChaveResolucaoSlug("filial-sul")).Result()
	require.NoError(t, err, "entrada persistida sob workspace:slug:{slug}")
	cache.Invalidar(ctx, "filial-sul")
	_, ok = cache.Buscar(ctx, "filial-sul")
	require.False(t, ok)

	// InvalidarOrganization é SELETIVO: derruba as entradas da organization
	// e preserva as das vizinhas (invalidação grosseira ≠ bombardeio).
	orgAlvo := uuid.NewString()
	orgVizinha := uuid.NewString()
	cache.Guardar(ctx, "alvo", dominioWorkspace.EntradaResolucao{WorkspaceUUID: uuid.NewString(), OrganizationUUID: orgAlvo, Status: "ativo"})
	cache.Guardar(ctx, "vizinha", dominioWorkspace.EntradaResolucao{WorkspaceUUID: uuid.NewString(), OrganizationUUID: orgVizinha, Status: "ativo"})
	cache.InvalidarOrganization(ctx, orgAlvo)
	_, ok = cache.Buscar(ctx, "alvo")
	require.False(t, ok)
	vistoVizinha, ok := cache.Buscar(ctx, "vizinha")
	require.True(t, ok)
	require.Equal(t, orgVizinha, vistoVizinha.OrganizationUUID)
}

func TestInativacaoDeWorkspaceInvalidaCacheNaEscrita(t *testing.T) {
	if !dockerDisponivel(t) {
		t.Skip("docker indisponível — teste de integração pulado")
	}
	iniciarRedisDeTeste(t, subirRedisEfemero(t))
	amb := subirAmbiente(t)

	svc := dominioWorkspace.NewService(dominioWorkspace.NewRepository(amb.db), cacheResolucaoRedis{})
	o, err := semearOrganizationPura(t, amb, "Org Cache "+uuid.NewString())
	require.NoError(t, err)
	ctxOrg := orgctx.WithOrganization(amb.ctx, o.UUID)

	ws, err := svc.Create(ctxOrg, modelworkspace.CreateInput{Nome: "Filial Cache", Slug: "filial-cache"})
	require.NoError(t, err)

	// Resolução aquece o cache com o status ativo.
	resolvido, err := svc.ResolverPorSlug(amb.ctx, "filial-cache")
	require.NoError(t, err)
	require.True(t, resolvido.Ativo())

	// Inativação PELO SERVICE invalida o cache: a resolução seguinte reflete
	// o status novo sem esperar TTL (filho nunca mais vivo que o pai).
	inativo := modelworkspace.StatusInativo
	_, err = svc.Update(ctxOrg, ws.UUID, modelworkspace.UpdateInput{Status: &inativo})
	require.NoError(t, err)
	resolvido, err = svc.ResolverPorSlug(amb.ctx, "filial-cache")
	require.NoError(t, err)
	require.False(t, resolvido.Ativo(), "cache invalidado na escrita: inatividade visível na hora")

	// Reativação devolve o workspace ao ar imediatamente (mesma regra).
	_, err = svc.Reativar(ctxOrg, ws.UUID)
	require.NoError(t, err)
	resolvido, err = svc.ResolverPorSlug(amb.ctx, "filial-cache")
	require.NoError(t, err)
	require.True(t, resolvido.Ativo())
}

// --- Decorador de permissões + observador de atribuição -----------------------

func TestDecoradorPermissoesCacheEInvalidacaoPorObservador(t *testing.T) {
	if !dockerDisponivel(t) {
		t.Skip("docker indisponível — teste de integração pulado")
	}
	iniciarRedisDeTeste(t, subirRedisEfemero(t))
	ctx := context.Background()

	base := &resolvedorBaseContadora{permissoes: []string{"identidade:user:ler"}}
	decorado := novoResolvedorPermissoesComCache(base)
	usuario, organizacao, workspace := uuid.New(), uuid.New(), uuid.New()

	// 1ª chamada: base responde e popula perm:{org}:{user}:{wks}.
	primeira, err := decorado.PermissoesEfetivas(ctx, usuario, organizacao, workspace)
	require.NoError(t, err)
	require.Equal(t, []string{"identidade:user:ler"}, primeira)
	require.Equal(t, 1, base.chamadas)

	// 2ª chamada: cache responde — base não é tocada.
	_, err = decorado.PermissoesEfetivas(ctx, usuario, organizacao, workspace)
	require.NoError(t, err)
	require.Equal(t, 1, base.chamadas)

	// Observador (contrato ligado ao service do user) invalida o par exato.
	invalidadorPermissoesRedis{}.AtribuicaoAlterada(
		orgctx.WithOrganization(ctx, organizacao), usuario, workspace)
	_, err = decorado.PermissoesEfetivas(ctx, usuario, organizacao, workspace)
	require.NoError(t, err)
	require.Equal(t, 2, base.chamadas, "invalidação derruba só o cache: base volta a ser consultada")

	// uuid.Nil = invalidação GROSSEIRA: TODAS as entradas do usuário na
	// organization caem (varredura perm:{org}:{user}:*), inclusive pares que
	// nunca foram consultados — grosseira e segura por desenho.
	outroWorkspace := uuid.New()
	_, err = decorado.PermissoesEfetivas(ctx, usuario, organizacao, outroWorkspace)
	require.NoError(t, err)
	require.Equal(t, 3, base.chamadas) // par novo = miss legítimo
	invalidadorPermissoesRedis{}.AtribuicaoAlterada(
		orgctx.WithOrganization(ctx, organizacao), usuario, uuid.Nil)
	_, err = decorado.PermissoesEfetivas(ctx, usuario, organizacao, workspace)
	require.NoError(t, err)
	_, err = decorado.PermissoesEfetivas(ctx, usuario, organizacao, outroWorkspace)
	require.NoError(t, err)
	require.Equal(t, 5, base.chamadas, "varredura derruba TODOS os pares do usuário")

	// TemVinculo NÃO é cacheado: caminho de concessão passa sempre na base.
	_, err = decorado.TemVinculo(ctx, usuario, organizacao, workspace)
	require.NoError(t, err)
	_, err = decorado.TemVinculo(ctx, usuario, organizacao, workspace)
	require.NoError(t, err)
	require.Equal(t, 2, base.vinculos)

	// Degradado (sem Redis): decorador vira passagem direta.
	rediscache.ResetarParaTeste()
	_, err = decorado.PermissoesEfetivas(ctx, usuario, organizacao, workspace)
	require.NoError(t, err)
	require.Equal(t, 6, base.chamadas)
}

type resolvedorBaseContadora struct {
	chamadas   int
	vinculos   int
	permissoes []string
}

func (r *resolvedorBaseContadora) TemVinculo(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (bool, error) {
	r.vinculos++
	return true, nil
}

func (r *resolvedorBaseContadora) PermissoesEfetivas(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) ([]string, error) {
	r.chamadas++
	return r.permissoes, nil
}

var _ middleware.ResolvedorPermissoes = (*resolvedorBaseContadora)(nil)

// Atribuição REAL pelo service dispara o observador ligado por opção —
// prova a fiação completa bootstrap ↔ subdomínio user ↔ Redis.
func TestAtribuirERemoverPapelInvalidamCacheViaObservador(t *testing.T) {
	if !dockerDisponivel(t) {
		t.Skip("docker indisponível — teste de integração pulado")
	}
	iniciarRedisDeTeste(t, subirRedisEfemero(t))
	amb := subirAmbiente(t)
	cliente, err := rediscache.Get()
	require.NoError(t, err)
	ctx := context.Background()

	svcUser := dominioUsuario.NewService(
		dominioUsuario.NewRepository(amb.db),
		dominioUsuario.NewRepositorioAtribuicoes(amb.db),
		validadorSemprePertence{}, dominioUsuario.NovasCredenciaisBcrypt(),
		dominioUsuario.ComObservadorAtribuicoes(invalidadorPermissoesRedis{}))

	o, err := semearOrganizationPura(t, amb, "Org Cache "+uuid.NewString())
	require.NoError(t, err)
	ctxOrg := orgctx.WithOrganization(amb.ctx, o.UUID)

	ana, err := svcUser.Create(ctxOrg, dominioUsuario.EntradaCriacao{
		Dados: modeluser.CreateInput{Nome: "Ana Cache", Email: "ana-cache@plataforma.teste"},
		Senha: "senha-segura-123",
	})
	require.NoError(t, err)

	papel := uuid.New()
	require.NoError(t, amb.db.Exec(`INSERT INTO identidade_user_papel (uuid, nome, descricao)
		VALUES (?, 'papel_cache', 'papel do teste de cache') ON CONFLICT (nome) DO NOTHING`, papel).Error)

	wsSvc := dominioWorkspace.NewService(dominioWorkspace.NewRepository(amb.db), nil)
	ws, err := wsSvc.Create(ctxOrg, modelworkspace.CreateInput{Nome: "Filial Papel", Slug: "filial-papel"})
	require.NoError(t, err)

	chave := rediscache.ChavePermissoes(o.UUID.String(), ana.UUID.String(), ws.UUID.String())
	// Aquece o cache como se já houvesse permissões lá.
	require.NoError(t, cliente.Set(ctx, chave, `["identidade:user:ler"]`, time.Minute).Err())

	// Atribuir papel (escrita real) dispara o observador → chave some.
	_, err = svcUser.AtribuirPapel(ctxOrg, ana.UUID, ws.UUID, papel)
	require.NoError(t, err)
	existe, err := cliente.Exists(ctx, chave).Result()
	require.NoError(t, err)
	require.Zero(t, existe, "atribuição nova invalida o cache do par")

	// Remoção usa a invalidação GROSSEIRA (todas as entradas do usuário).
	atribuicoes, err := svcUser.Atribuicoes(ctxOrg, ana.UUID)
	require.NoError(t, err)
	require.Len(t, atribuicoes, 1)
	require.NoError(t, cliente.Set(ctx, chave, `["identidade:user:ler"]`, time.Minute).Err())
	require.NoError(t, svcUser.RemoverAtribuicao(ctxOrg, ana.UUID, atribuicoes[0].UUID))
	existe, err = cliente.Exists(ctx, chave).Result()
	require.NoError(t, err)
	require.Zero(t, existe, "remoção invalida as entradas do usuário (grosseira)")
}

// --- Lockout de login (adaptador da aplicação auth) ---------------------------

func TestLimitadorLoginAuthBloqueiaCumpreELibera(t *testing.T) {
	if !dockerDisponivel(t) {
		t.Skip("docker indisponível — teste de integração pulado")
	}
	// Config do teste: teto 3, bloqueio de 2s (iniciarRedisDeTeste fixa).
	iniciarRedisDeTeste(t, subirRedisEfemero(t))

	limitador := limitadorLoginAuth{}
	email, ip := "bruto@Exemplo.COM ", "10.9.8.7"

	bloqueado, espera, err := limitador.Autorizado(context.Background(), email, ip)
	require.NoError(t, err)
	require.False(t, bloqueado)
	require.Zero(t, espera)

	for i := 0; i < 2; i++ { // abaixo do teto: livre
		require.NoError(t, limitador.RegistrarFalha(context.Background(), email, ip))
		bloqueado, _, err = limitador.Autorizado(context.Background(), email, ip)
		require.NoError(t, err)
		require.False(t, bloqueado)
	}
	require.NoError(t, limitador.RegistrarFalha(context.Background(), email, ip)) // 3ª = teto
	bloqueado, espera, err = limitador.Autorizado(context.Background(), email, ip)
	require.NoError(t, err)
	require.True(t, bloqueado)
	require.Greater(t, espera, time.Duration(0))

	// Sucesso zera contador pendente mas NÃO descumpre bloqueio ativo.
	require.NoError(t, limitador.RegistrarSucesso(context.Background(), email, ip))
	bloqueado, _, err = limitador.Autorizado(context.Background(), email, ip)
	require.NoError(t, err)
	require.True(t, bloqueado)

	// Cumprido o bloqueio curto da config de teste, o par volta a logar.
	time.Sleep(2100 * time.Millisecond)
	bloqueado, _, err = limitador.Autorizado(context.Background(), email, ip)
	require.NoError(t, err)
	require.False(t, bloqueado)

	// Degradado: sem lockout, sem erro (feature desligada, nunca login preso).
	rediscache.ResetarParaTeste()
	bloqueado, espera, err = limitador.Autorizado(context.Background(), email, ip)
	require.NoError(t, err)
	require.False(t, bloqueado)
	require.Zero(t, espera)
	require.NoError(t, limitador.RegistrarFalha(context.Background(), email, ip))
	require.NoError(t, limitador.RegistrarSucesso(context.Background(), email, ip))
}
