package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestArquiteturaDoProjeto roda o arch-go (regras de camada em arch-go.yml,
// compliance e coverage 100) dentro do go test ./... — pula sozinho se a
// ferramenta não estiver instalada; com ela instalada, reprovação é gate.
//
// Instalar: go install github.com/fdaines/arch-go@latest
func TestArquiteturaDoProjeto(t *testing.T) {
	executavel, err := exec.LookPath("arch-go")
	if err != nil {
		t.Skip("arch-go não instalado: gate de arquitetura pulado (go install github.com/fdaines/arch-go@latest)")
	}
	saida, err := exec.Command(executavel).CombinedOutput()
	if err != nil {
		t.Fatalf("arch-go reprovou a arquitetura:\n%s", saida)
	}
}

// raizDoRepositorio sobe até encontrar o arch-go.yml — o teste roda da raiz
// do módulo, mas fica à prova de mudança de diretório de trabalho.
func raizDoRepositorio(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("falha ao obter diretório atual: %v", err)
	}
	for {
		if _, erro := os.Stat(filepath.Join(dir, "arch-go.yml")); erro == nil {
			return dir
		}
		pai := filepath.Dir(dir)
		if pai == dir {
			t.Fatal("arch-go.yml não encontrado acima do diretório atual")
		}
		dir = pai
	}
}
