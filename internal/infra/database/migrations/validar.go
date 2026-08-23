package migrations

import (
	"fmt"
	"os"
	"strings"
)

// Validar é o gate rápido de CI/local: confere pares up/down, sequência
// sem buracos desde 0001, SQL não vazio e marcação manual bem formada —
// SEM abrir conexão com o banco.
func Validar(dir string) error {
	pares, err := listarPares(dir)
	if err != nil {
		return err
	}
	esperada := uint(1)
	for _, par := range pares {
		if par.versao != esperada {
			return fmt.Errorf("migrations: sequência quebrada: esperava versão %04d e encontrei %04d", esperada, par.versao)
		}
		esperada++
		for _, caminho := range []string{par.caminhoUp, par.caminhoDown} {
			vazio, err := sqlEfetivamenteVazio(caminho)
			if err != nil {
				return err
			}
			if vazio {
				return fmt.Errorf("migrations: %s está vazio (só comentários/espaços)", caminho)
			}
		}
	}
	return nil
}

// sqlEfetivamenteVazio ignora linhas de comentário e em branco.
func sqlEfetivamenteVazio(caminho string) (bool, error) {
	conteudo, err := os.ReadFile(caminho)
	if err != nil {
		return false, fmt.Errorf("migrations: falha ao ler %s: %w", caminho, err)
	}
	for _, linha := range strings.Split(string(conteudo), "\n") {
		aparada := strings.TrimSpace(linha)
		if aparada == "" || strings.HasPrefix(aparada, "--") {
			continue
		}
		return false, nil
	}
	return true, nil
}
