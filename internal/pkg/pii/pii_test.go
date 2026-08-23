package pii

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMascaraEmail(t *testing.T) {
	casos := []struct {
		entrada  string
		esperado string
	}{
		{"ana@exemplo.com", "a***@exemplo.com"},
		{" ana.silva@corp.exemplo.com ", "a***@corp.exemplo.com"},
		{"a@b.co", "a***@b.co"},
		{"sem-arroba", "***"},
		{"", "***"},
		{"   ", "***"},
		{"@dominio.com", "***"},
		{"local@", "***"},
		{"@@@", "***"},
	}
	for _, caso := range casos {
		t.Run(fmt.Sprintf("%q", caso.entrada), func(t *testing.T) {
			assert.Equal(t, caso.esperado, MascaraEmail(caso.entrada))
			if caso.entrada != "" {
				assert.NotContains(t, MascaraEmail(caso.entrada), caso.entrada,
					"e-mail de entrada nunca aparece inteiro na saída")
			}
		})
	}
}
