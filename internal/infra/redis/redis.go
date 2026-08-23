// Package redis é o adaptador da dependência DEGRADÁVEL de cache/lockout
// distribuído (evolução da issue #8): cache da resolução de workspace por
// slug, cache de permissões efetivas, denylist do JWT e rate-limit/lockout
// de login.
//
// Regra que define o desenho (agents/02): Redis NUNCA derruba o processo.
// Connect não erra — devolve o cliente quando o servidor responde, ou
// CLIENTE NIL com log [DEGRADADO] quando desabilitado/inacessível; o
// consumidor é obrigado a tratar a ausência (sem cache = consulta direta à
// fonte; sem lockout = fluxo atual). A fonte da verdade continua sempre no
// Postgres — nada aqui guarda dado que não exista lá.
//
// Prefixos de chave fixos: ver chaves.go. Par função pura + singleton:
// Connect puro e testável; InitRedis/Get/Close com sync.Once (mutex cobre o
// once.Do inteiro — lição R7 sobre ResetarParaTeste disputando o Once).
package redis

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"workspace-api/internal/pkg/config"
)

// timeoutConexao limita dial/read/write: Redis lento não pode contaminar o
// caminho de request além do previsto.
const timeoutConexao = 3 * time.Second

var (
	mutex              sync.RWMutex
	once               sync.Once
	instance           *goredis.Client
	iniciado           bool
	initErr            error
	ErrNaoInicializado = errors.New("redis não inicializado: chame InitRedis no boot")
)

// Connect é a função PURA de montagem: NUNCA devolve erro. Config
// desabilitada ou servidor inacessível no boot = cliente NIL com log
// [DEGRADADO]; os testes unitários usam só ele, sem singleton.
func Connect(cfg config.RedisConfig) *goredis.Client {
	if !cfg.Enabled {
		slog.Info("[DEGRADADO] redis desabilitado na config (databases.redis.enabled=false) — sem cache distribuído e sem lockout de login")
		return nil
	}
	cliente := goredis.NewClient(&goredis.Options{
		Addr:         cfg.Addr(),
		Password:     cfg.Pass,
		DB:           cfg.DB,
		DialTimeout:  timeoutConexao,
		ReadTimeout:  timeoutConexao,
		WriteTimeout: timeoutConexao,
	})
	ctx, cancelar := context.WithTimeout(context.Background(), timeoutConexao)
	defer cancelar()
	if err := cliente.Ping(ctx).Err(); err != nil {
		_ = cliente.Close()
		// Mensagem do ping pode carregar endereço, jamais credencial.
		slog.Warn("[DEGRADADO] redis inacessível no boot — processo sobe sem cache distribuído e sem lockout de login", "erro", err.Error())
		return nil
	}
	return cliente
}

// InitRedis monta o singleton do processo a partir da config do boot.
// DEGRADÁVEL: nunca derruba o boot — erro só de configuração ausente (que o
// bootstrap já teria reprovado antes).
func InitRedis() (*goredis.Client, error) {
	mutex.Lock()
	defer mutex.Unlock()
	once.Do(func() {
		cfg, err := config.Use()
		if err != nil {
			initErr = err
			return
		}
		instance = Connect(cfg.Databases.Redis)
		iniciado = true
	})
	if initErr != nil {
		return nil, initErr
	}
	return instance, nil
}

// Get devolve o cliente do processo. NIL com erro nil = degradado (o
// consumidor trata a ausência); erro só quando o boot não rodou InitRedis.
func Get() (*goredis.Client, error) {
	mutex.RLock()
	defer mutex.RUnlock()
	if !iniciado {
		return nil, ErrNaoInicializado
	}
	return instance, nil
}

// Disponivel responde se há cliente vivo — atalho para os adaptadores que
// tratam a ausência antes de montar chaves.
func Disponivel() bool {
	cliente, err := Get()
	return err == nil && cliente != nil
}

// Close encerra o cliente (idempotente; nil é inofensivo).
func Close() {
	mutex.Lock()
	defer mutex.Unlock()
	if instance != nil {
		_ = instance.Close()
		instance = nil
	}
}

// ResetarParaTeste restaura o estado do singleton — uso EXCLUSIVO dos
// testes de pacotes que exercem o boot; nunca chamado pelo processo real.
func ResetarParaTeste() {
	mutex.Lock()
	defer mutex.Unlock()
	if instance != nil {
		_ = instance.Close()
	}
	instance = nil
	iniciado = false
	initErr = nil
	once = sync.Once{}
}
