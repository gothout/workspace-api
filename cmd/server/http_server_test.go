package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// portaLivre reserva uma porta efêmera para o teste.
func portaLivre(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	porta := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	return porta
}

// TestPararDrenaRequisicaoEmVoo prova o contrato do cmd/server: no shutdown
// o servidor para de aceitar conexões MAS espera a requisição lenta terminar
// em vez de cortá-la.
func TestPararDrenaRequisicaoEmVoo(t *testing.T) {
	recebeu := make(chan struct{})
	liberar := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/lento", func(w http.ResponseWriter, _ *http.Request) {
		close(recebeu) // sinaliza que a requisição chegou ao handler
		<-liberar      // segura a resposta até o teste liberar
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/pronto", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	servidor := Novo(mux, Opcoes{
		Porta:           portaLivre(t),
		ReadTimeout:     5 * time.Second,
		WriteTimeout:    10 * time.Second, // maior que a requisição lenta
		IdleTimeout:     5 * time.Second,
		ShutdownTimeout: 5 * time.Second,
	})

	errServir := make(chan error, 1)
	go func() { errServir <- servidor.Servir() }()

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", servidor.Porta())
	require.Eventually(t, func() bool {
		resp, err := http.Get(baseURL + "/pronto")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 3*time.Second, 50*time.Millisecond, "servidor não abriu a porta")

	resposta := make(chan *http.Response, 1)
	go func() {
		resp, err := http.Get(baseURL + "/lento")
		if err != nil {
			resp = nil
		}
		resposta <- resp
	}()
	<-recebeu // handler lento já está segurando a conexão

	errParar := make(chan error, 1)
	go func() { errParar <- servidor.Parar(context.Background()) }()

	// Ao chamar Parar, o listener para na hora: Servir encerra sem erro...
	select {
	case err := <-errServir:
		assert.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("listener não parou após iniciar o shutdown")
	}

	// ...mas o drain continua segurando enquanto há requisição em voo.
	select {
	case err := <-errParar:
		t.Fatalf("Parar retornou antes de drenar a requisição em voo: %v", err)
	case <-time.After(300 * time.Millisecond):
	}

	// Libera a resposta: agora o drain completa e o cliente recebe 200.
	close(liberar)
	select {
	case err := <-errParar:
		assert.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("Parar não completou após drenar")
	}

	select {
	case resp := <-resposta:
		require.NotNil(t, resp)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode, "requisição lenta foi DRENADA e não cortada")
	case <-time.After(2 * time.Second):
		t.Fatal("cliente não recebeu a resposta drenada")
	}
}
