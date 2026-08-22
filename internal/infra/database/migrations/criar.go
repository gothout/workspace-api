package migrations

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

// padraoDescricao exige {dominio}_{subdominio}_{desc}: pelo menos dois
// underscores separando trechos válidos.
var padraoDescricao = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+){2,}$`)

const cabecalhoArquivo = `-- Migration %04d_%s (%s).
-- %s
`

// Create gera o par NNNN_{descricao}.{up,down}.sql já no padrão de nome,
// com a numeração seguinte — criar migration à mão fora do padrão reprova.
func Create(dir, descricao string) (string, string, error) {
	if !padraoDescricao.MatchString(descricao) {
		return "", "", fmt.Errorf(
			"migrations: descrição %q inválida — use o padrão dominio_subdominio_descricao (snake_case, ao menos dois underscores)", descricao)
	}
	pares, err := listarPares(dir)
	if err != nil {
		return "", "", err
	}
	proxima := uint(1)
	if len(pares) > 0 {
		proxima = pares[len(pares)-1].versao + 1
		if proxima > 9999 {
			return "", "", fmt.Errorf("migrations: numeração esgotada acima de 9999")
		}
	}
	nomeBase := fmt.Sprintf("%04d_%s", proxima, descricao)
	caminhoUp := filepath.Join(dir, nomeBase+".up.sql")
	caminhoDown := filepath.Join(dir, nomeBase+".down.sql")

	conteudoUp := fmt.Sprintf(cabecalhoArquivo+"-- Escreva o DDL daqui para baixo.\n", proxima, descricao, "up", "O que cria/altera; descreva aqui.")
	conteudoDown := fmt.Sprintf(cabecalhoArquivo+"-- Desfaz exatamente o up; descreva o que é reversível.\n", proxima, descricao, "down", "O que desfaz.")
	if err := os.WriteFile(caminhoUp, []byte(conteudoUp), 0o644); err != nil {
		return "", "", fmt.Errorf("migrations: falha ao criar %s: %w", caminhoUp, err)
	}
	if err := os.WriteFile(caminhoDown, []byte(conteudoDown), 0o644); err != nil {
		return "", "", fmt.Errorf("migrations: falha ao criar %s: %w", caminhoDown, err)
	}
	return caminhoUp, caminhoDown, nil
}

// ProximaVersao devolve o próximo número disponível (uso do CLI no log).
func ProximaVersao(dir string) (uint, error) {
	pares, err := listarPares(dir)
	if err != nil {
		return 0, err
	}
	sort.Slice(pares, func(i, j int) bool { return pares[i].versao < pares[j].versao })
	if len(pares) == 0 {
		return 1, nil
	}
	return pares[len(pares)-1].versao + 1, nil
}
