// Package server sobe o http.Server sobre o engine montado em routes:
// timeouts explícitos sempre e graceful shutdown que DRENA as requisições
// em voo no SIGTERM/SIGINT antes de devolver o controle ao bootstrap.
// Este pacote NÃO importa internal/infra — sondas chegam injetadas.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Opções de execução do servidor (espelham server.http da config).
type Opcoes struct {
	Porta           int
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
}

// Servidor é o http.Server pronto para subir e descer — nada mais.
type Servidor struct {
	srv             *http.Server
	shutdownTimeout time.Duration
}

// Novo monta o servidor com TODOS os timeouts explícitos — servidor sem
// timeout é refém de cliente lento.
func Novo(handler http.Handler, opcoes Opcoes) *Servidor {
	return &Servidor{
		srv: &http.Server{
			Addr:              fmt.Sprintf(":%d", opcoes.Porta),
			Handler:           handler,
			ReadHeaderTimeout: opcoes.ReadTimeout,
			ReadTimeout:       opcoes.ReadTimeout,
			WriteTimeout:      opcoes.WriteTimeout,
			IdleTimeout:       opcoes.IdleTimeout,
		},
		shutdownTimeout: opcoes.ShutdownTimeout,
	}
}

// Porta devolve a porta configurada.
func (s *Servidor) Porta() int {
	var porta int
	_, _ = fmt.Sscanf(s.srv.Addr, ":%d", &porta)
	return porta
}

// Servir sobe o listener e bloqueia até falha ou encerramento externo
// (http.ErrServerClosed não é erro).
func (s *Servidor) Servir() error {
	err := s.srv.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("server: falha ao servir na porta %d: %w", s.Porta(), err)
	}
	return nil
}

// Parar para de aceitar conexões novas e DRENA as requisições em voo até o
// timeout de shutdown — cortar requisição lenta no meio é o último recurso.
func (s *Servidor) Parar(ctx context.Context) error {
	timeout := s.shutdownTimeout
	if prazo, ok := ctx.Deadline(); ok {
		if restante := time.Until(prazo); restante < timeout {
			timeout = restante
		}
	}
	ctxDesligar, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	inicio := time.Now()
	if err := s.srv.Shutdown(ctxDesligar); err != nil {
		return fmt.Errorf("server: shutdown excedeu %s com requisições pendentes: %w", timeout, err)
	}
	slog.Info("server: shutdown drenou requisições e fechou", "duracao_ms", time.Since(inicio).Milliseconds())
	return nil
}

// ServirAteSinal sobe o servidor e aguarda SIGTERM/SIGINT; recebido o sinal,
// drena as requisições em voo e devolve o processo ao bootstrap (que segue
// o fechamento LIFO).
func (s *Servidor) ServirAteSinal() error {
	errCh := make(chan error, 1)
	go func() { errCh <- s.Servir() }()

	slog.Info("server: escutando", "porta", s.Porta())
	sinais := make(chan os.Signal, 1)
	signal.Notify(sinais, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sinais)

	select {
	case err := <-errCh:
		return err
	case sig := <-sinais:
		slog.Info("server: sinal recebido, drenando conexões", "sinal", sig.String())
		return s.Parar(context.Background())
	}
}
