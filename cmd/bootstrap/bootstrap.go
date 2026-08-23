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

	"workspace-api/cmd/server"
	"workspace-api/cmd/server/routes"
	"workspace-api/internal/infra/database/migrations"
	"workspace-api/internal/infra/database/postgres"
	"workspace-api/internal/infra/jwt"
	"workspace-api/internal/pkg/config"
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

	// 5. Migrations — `up` automático quando auto_run (advisory lock impede
	// réplicas de migrar juntas); rollback NUNCA é automático.
	if cfg.Databases.Migrations.AutoRun {
		aplicadas, err := migrations.Subir(ctxBoot, fonteBanco{}, cfg.Databases.Migrations.Path, timeoutsMigracao(cfg))
		if err != nil {
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

	// Revogação persistida do refresh (F4): o validador do JWT passa a
	// conferir identidade_user_refresh_token via adaptador que resolve NA
	// CHAMADA — a denylist Redis da evolução será só cache desta verdade.
	gerenciadorJWT.DefinirRevogador(revogadorRefresh{})

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
	_, err = dominioOrganizacao.New(db, suspendedorWorkspaces{}, encerradorSessoesUsuario{})
	if err != nil {
		return fmt.Errorf("boot: %w", err)
	}
	slog.Info("[BOOTSTRAP-DI] Contêiner Identidade/Organization inicializado.")

	// Cache de resolução por slug: contrato CacheResolucao do subdomínio;
	// implementação Redis é evolução (#8) — nil é operação normal.
	_, err = dominioWorkspace.New(db, nil)
	if err != nil {
		return fmt.Errorf("boot: %w", err)
	}
	slog.Info("[BOOTSTRAP-DI] Contêiner Identidade/Workspace inicializado.")

	_, err = dominioUsuario.New(db, validadorWorkspaces{})
	if err != nil {
		return fmt.Errorf("boot: %w", err)
	}
	slog.Info("[BOOTSTRAP-DI] Contêiner Identidade/User inicializado.")

	// Aplicações — orquestrações que cruzam os subdomínios acima. O auth
	// recebe os contratos ligados por adaptadores que resolvem os singletons
	// NA CHAMADA (usuario.go) — inclusive a vitalidade da organization dona
	// da sessão (R4: refresh falha fechado com dona inativa/removida).
	if _, err := aplicacaoauth.New(aplicacaoauth.Dependencias{
		Usuarios:     usuariosAuth{},
		Emissor:      emissorToken{},
		Organizacoes: resolvedorOrganizacao{},
		Vitalidade:   vitalidadeOrganizacao{},
	}); err != nil {
		return fmt.Errorf("boot: %w", err)
	}
	slog.Info("[BOOTSTRAP-DI] Contêiner Identidade/Auth inicializado.")

	// Aplicação catalogo — DEPOIS de todos os subdomínios: o agregador
	// acima só enxerga os Catalogo() dos pacotes já importados e bootados.
	if _, err := aplicacaocatalogo.New(aplicacaocatalogo.Dependencias{
		Permissoes: novoAgregadorPermissoes(),
	}); err != nil {
		return fmt.Errorf("boot: %w", err)
	}
	slog.Info("[BOOTSTRAP-DI] Contêiner Identidade/Catalogo inicializado.")

	// 8. HTTP — sondas e provedor de domínios custom injetados como funções;
	// o servidor drena requisições em voo antes do fechamento LIFO.
	engine := routes.Montar(routes.Opcoes{
		App:            cfg.App,
		Cors:           cfg.Server.HTTP.Cors,
		SondaBanco:     postgres.Ping,
		DominiosCustom: dominiosCustomParaCors,
	})
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
