// Package jwt guarda o segredo e os parâmetros de emissão/validação de
// tokens (access + refresh). JWT é dependência FATAL: sem chave confiável o
// processo não sobe (decisão do bootstrap).
//
// Par função pura + singleton: Connect(...) é puro e testável; InitJWT/
// Get/Close são o singleton do processo com sync.Once.
package jwt

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"workspace-api/internal/pkg/config"
)

const comprimentoMinimoSegredo = 16

var (
	instance *Manager
	once     sync.Once
	initErr  error

	ErrNaoInicializado = errors.New("jwt não inicializado: chame InitJWT no boot")
)

// Manager carrega os parâmetros assinados no boot; a lógica de emissão e
// verificação de tokens entra na Fase 1 (middleware fail-closed).
type Manager struct {
	segredo    []byte
	ttlAccess  time.Duration
	ttlRefresh time.Duration
}

// TTLAccess devolve a validade do access token.
func (m *Manager) TTLAccess() time.Duration { return m.ttlAccess }

// TTLRefresh devolve a validade do refresh token.
func (m *Manager) TTLRefresh() time.Duration { return m.ttlRefresh }

// Connect é a função PURA de montagem: valida o segredo e os prazos sem
// tocar em estado global — os testes usam só ele.
func Connect(segredo string, ttlMin, refreshHoras int) (*Manager, error) {
	if len(strings.TrimSpace(segredo)) < comprimentoMinimoSegredo {
		return nil, fmt.Errorf("jwt: security.jwt_secret ausente ou curto demais (mínimo %d caracteres)", comprimentoMinimoSegredo)
	}
	if ttlMin <= 0 {
		return nil, errors.New("jwt: security.jwt_ttl_min deve ser maior que zero")
	}
	if refreshHoras <= 0 {
		return nil, errors.New("jwt: security.jwt_refresh_ttl_hours deve ser maior que zero")
	}
	return &Manager{
		segredo:    []byte(segredo),
		ttlAccess:  time.Duration(ttlMin) * time.Minute,
		ttlRefresh: time.Duration(refreshHoras) * time.Hour,
	}, nil
}

// InitJWT monta o singleton do processo a partir da config do boot. Erro
// aqui é fatal para o processo (o bootstrap encerra com a mensagem).
func InitJWT() (*Manager, error) {
	once.Do(func() {
		cfg, err := config.Use()
		if err != nil {
			initErr = fmt.Errorf("jwt: config ausente no boot: %w", err)
			return
		}
		manager, err := Connect(cfg.Security.JwtSecret, cfg.Security.JwtTtlMin, cfg.Security.JwtRefreshTtlHours)
		if err != nil {
			initErr = err
			return
		}
		instance = manager
	})
	return instance, initErr
}

// Get devolve o manager do processo; erro se o boot ainda não rodou.
func Get() (*Manager, error) {
	if instance == nil {
		return nil, ErrNaoInicializado
	}
	return instance, nil
}

// Close limpa o segredo da memória — fechamento LIFO do bootstrap.
func Close() {
	if instance != nil {
		for i := range instance.segredo {
			instance.segredo[i] = 0
		}
		instance = nil
	}
}

// ResetarParaTeste restaura o estado do singleton — uso EXCLUSIVO dos testes.
func ResetarParaTeste() {
	instance = nil
	initErr = nil
	once = sync.Once{}
}
