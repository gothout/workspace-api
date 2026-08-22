// Package postgres conecta a API ao Postgres via gorm/pgx. Postgres é
// dependência FATAL: erro no boot derruba o processo (decisão do bootstrap).
//
// Par função pura + singleton: Connect(cfg) é puro e testável; InitPostgres/
// GetDB/Close são o singleton do processo com sync.Once.
package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"workspace-api/internal/pkg/config"
)

var (
	instance *gorm.DB
	once     sync.Once
	initErr  error

	ErrNaoInicializado = fmt.Errorf("postgres não inicializado: chame InitPostgres no boot")
)

// Connect é a função PURA de conexão: recebe config, devolve o handle gorm
// com pool configurado, timestamps em UTC e ping inicial. Sem estado global.
func Connect(ctx context.Context, cfg config.PostgresConfig) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: cfg.DSN()}), &gorm.Config{
		Logger:  logger.Default.LogMode(logger.Warn),
		NowFunc: func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		return nil, fmt.Errorf("postgres: falha ao conectar em %s:%d/%s (%s): %s",
			cfg.Host, cfg.Port, cfg.Name, cfg.SslMode, sanitizar(err.Error(), cfg.Pass))
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("postgres: falha ao obter pool de conexões: %w", err)
	}
	aplicarPool(sqlDB, cfg.Pool)
	if cfg.Pool.MaxOpenConns > 0 || cfg.Pool.MaxIdleConns > 0 {
		slog.Info("postgres: pool configurado",
			"max_open", cfg.Pool.MaxOpenConns, "max_idle", cfg.Pool.MaxIdleConns,
			"lifetime_min", cfg.Pool.ConnMaxLifetimeMin, "idle_time_min", cfg.Pool.ConnMaxIdleTimeMin)
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("postgres: banco em %s:%d/%s não respondeu ao ping: %s",
			cfg.Host, cfg.Port, cfg.Name, sanitizar(err.Error(), cfg.Pass))
	}
	return db, nil
}

// aplicarPool configura limites de conexão — defaults do driver viram gargalo.
func aplicarPool(sqlDB *sql.DB, pool config.PoolConfig) {
	if pool.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(pool.MaxOpenConns)
	}
	if pool.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(pool.MaxIdleConns)
	}
	if pool.ConnMaxLifetimeMin > 0 {
		sqlDB.SetConnMaxLifetime(time.Duration(pool.ConnMaxLifetimeMin) * time.Minute)
	}
	if pool.ConnMaxIdleTimeMin > 0 {
		sqlDB.SetConnMaxIdleTime(time.Duration(pool.ConnMaxIdleTimeMin) * time.Minute)
	}
}

// InitPostgres conecta e guarda o singleton do processo. Erro aqui é fatal:
// o bootstrap encerra o processo com a mensagem devolvida.
func InitPostgres(ctx context.Context) (*gorm.DB, error) {
	once.Do(func() {
		cfg, err := config.Use()
		if err != nil {
			initErr = fmt.Errorf("postgres: config ausente no boot: %w", err)
			return
		}
		db, err := Connect(ctx, cfg.Databases.Postgres)
		if err != nil {
			initErr = err
			return
		}
		instance = db
	})
	return instance, initErr
}

// GetDB devolve o handle do processo; erro se o boot ainda não rodou — nunca
// reconecta silenciosamente.
func GetDB() (*gorm.DB, error) {
	if instance == nil {
		return nil, ErrNaoInicializado
	}
	return instance, nil
}

// Ping é a sonda de saúde injetada no servidor pelo bootstrap para o
// /api/status (este pacote não conhece HTTP).
func Ping(ctx context.Context) error {
	if instance == nil {
		return ErrNaoInicializado
	}
	sqlDB, err := instance.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

// Close encerra o pool e invalida o singleton — chamada pelo fechamento
// LIFO do bootstrap; depois dela GetDB/Ping voltam a falhar explicitamente.
func Close() error {
	if instance == nil {
		return nil
	}
	sqlDB, err := instance.DB()
	if err != nil {
		instance = nil
		return err
	}
	instance = nil
	return sqlDB.Close()
}

// sanitizar remove credenciais de mensagens de erro antes de qualquer log.
func sanitizar(msg, segredo string) string {
	if segredo == "" {
		return msg
	}
	return strings.ReplaceAll(msg, segredo, "***")
}
