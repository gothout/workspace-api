package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	pgxmig "github.com/golang-migrate/migrate/v4/database/pgx/v5"
)

// FonteConexao é o contrato estreito do runner com o banco — implementado
// pelo cmd/bootstrap sobre o pool do Postgres; este pacote nunca importa
// outro infra (regra 2 de agents/01).
type FonteConexao interface {
	SQLDB() (*sql.DB, error)
	NomeDatabase() string
}

// TimeoutsMigracao são os limites curtos exigidos pelo doc 02: falha rápida
// em vez de lock eterno em produção.
type TimeoutsMigracao struct {
	LockTimeout      time.Duration // default 5s
	StatementTimeout time.Duration // default 10min
}

func (t TimeoutsMigracao) comDefaults() TimeoutsMigracao {
	if t.LockTimeout <= 0 {
		t.LockTimeout = 5 * time.Second
	}
	if t.StatementTimeout <= 0 {
		t.StatementTimeout = 10 * time.Minute
	}
	return t
}

// LinhaStatus é uma linha do relatório de status.
type LinhaStatus struct {
	Versao   uint   `json:"versao"`
	Nome     string `json:"nome"`
	Situacao string `json:"situacao"` // aplicada | pendente | pendente-manual
}

// Estado é o retrato completo das migrations: versão corrente + situação.
type Estado struct {
	VersaoAtual uint
	Dirty       bool
	Linhas      []LinhaStatus `json:"linhas"`
	Pendentes   int           `json:"pendentes"`
}

// novaInstancia monta o par fonte+driver sobre a conexão, com os timeouts
// curtos da sessão de migração e advisory lock do próprio driver do banco
// (pg_advisory_lock — quem perde espera e reconfere a versão antes de rodar).
func novaInstancia(fonte FonteConexao, dir string, timeouts TimeoutsMigracao) (*migrate.Migrate, error) {
	sqlDB, err := fonte.SQLDB()
	if err != nil {
		return nil, fmt.Errorf("migrations: conexão indisponível: %w", err)
	}
	pares, err := listarPares(dir)
	if err != nil {
		return nil, err
	}
	fonteDriver := novoDriverFonte(pares)

	limites := timeouts.comDefaults()
	driverBanco, err := pgxmig.WithInstance(sqlDB, &pgxmig.Config{
		MigrationsTable:  "schema_migrations",
		DatabaseName:     fonte.NomeDatabase(),
		StatementTimeout: limites.StatementTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("migrations: falha ao preparar driver do banco: %w", err)
	}
	instancia, err := migrate.NewWithInstance("fs", fonteDriver, fonte.NomeDatabase(), driverBanco)
	if err != nil {
		return nil, fmt.Errorf("migrations: falha ao montar runner: %w", err)
	}
	// lock_timeout curto na sessão: DDL travado por outra operação falha rápido.
	if _, err := sqlDB.Exec(fmt.Sprintf("SET lock_timeout = '%s'", limites.LockTimeout)); err != nil {
		return nil, fmt.Errorf("migrations: falha ao aplicar lock_timeout: %w", err)
	}
	return instancia, nil
}

// Subir aplica todas as pendentes (exceto manuais) — usado no boot quando
// auto_run ligado e no CLI `migrate up`. Devolve quantas foram aplicadas.
func Subir(ctx context.Context, fonte FonteConexao, dir string, timeouts TimeoutsMigracao) (int, error) {
	pares, err := listarPares(dir)
	if err != nil {
		return 0, err
	}
	if len(novoDriverFonte(pares).pares) == 0 {
		slog.InfoContext(ctx, "migrations.up: nenhuma migration executável no diretório", "dir", dir)
		return 0, nil
	}
	instancia, err := novaInstancia(fonte, dir, timeouts)
	if err != nil {
		return 0, err
	}
	antes := estadoVersao(instancia)
	if err := instancia.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return 0, traduzirErroMigrate(err)
	}
	depois := estadoVersao(instancia)
	aplicadas := 0
	if depois > antes {
		aplicadas = int(depois - antes)
	}
	slog.InfoContext(ctx, "migrations.up concluído",
		"aplicadas", aplicadas, "versao_atual", depois)
	return aplicadas, nil
}

// Descer reverte as últimas N migrations — rollback manual e explícito via
// CLI; NUNCA chamado pelo boot.
func Descer(fonte FonteConexao, dir string, n int, timeouts TimeoutsMigracao) error {
	if n <= 0 {
		return errors.New("migrations: down exige N maior que zero")
	}
	pares, err := listarPares(dir)
	if err != nil {
		return err
	}
	if len(novoDriverFonte(pares).pares) == 0 {
		return errors.New("migrations: nenhuma migration executável para reverter")
	}
	instancia, err := novaInstancia(fonte, dir, timeouts)
	if err != nil {
		return err
	}
	if err := instancia.Steps(-n); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return traduzirErroMigrate(err)
	}
	slog.Info("migrations.down concluído", "revertidas", n)
	return nil
}

// IrPara sobe ou desce até a versão V — CLI `migrate goto`.
func IrPara(fonte FonteConexao, dir string, versao uint, timeouts TimeoutsMigracao) error {
	instancia, err := novaInstancia(fonte, dir, timeouts)
	if err != nil {
		return err
	}
	if err := instancia.Migrate(versao); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return traduzirErroMigrate(err)
	}
	slog.Info("migrations.goto concluído", "versao", versao)
	return nil
}

// Forcar marca a versão manualmente sem executar SQL — recuperação de estado
// dirty; CLI `migrate force`.
func Forcar(fonte FonteConexao, dir string, versao int, timeouts TimeoutsMigracao) error {
	instancia, err := novaInstancia(fonte, dir, timeouts)
	if err != nil {
		return err
	}
	if err := instancia.Force(versao); err != nil {
		return traduzirErroMigrate(err)
	}
	slog.Info("migrations.force concluído", "versao", versao)
	return nil
}

// EstadoAtual devolve versão corrente, flag dirty e cada migration com sua
// situação (aplicada / pendente / pendente-manual).
func EstadoAtual(fonte FonteConexao, dir string, timeouts TimeoutsMigracao) (Estado, error) {
	sqlDB, err := fonte.SQLDB()
	if err != nil {
		return Estado{}, fmt.Errorf("migrations: conexão indisponível: %w", err)
	}
	pares, err := listarPares(dir)
	if err != nil {
		return Estado{}, err
	}
	versao, dirty, err := lerVersaoAtual(sqlDB)
	if err != nil {
		return Estado{}, err
	}
	estado := Estado{VersaoAtual: versao, Dirty: dirty}
	for _, p := range pares {
		situacao := "pendente"
		switch {
		case p.manual && p.versao > versao:
			situacao = "pendente-manual"
		case p.versao <= versao:
			situacao = "aplicada"
		}
		estado.Linhas = append(estado.Linhas, LinhaStatus{Versao: p.versao, Nome: p.nome, Situacao: situacao})
		if situacao == "pendente" || situacao == "pendente-manual" {
			estado.Pendentes++
		}
	}
	return estado, nil
}

// lerVersaoAtual consulta schema_migrations direto; tabela ausente = versão 0.
func lerVersaoAtual(sqlDB *sql.DB) (uint, bool, error) {
	var (
		versao uint
		dirty  bool
	)
	err := sqlDB.QueryRow("SELECT version, dirty FROM schema_migrations").Scan(&versao, &dirty)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return 0, false, nil
	case err != nil:
		// relação não existe ainda: banco virgem
		if strings.Contains(err.Error(), "does not exist") || strings.Contains(err.Error(), "não existe") {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("migrations: falha ao consultar versão atual: %w", err)
	}
	return versao, dirty, nil
}

// estadoVersao devolve a versão lida pelo próprio migrador (0 se vazia).
func estadoVersao(instancia *migrate.Migrate) uint {
	v, _, _ := instancia.Version()
	return v
}

func traduzirErroMigrate(err error) error {
	var dirty migrate.ErrDirty
	if errors.As(err, &dirty) {
		return fmt.Errorf("migrations: banco em estado DIRTY na versão %d — corrija o esquema e use `migrate force` para retomar: %w", dirty.Version, err)
	}
	return fmt.Errorf("migrations: %w", err)
}
