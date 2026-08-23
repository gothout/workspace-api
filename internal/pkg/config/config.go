// Package config carrega a configuração do processo a partir de um arquivo
// JSON (modelo versionado em configs_example.json) e a expõe ao restante do
// processo pelo par Init (boot) / Use / MustUse.
//
// Regras: segredo nunca é logado nem devolvido em erro; config inválida
// falha no Init com mensagem acionável — não existe valor padrão silencioso
// para chave de segurança.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/spf13/viper"
)

var (
	instance *Config
	once     sync.Once
	initErr  error

	ErrNaoInicializada = errors.New("config não inicializada: chame config.Init(path) no boot")
)

// Config é a raiz da configuração do processo — espelha o modelo do doc 02.
type Config struct {
	App       AppConfig       `mapstructure:"app"`
	Server    ServerConfig    `mapstructure:"server"`
	Security  SecurityConfig  `mapstructure:"security"`
	Databases DatabasesConfig `mapstructure:"databases"`
	Cache     CacheConfig     `mapstructure:"cache"`
	Logs      LogsConfig      `mapstructure:"logs"`
}

type AppConfig struct {
	Name       string `mapstructure:"name"`
	Env        string `mapstructure:"env"`
	Version    string `mapstructure:"version"`
	BaseDomain string `mapstructure:"base_domain"`
}

type ServerConfig struct {
	HTTP HTTPConfig `mapstructure:"http"`
}

type HTTPConfig struct {
	Port               int        `mapstructure:"port"`
	ReadTimeoutSec     int        `mapstructure:"read_timeout_sec"`
	WriteTimeoutSec    int        `mapstructure:"write_timeout_sec"`
	IdleTimeoutSec     int        `mapstructure:"idle_timeout_sec"`
	ShutdownTimeoutSec int        `mapstructure:"shutdown_timeout_sec"`
	TrustedProxy       []string   `mapstructure:"trusted_proxy"`
	Cors               CorsConfig `mapstructure:"cors"`
}

type CorsConfig struct {
	AllowedOrigins []string `mapstructure:"allowed_origins"`
}

type SecurityConfig struct {
	JwtSecret          string `mapstructure:"jwt_secret"`
	JwtTtlMin          int    `mapstructure:"jwt_ttl_min"`
	JwtRefreshTtlHours int    `mapstructure:"jwt_refresh_ttl_hours"`
}

type DatabasesConfig struct {
	Postgres   PostgresConfig   `mapstructure:"postgres"`
	Migrations MigrationsConfig `mapstructure:"migrations"`
	Redis      RedisConfig      `mapstructure:"redis"`
	ClickHouse ClickHouseConfig `mapstructure:"clickhouse"`
}

// ClickHouseConfig é a conexão do banco de logs assíncronos (evolução #9) —
// dependência DEGRADÁVEL como o Redis: enabled=false ou servidor inacessível
// no boot deixam o processo subir sem ele (log [DEGRADADO]) e as trilhas caem
// para o stdout. Nunca derruba o boot.
type ClickHouseConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Host    string `mapstructure:"host"`
	Port    int    `mapstructure:"port"` // protocolo NATIVO (9000), não HTTP
	User    string `mapstructure:"user"`
	Pass    string `mapstructure:"pass"` // nunca logada nem em erro
	Database string `mapstructure:"database"`
}

func (c ClickHouseConfig) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// LogsConfig agrega os parâmetros dos writers em lote das trilhas de log
// (evolução #9). Tudo opcional: zero vira default em validar() — fila limitada
// é obrigatória por desenho (memória finita; fila cheia descarta e conta).
type LogsConfig struct {
	LoteTamanho     int `mapstructure:"lote_tamanho"`
	LoteJanelaMs    int `mapstructure:"lote_janela_ms"`
	FilaTamanho     int `mapstructure:"fila_tamanho"`
	DrainTimeoutSec int `mapstructure:"drain_timeout_sec"`
	// AlertaJanelaSeg é a janela de agregação do sink de alerta da evolução
	// errobserve (#10): críticos repetidos do mesmo código viram UM [ALERTA]
	// por janela. Zero vira 60s em validar().
	AlertaJanelaSeg int `mapstructure:"alerta_janela_seg"`
}

// RedisConfig é a conexão do cache/lockout distribuído — dependência
// DEGRADÁVEL: enabled=false ou servidor inacessível no boot deixam o
// processo subir sem Redis (log [DEGRADADO]); os consumidores tratam a
// ausência (sem cache, sem lockout) e nada derruba o processo.
type RedisConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Host    string `mapstructure:"host"`
	Port    int    `mapstructure:"port"`
	Pass    string `mapstructure:"pass"` // nunca logada nem em erro
	DB      int    `mapstructure:"db"`
}

func (r RedisConfig) Addr() string {
	return fmt.Sprintf("%s:%d", r.Host, r.Port)
}

// CacheConfig agrega os parâmetros dos caches de leitura e do lockout de
// login. Tudo opcional: zero vira default em validar() — TTL curto é
// obrigatório por desenho (agents/02), nunca configurável para "eterno".
type CacheConfig struct {
	TtlResolucaoSeg  int             `mapstructure:"ttl_resolucao_seg"`
	TtlPermissoesSeg int             `mapstructure:"ttl_permissoes_seg"`
	LoginLockout     LoginLockConfig `mapstructure:"login_lockout"`
}

type LoginLockConfig struct {
	MaxTentativas int `mapstructure:"max_tentativas"`
	JanelaSeg     int `mapstructure:"janela_seg"`
	BloqueioSeg   int `mapstructure:"bloqueio_seg"`
}

type PostgresConfig struct {
	Host    string     `mapstructure:"host"`
	Port    int        `mapstructure:"port"`
	User    string     `mapstructure:"user"`
	Pass    string     `mapstructure:"pass"`
	Name    string     `mapstructure:"name"`
	SslMode string     `mapstructure:"ssl_mode"`
	Pool    PoolConfig `mapstructure:"pool"`
}

type PoolConfig struct {
	MaxOpenConns       int `mapstructure:"max_open_conns"`
	MaxIdleConns       int `mapstructure:"max_idle_conns"`
	ConnMaxLifetimeMin int `mapstructure:"conn_max_lifetime_min"`
	ConnMaxIdleTimeMin int `mapstructure:"conn_max_idle_time_min"`
}

func (p PostgresConfig) DSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s&TimeZone=UTC",
		p.User, p.Pass, p.Host, p.Port, p.Name, p.SslMode)
}

type MigrationsConfig struct {
	Path                string `mapstructure:"path"`
	AutoRun             bool   `mapstructure:"auto_run"`
	LockTimeoutSec      int    `mapstructure:"lock_timeout_sec"`
	StatementTimeoutMin int    `mapstructure:"statement_timeout_min"`
}

// Timeouts devolve os timeouts do servidor HTTP em durações prontas para uso.
func (h HTTPConfig) Timeouts() (read, write, idle time.Duration) {
	return time.Duration(h.ReadTimeoutSec) * time.Second,
		time.Duration(h.WriteTimeoutSec) * time.Second,
		time.Duration(h.IdleTimeoutSec) * time.Second
}

// ShutdownTimeout devolve o timeout de drenagem do shutdown.
func (h HTTPConfig) ShutdownTimeout() time.Duration {
	return time.Duration(h.ShutdownTimeoutSec) * time.Second
}

// Init lê o arquivo em path e guarda a configuração do processo. É chamado
// UMA vez no boot, antes de qualquer infra; erro de leitura/validação é fatal
// para quem chama (bootstrap decide).
func Init(path string) error {
	once.Do(func() {
		cfg, err := ler(path)
		if err != nil {
			initErr = err
			return
		}
		if err := cfg.validar(); err != nil {
			initErr = err
			return
		}
		instance = cfg
	})
	return initErr
}

func ler(path string) (*Config, error) {
	if path == "" {
		return nil, errors.New("caminho do arquivo de configuração vazio: informe --config")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("falha ao ler configuração %q: %w", path, err)
	}
	v := viper.New()
	v.SetConfigType("json")
	if err := v.ReadConfig(bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("falha ao interpretar configuração %q: %w", path, err)
	}
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("falha ao mapear configuração %q: %w", path, err)
	}
	return &cfg, nil
}

// validar confere chaves obrigatórias e valores mínimos — mensagem cita o
// campo e o motivo, NUNCA o valor de segredos.
func (c *Config) validar() error {
	if c.App.Name == "" {
		return errors.New("configuração inválida: app.name é obrigatório")
	}
	if c.App.BaseDomain == "" {
		return errors.New("configuração inválida: app.base_domain é obrigatório")
	}
	if c.Server.HTTP.Port <= 0 || c.Server.HTTP.Port > 65535 {
		return errors.New("configuração inválida: server.http.port deve estar entre 1 e 65535")
	}
	if c.Server.HTTP.ShutdownTimeoutSec <= 0 {
		return errors.New("configuração inválida: server.http.shutdown_timeout_sec deve ser maior que zero")
	}
	segredoExemplo := c.Security.JwtSecret == "trocar-em-producao-por-um-segredo-de-32-bytes"
	if c.Security.JwtSecret == "" || (segredoExemplo && c.App.Env == "producao") {
		return errors.New("configuração inválida: security.jwt_secret é obrigatório e não pode ser o valor de exemplo em produção")
	}
	if c.Security.JwtTtlMin <= 0 {
		return errors.New("configuração inválida: security.jwt_ttl_min deve ser maior que zero")
	}
	if c.Databases.Postgres.Host == "" {
		return errors.New("configuração inválida: databases.postgres.host é obrigatório")
	}
	if c.Databases.Postgres.Port <= 0 {
		return errors.New("configuração inválida: databases.postgres.port é obrigatório")
	}
	if c.Databases.Postgres.Name == "" {
		return errors.New("configuração inválida: databases.postgres.name é obrigatório")
	}
	if c.Databases.Migrations.Path == "" {
		return errors.New("configuração inválida: databases.migrations.path é obrigatório")
	}
	if c.Databases.Migrations.LockTimeoutSec <= 0 {
		c.Databases.Migrations.LockTimeoutSec = 5
	}
	if c.Databases.Migrations.StatementTimeoutMin <= 0 {
		c.Databases.Migrations.StatementTimeoutMin = 10
	}
	if c.Databases.Redis.Enabled && (c.Databases.Redis.Host == "" || c.Databases.Redis.Port <= 0 || c.Databases.Redis.Port > 65535) {
		return errors.New("configuração inválida: databases.redis.enabled=true exige host e port (1–65535)")
	}
	if c.Databases.ClickHouse.Enabled && (c.Databases.ClickHouse.Host == "" || c.Databases.ClickHouse.Port <= 0 || c.Databases.ClickHouse.Port > 65535) {
		return errors.New("configuração inválida: databases.clickhouse.enabled=true exige host e port (1–65535)")
	}
	if c.Databases.ClickHouse.User == "" {
		c.Databases.ClickHouse.User = "default"
	}
	if c.Databases.ClickHouse.Database == "" {
		c.Databases.ClickHouse.Database = "workspace_logs"
	}
	if c.Logs.LoteTamanho <= 0 {
		c.Logs.LoteTamanho = 500
	}
	if c.Logs.LoteJanelaMs <= 0 {
		c.Logs.LoteJanelaMs = 2000
	}
	if c.Logs.FilaTamanho <= 0 {
		c.Logs.FilaTamanho = 10000
	}
	if c.Logs.DrainTimeoutSec <= 0 {
		c.Logs.DrainTimeoutSec = 5
	}
	if c.Logs.AlertaJanelaSeg <= 0 {
		c.Logs.AlertaJanelaSeg = 60
	}
	if c.Cache.TtlResolucaoSeg <= 0 {
		c.Cache.TtlResolucaoSeg = 30
	}
	if c.Cache.TtlPermissoesSeg <= 0 {
		c.Cache.TtlPermissoesSeg = 60
	}
	if c.Cache.LoginLockout.MaxTentativas <= 0 {
		c.Cache.LoginLockout.MaxTentativas = 5
	}
	if c.Cache.LoginLockout.JanelaSeg <= 0 {
		c.Cache.LoginLockout.JanelaSeg = 300
	}
	if c.Cache.LoginLockout.BloqueioSeg <= 0 {
		c.Cache.LoginLockout.BloqueioSeg = 900
	}
	return nil
}

// Use devolve a configuração carregada; erro se o boot ainda não rodou Init.
func Use() (*Config, error) {
	if instance == nil {
		return nil, ErrNaoInicializada
	}
	return instance, nil
}

// MustUse devolve a configuração e entra em pânico se não inicializada —
// restrito ao bootstrap (panic fora do boot é proibido).
func MustUse() *Config {
	if instance == nil {
		panic(ErrNaoInicializada)
	}
	return instance
}

// ResetarParaTeste restaura o estado do singleton — uso EXCLUSIVO dos testes
// (pacote config e consumidores que exercem o boot); nunca chamado pelo
// processo real.
func ResetarParaTeste() {
	instance = nil
	initErr = nil
	once = sync.Once{}
}
