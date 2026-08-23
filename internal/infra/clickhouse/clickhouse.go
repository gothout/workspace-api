// Package clickhouse é o adaptador da dependência DEGRADÁVEL de logs
// assíncronos (evolução da issue #9): trilhas de auditoria e acesso fora do
// caminho síncrono do request, escritas em lote no ClickHouse.
//
// Regra que define o desenho (agents/02): ClickHouse NUNCA derruba o processo.
// Connect não erra — devolve a conexão quando o servidor responde, ou NIL com
// log [DEGRADADO] quando desabilitado/inacessível; o bootstrap então liga as
// trilhas ao destino stdout (pkg/log/{audit_log,access_log}.SlogPadrao).
// Telemetria nunca vale disponibilidade.
//
// O writer em lote (escritor.go) é a peça central: fila limitada por trilha,
// flush por tamanho OU por janela de tempo, fila cheia DESCARTA E CONTA
// (nunca bloqueia o request), e drain completo no shutdown. Par função pura +
// singleton: Connect/NovaEscritor puros e testáveis; InitClickhouse/Use/Close
// com sync.Once (mutex cobre o once.Do inteiro — lição R7).
package clickhouse

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	chgo "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"workspace-api/internal/pkg/config"
)

// timeoutConexao limita dial/ping: banco de log lento não pode travar o boot.
const timeoutConexao = 3 * time.Second

var (
	mutex    sync.RWMutex
	once     sync.Once
	instance *Escritor
	consult  *Consultor
	connexao driver.Conn
	iniciado bool
	initErr  error

	ErrNaoInicializado = errors.New("clickhouse não inicializado: chame InitClickhouse no boot")
)

// Connect é a função PURA de conexão: NUNCA devolve erro. Config
// desabilitada ou servidor inacessível = conexão NIL com log [DEGRADADO];
// os testes unitários usam só ele, sem singleton.
func Connect(cfg config.ClickHouseConfig) driver.Conn {
	if !cfg.Enabled {
		slog.Info("[DEGRADADO] clickhouse desabilitado na config (databases.clickhouse.enabled=false) — trilhas de auditoria e acesso saem pelo stdout")
		return nil
	}
	conn, err := chgo.Open(&chgo.Options{
		Addr: []string{cfg.Addr()},
		Auth: chgo.Auth{
			Database: cfg.Database,
			Username: cfg.User,
			Password: cfg.Pass,
		},
		DialTimeout: timeoutConexao,
	})
	if err == nil {
		ctx, cancelar := context.WithTimeout(context.Background(), timeoutConexao)
		defer cancelar()
		err = conn.Ping(ctx)
	}
	if err != nil {
		if conn != nil {
			_ = conn.Close()
		}
		// Mensagem pode carregar endereço, jamais credencial.
		slog.Warn("[DEGRADADO] clickhouse inacessível no boot — processo sobe com as trilhas no stdout", "erro", err.Error())
		return nil
	}
	return conn
}

// InitClickhouse monta o singleton do processo a partir da config do boot.
// DEGRADÁVEL: nunca derruba o boot — sem servidor, devolve (nil, nil) e o
// bootstrap liga os destinos stdout. Com servidor, abre o writer em lote que
// consome as duas trilhas e o consultor de leitura (E5) sobre a MESMA
// conexão.
func InitClickhouse() (*Escritor, error) {
	mutex.Lock()
	defer mutex.Unlock()
	once.Do(func() {
		cfg, err := config.Use()
		if err != nil {
			initErr = err
			return
		}
		connexao = Connect(cfg.Databases.ClickHouse)
		if connexao == nil {
			instance = nil
			consult = nil
		} else {
			instance = NovaEscritor(NovoGravador(connexao), cfg.Logs)
			consult = NovoConsultor(connexao)
			slog.Info("[BOOTSTRAP] clickhouse conectado",
				"database", cfg.Databases.ClickHouse.Database)
		}
		iniciado = true
	})
	if initErr != nil {
		return nil, initErr
	}
	return instance, nil
}

// Use devolve o writer do processo. NIL com erro nil = degradado (o
// consumidor trata a ausência); erro só quando o boot não rodou InitClickhouse.
func Use() (*Escritor, error) {
	mutex.RLock()
	defer mutex.RUnlock()
	if !iniciado {
		return nil, ErrNaoInicializado
	}
	return instance, nil
}

// UseConsultor devolve o consultor de leitura das trilhas (E5). NIL com erro
// nil = degradado (o consumidor responde 503 padronizado); erro só quando o
// boot não rodou InitClickhouse.
func UseConsultor() (*Consultor, error) {
	mutex.RLock()
	defer mutex.RUnlock()
	if !iniciado {
		return nil, ErrNaoInicializado
	}
	return consult, nil
}

// Close encerra o singleton: drena o writer (lotes pendentes vão ao banco)
// e fecha a conexão. Idempotente; degradado é inofensivo.
func Close() {
	mutex.Lock()
	defer mutex.Unlock()
	if instance != nil {
		instance.Fechar()
		instance = nil
	}
	consult = nil
	if connexao != nil {
		_ = connexao.Close()
		connexao = nil
	}
}

// ResetarParaTeste restaura o estado do singleton — uso EXCLUSIVO dos testes
// de pacotes que exercem o boot; nunca chamado pelo processo real.
func ResetarParaTeste() {
	mutex.Lock()
	defer mutex.Unlock()
	if instance != nil {
		instance.Fechar()
	}
	if connexao != nil {
		_ = connexao.Close()
	}
	instance = nil
	consult = nil
	connexao = nil
	iniciado = false
	initErr = nil
	once = sync.Once{}
}
