// R7 (issue #25): Use/MustUse/ResetarParaTeste sob disputa — o lazy da
// cadeia fechada e o reset do singleton são sincronizados; este teste existe
// para rodar com -race e reprovar qualquer leitura/escrita fora do mutex.
package middleware

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"workspace-api/internal/infra/jwt"
)

func TestUseEResetSobDisputa(t *testing.T) {
	ResetarParaTeste()
	defer ResetarParaTeste()

	const goroutines = 24
	var largada sync.WaitGroup
	largada.Add(1)
	var fim sync.WaitGroup
	fim.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer fim.Done()
			largada.Wait()
			for j := 0; j < 50; j++ {
				switch (i + j) % 3 {
				case 0:
					_ = Use() // lazy da cadeia fechada + leitura de instance
				case 1:
					_ = New(Dependencias{}) // erro esperado, toca once/initErr
				case 2:
					ResetarParaTeste()
				}
			}
		}(i)
	}
	largada.Done()
	fim.Wait()

	// sync.Once pode ter sido consumido por um New falho da disputa: estado
	// limpo antes de provar que a cadeia volta a bootar.
	ResetarParaTeste()

	// Depois da disputa, a cadeia segue utilizável: fechada responde.
	assert.NotNil(t, Use())

	gerenciador, err := jwt.Connect("segredo-da-disputa-do-use-e-reset-r7!", 30, 24)
	require.NoError(t, err)
	require.NoError(t, New(Dependencias{
		JWT:        gerenciador,
		Workspaces: novoResolvedorWorkspacesFalso(),
		Permissoes: &resolvedorPermissoesFalso{},
	}))
	assert.Same(t, MustUse(), instance)
}

func TestMustUseRecuperaPanicoSemBoot(t *testing.T) {
	ResetarParaTeste()
	defer ResetarParaTeste()

	assert.PanicsWithError(t, ErrNaoInicializada.Error(), func() {
		_ = MustUse()
	})
}
