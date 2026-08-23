// Package bootstrap monta o processo: ordem de boot inviolável (config →
// validator → postgres → jwt → migrations → middleware/domínios → HTTP),
// toda ligação de interface entre pacotes por adaptadores que resolvem NA
// CHAMADA e fechamento LIFO no encerramento.
//
// É o ÚNICO lugar onde infra e infra se encontram — e o único com direito a
// pânico implícito via MustUse.
package bootstrap

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os/signal"
	"syscall"
	"time"

	dominioOrganizacao "workspace-api/internal/identidade/domain/organization"
	dominioUsuario "workspace-api/internal/identidade/domain/user"
	dominioWorkspace "workspace-api/internal/identidade/domain/workspace"

	aplicacaoauth "workspace-api/internal/identidade/application/auth"
	aplicacaocatalogo "workspace-api/internal/identidade/application/catalogo"
	aplicacaologs "workspace-api/internal/identidade/application/logs"

	"workspace-api/cmd/server"
	"workspace-api/cmd/server/routes"
	"workspace-api/internal/infra/clickhouse"
	"workspace-api/internal/infra/database/migrations"
	"workspace-api/internal/infra/database/postgres"
	"workspace-api/internal/infra/jwt"
	rediscache "workspace-api/internal/infra/redis"
	"workspace-api/internal/pkg/config"
	"workspace-api/internal/pkg/errobserve"
	"workspace-api/internal/pkg/validator"
)

// Serve executa o boot completo e bloqueia servindo HTTP até SIGTERM/SIGINT;
// o fechamento LIFO roda no retorno.
func Serve(caminhoConfig string) error {
	fechamentos := &pilhaFechamento{}
	defer fechamentos.executar()

	ctxBoot := context.Background()

	// 1. Config — sem config nada mais sobe; erro é fatal e acionável.
	if err := config.Init(caminhoConfig); err != nil {
		return fmt.Errorf("boot: %w", err)
	}
	cfg := config.MustUse()
	slog.Info("[BOOTSTRAP] config carregada",
		"app", cfg.App.Name, "env", cfg.App.Env, "versao", cfg.App.Version)

	// 2. Validator — tags customizadas do binding registradas UMA vez.
	if err := validator.Registrar(); err != nil {
		return fmt.Errorf("boot: %w", err)
	}
	slog.Info("[BOOTSTRAP] validator registrado")

	// 3. Postgres — FATAL: sem banco não há API gerenciadora.
	if _, err := postgres.InitPostgres(ctxBoot); err != nil {
		return fmt.Errorf("boot: %w", err)
	}
	fechamentos.empilhar(func() { _ = postgres.Close() })
	slog.Info("[BOOTSTRAP] postgres conectado")

	// 4. JWT — FATAL: assinar token sem chave confiável não pode subir.
	if _, err := jwt.InitJWT(); err != nil {
		return fmt.Errorf("boot: %w", err)
	}
	fechamentos.empilhar(jwt.Close)
	slog.Info("[BOOTSTRAP] jwt inicializado")

	// 4.5 Redis — DEGRADÁVEL (evolução #8): desabilitado/inacessível NUNCA
	// derruba o boot; os adaptadores de cache_redis.go tratam a ausência.
	// Degradado é EVENTO DE PLATAFORMA (sistema.degradacao_dependencia).
	clienteRedis, err := rediscache.InitRedis()
	if err != nil {
		return fmt.Errorf("boot: %w", err)
	}
	fechamentos.empilhar(rediscache.Close)
	if clienteRedis == nil {
		observadorPlataforma().Observe(ctxBoot,
			fmt.Errorf("%w: redis indisponível no boot (cache e lockout desligados)", errobserve.ErrDegradacao))
	}

	// 4.6 ClickHouse + trilhas de log — DEGRADÁVEL (evolução #9): sem banco,
	// auditoria e acesso saem pelo stdout; com banco, o writer em lote
	// consome as duas trilhas fora do caminho síncrono do request. O Close
	// DRENA o writer (lotes pendentes vão ao banco) antes do pool fechar.
	trilhasLog, err := iniciarTrilhas()
	if err != nil {
		return fmt.Errorf("boot: %w", err)
	}
	fechamentos.empilhar(clickhouse.Close)

	// 5. Migrations — `up` automático quando auto_run (advisory lock impede
	// réplicas de migrar juntas); rollback NUNCA é automático. Falha aqui é
	// fatal e vira EVENTO DE PLATAFORMA critical (sistema.migrations.up).
	if cfg.Databases.Migrations.AutoRun {
		aplicadas, err := migrations.Subir(ctxBoot, fonteBanco{}, cfg.Databases.Migrations.Path, timeoutsMigracao(cfg))
		if err != nil {
			observadorPlataforma().Observe(ctxBoot, fmt.Errorf("%w: %v", errobserve.ErrMigracao, err))
			return fmt.Errorf("boot: migrations: %w", err)
		}
		if aplicadas > 0 {
			slog.Info("[BOOTSTRAP] migrations aplicadas", "quantidade", aplicadas)
		} else {
			slog.Info("[BOOTSTRAP] migrations em dia")
		}
	}

	// 6. Middleware — cadeia de auth/resolução/autorização ligada por
	// adaptadores que resolvem NA CHAMADA (cmd/bootstrap/middleware.go);
	// sobe antes dos controllers que consomem as funções de pacote.
	gerenciadorJWT, err := jwt.Get()
	if err != nil {
		return fmt.Errorf("boot: %w", err)
	}
	if err := ligarMiddleware(gerenciadorJWT); err != nil {
		return fmt.Errorf("boot: middleware: %w", err)
	}
	slog.Info("[BOOTSTRAP] middleware da cadeia de autorização inicializado")

	// Revogação persistida do refresh (F4) + denylist Redis (#8): o
	// revogador composto confere o cache jwt:deny:{jti} e cai à tabela
	// persistida no miss — negativo nunca é cacheado, então logout/rotação
	// valem na hora (fonte da verdade segue sendo o Postgres).
	gerenciadorJWT.DefinirRevogador(novoRevogadorComCache(nil))

	// 7. Domínios — subdomínios de internal/identidade/domain na ordem de
	// dependência (organization → workspace → user); cada adaptador do
	// middleware acima resolve estes singletons NA CHAMADA. A cascata
	// organization→{workspace,user} entra pelos contratos (contratos.go da
	// organization) com os adaptadores de workspace.go e usuario.go
	// resolvendo NA CHAMADA.
	db, err := postgres.GetDB()
	if err != nil {
		return fmt.Errorf("boot: %w", err)
	}
	// A trilha de auditoria assíncrona (#9) entra em TODOS os subdomínios e
	// na aplicação auth — destino ClickHouse quando existe, stdout quando não.
	opcaoTrilha := dominioOrganizacao.ComTrilha(trilhasLog.auditoria)
	_, err = dominioOrganizacao.New(db, suspendedorWorkspaces{}, encerradorSessoesUsuario{}, opcaoTrilha)
	if err != nil {
		return fmt.Errorf("boot: %w", err)
	}
	slog.Info("[BOOTSTRAP-DI] Contêiner Identidade/Organization inicializado.")

	// Cache de resolução por slug (#8): contrato CacheResolucao do subdomínio
	// com implementação Redis — sem Redis o adaptador vira no-op (consulta
	// direta à fonte, operação normal).
	_, err = dominioWorkspace.New(db, cacheResolucaoRedis{}, dominioWorkspace.ComTrilha(trilhasLog.auditoria))
	if err != nil {
		return fmt.Errorf("boot: %w", err)
	}
	slog.Info("[BOOTSTRAP-DI] Contêiner Identidade/Workspace inicializado.")

	_, err = dominioUsuario.New(db, validadorWorkspaces{},
		dominioUsuario.ComObservadorAtribuicoes(invalidadorPermissoesRedis{}),
		dominioUsuario.ComTrilha(trilhasLog.auditoria))
	if err != nil {
		return fmt.Errorf("boot: %w", err)
	}
	slog.Info("[BOOTSTRAP-DI] Contêiner Identidade/User inicializado.")

	// Aplicações — orquestrações que cruzam os subdomínios acima. O auth
	// recebe os contratos ligados por adaptadores que resolvem os singletons
	// NA CHAMADA (usuario.go) — inclusive a vitalidade da organization dona
	// da sessão (R4), o lockout de login por e-mail+IP (#8; nil-safe) e a
	// mesma trilha de auditoria dos subdomínios (#9).
	if _, err := aplicacaoauth.New(aplicacaoauth.Dependencias{
		Usuarios:     usuariosAuth{},
		Emissor:      emissorToken{},
		Organizacoes: resolvedorOrganizacao{},
		Vitalidade:   vitalidadeOrganizacao{},
		Limite:       limitadorLoginAuth{},
		Trilha:       trilhasLog.auditoria,
	}); err != nil {
		return fmt.Errorf("boot: %w", err)
	}
	slog.Info("[BOOTSTRAP-DI] Contêiner Identidade/Auth inicializado.")

	// Aplicação logs (E5) — leitura das trilhas do ClickHouse com recorte em
	// 3 níveis; o adaptador resolve o consultor NA CHAMADA, então ClickHouse
	// degradado não impede o boot (as rotas respondem 503 padronizado).
	if _, err := aplicacaologs.New(aplicacaologs.Dependencias{
		Trilhas: consultorLogs{},
	}); err != nil {
		return fmt.Errorf("boot: %w", err)
	}
	slog.Info("[BOOTSTRAP-DI] Contêiner Identidade/Logs inicializado.")

	// Aplicação catalogo — DEPOIS de todos os subdomínios: os agregadores
	// acima só enxergam os Catalogo()/CatalogoEventos() dos pacotes já
	// importados e bootados.
	if _, err := aplicacaocatalogo.New(aplicacaocatalogo.Dependencias{
		Permissoes: novoAgregadorPermissoes(),
		Eventos:    novoAgregadorEventos(),
	}); err != nil {
		return fmt.Errorf("boot: %w", err)
	}
	slog.Info("[BOOTSTRAP-DI] Contêiner Identidade/Catalogo inicializado.")

	// 8. HTTP — sondas, trilha de acesso e provedor de domínios custom
	// injetados como funções/destinos; o servidor drena requisições em voo
	// antes do fechamento LIFO.
	engine, err := routes.Montar(routes.Opcoes{
		App:            cfg.App,
		Cors:           cfg.Server.HTTP.Cors,
		TrustedProxies: cfg.Server.HTTP.TrustedProxy,
		SondaBanco:     postgres.Ping,
		DominiosCustom: dominiosCustomParaCors,
		AcessoLog:      trilhasLog.acesso,
	})
	if err != nil {
		return fmt.Errorf("boot: %w", err)
	}
	readTimeout, writeTimeout, idleTimeout := cfg.Server.HTTP.Timeouts()
	servidor := server.Novo(engine, server.Opcoes{
		Porta:           cfg.Server.HTTP.Port,
		ReadTimeout:     readTimeout,
		WriteTimeout:    writeTimeout,
		IdleTimeout:     idleTimeout,
		ShutdownTimeout: cfg.Server.HTTP.ShutdownTimeout(),
	})

	ctxSinal, pararSinais := signal.NotifyContext(ctxBoot, syscall.SIGINT, syscall.SIGTERM)
	defer pararSinais()

	errServe := make(chan error, 1)
	go func() { errServe <- servidor.Servir() }()
	slog.Info("[BOOTSTRAP] processo pronto", "porta", cfg.Server.HTTP.Port)

	select {
	case err := <-errServe:
		return err
	case <-ctxSinal.Done():
		slog.Info("[BOOTSTRAP] sinal de encerramento recebido, drenando")
		if err := servidor.Parar(context.Background()); err != nil {
			return fmt.Errorf("boot: %w", err)
		}
	}
	return nil
}

// --- Operações de CLI (migrate/seed) ---------------------------------------

type operacao struct {
	cfg         *config.Config
	fonte       migrations.FonteConexao
	fechamentos *pilhaFechamento
}

// abrirOperacao monta o mínimo para uma operação de CLI; comBanco=false
// serve ao validate, que roda SEM conexão.
func abrirOperacao(caminhoConfig string, comBanco bool) (*operacao, error) {
	if err := config.Init(caminhoConfig); err != nil {
		return nil, fmt.Errorf("cli: %w", err)
	}
	op := &operacao{cfg: config.MustUse(), fechamentos: &pilhaFechamento{}}
	if !comBanco {
		return op, nil
	}
	ctxBoot := context.Background()
	if _, err := postgres.InitPostgres(ctxBoot); err != nil {
		op.fechamentos.executar()
		return nil, fmt.Errorf("cli: %w", err)
	}
	op.fechamentos.empilhar(func() { _ = postgres.Close() })
	op.fonte = fonteBanco{}
	return op, nil
}

func (op *operacao) fechar() { op.fechamentos.executar() }

func timeoutsMigracao(cfg *config.Config) migrations.TimeoutsMigracao {
	return migrations.TimeoutsMigracao{
		LockTimeout:      time.Duration(cfg.Databases.Migrations.LockTimeoutSec) * time.Second,
		StatementTimeout: time.Duration(cfg.Databases.Migrations.StatementTimeoutMin) * time.Minute,
	}
}

// MigrateUp aplica as pendentes.
func MigrateUp(caminhoConfig string) (int, error) {
	op, err := abrirOperacao(caminhoConfig, true)
	if err != nil {
		return 0, err
	}
	defer op.fechar()
	return migrations.Subir(context.Background(), op.fonte, op.cfg.Databases.Migrations.Path, timeoutsMigracao(op.cfg))
}

// MigrateDown reverte as últimas N (rollback manual e explícito).
func MigrateDown(caminhoConfig string, n int) error {
	op, err := abrirOperacao(caminhoConfig, true)
	if err != nil {
		return err
	}
	defer op.fechar()
	return migrations.Descer(op.fonte, op.cfg.Databases.Migrations.Path, n, timeoutsMigracao(op.cfg))
}

// MigrateGoto sobe/desce até a versão V.
func MigrateGoto(caminhoConfig string, versao uint) error {
	op, err := abrirOperacao(caminhoConfig, true)
	if err != nil {
		return err
	}
	defer op.fechar()
	return migrations.IrPara(op.fonte, op.cfg.Databases.Migrations.Path, versao, timeoutsMigracao(op.cfg))
}

// MigrateForce marca a versão manualmente (recuperação de estado dirty).
func MigrateForce(caminhoConfig string, versao int) error {
	op, err := abrirOperacao(caminhoConfig, true)
	if err != nil {
		return err
	}
	defer op.fechar()
	return migrations.Forcar(op.fonte, op.cfg.Databases.Migrations.Path, versao, timeoutsMigracao(op.cfg))
}

// MigrateStatus devolve o estado completo das migrations.
func MigrateStatus(caminhoConfig string) (*migrations.Estado, error) {
	op, err := abrirOperacao(caminhoConfig, true)
	if err != nil {
		return nil, err
	}
	defer op.fechar()
	estado, err := migrations.EstadoAtual(op.fonte, op.cfg.Databases.Migrations.Path, timeoutsMigracao(op.cfg))
	if err != nil {
		return nil, err
	}
	return &estado, nil
}

// MigrateValidate confere pares/sequência/SQL SEM conexão — gate rápido de CI.
func MigrateValidate(caminhoConfig string) error {
	op, err := abrirOperacao(caminhoConfig, false)
	if err != nil {
		return err
	}
	defer op.fechar()
	return migrations.Validar(op.cfg.Databases.Migrations.Path)
}

// MigrateCreate gera o próximo par numerado já no padrão de nome.
func MigrateCreate(caminhoConfig, descricao string) (string, string, error) {
	op, err := abrirOperacao(caminhoConfig, false)
	if err != nil {
		return "", "", err
	}
	defer op.fechar()
	return migrations.Create(op.cfg.Databases.Migrations.Path, descricao)
}

// Seed roda dados mínimos idempotentes — NUNCA automático no boot. Os 5
// papéis globais da plataforma e suas permissões estão em seed.go.
// (implementação movida para seed.go na F1)

// --- Adaptadores ------------------------------------------------------------

// fonteBanco adapta o postgres ao contrato do runner de migrations. A
// dependência entre os dois infra entra por interface declarada no
// consumidor e é ligada SÓ aqui (regra 2 de agents/01).
type fonteBanco struct{}

func (fonteBanco) SQLDB() (*sql.DB, error) {
	db, err := postgres.GetDB()
	if err != nil {
		return nil, err
	}
	return db.DB()
}

func (fonteBanco) NomeDatabase() string {
	cfg, err := config.Use()
	if err != nil {
		return ""
	}
	return cfg.Databases.Postgres.Name
}

// SessaoDedicada abre um pool NOVO de 1 conexão para a sessão de migração —
// o pool gorm do negócio nunca recebe SET nem DDL, e a conexão da migração
// morre com ela (fechada pelo runner ao fim de cada operação).
func (fonteBanco) SessaoDedicada() (*sql.DB, error) {
	cfg, err := config.Use()
	if err != nil {
		return nil, fmt.Errorf("migrations: config ausente para sessão dedicada: %w", err)
	}
	sessao, err := postgres.AbrirPoolSQL(context.Background(), cfg.Databases.Postgres)
	if err != nil {
		return nil, err
	}
	sessao.SetMaxOpenConns(1)
	sessao.SetMaxIdleConns(1)
	return sessao, nil
}

// pilhaFenchamento guarda os Close na ordem de subida e desce na INVERSA.
type pilhaFechamento struct{ itens []func() }

func (p *pilhaFechamento) empilhar(fn func()) { p.itens = append(p.itens, fn) }

func (p *pilhaFechamento) executar() {
	for i := len(p.itens) - 1; i >= 0; i-- {
		p.itens[i]()
	}
	p.itens = nil
}
