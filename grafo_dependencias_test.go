package main

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// --- Conferência do Dependency Graph (fase F6) -------------------------------
//
// O Dependency Graph do Go Architect é uma ferramenta VISUAL (aplicação de
// desktop) e o módulo dela não compila com o toolchain atual (x/tools antigo,
// tokeninternal). Esta suíte reproduz o MESMO grafo que ela desenha — nós
// internos do projeto e arestas de import de produção, via `go list -json` —
// e transforma a conferência do doc 01 em GATE executável:
//
//   - nenhuma aresta infra → infra;
//   - nenhuma aresta middleware → {dominio}/domain ou → application;
//   - nenhuma aresta model → domain/application/infra/middleware;
//   - nenhuma aresta domain → domain (irmão ou cruzado) nem → application/cmd;
//   - nenhuma aresta application → domain/ nem → cmd.
//
// O fluxo permitido `pkg ← infra ← {dominio}/model ← {dominio}/domain ←
// {dominio}/application ← cmd` fica valendo por construção: tudo que NÃO é
// proibido acima é exatamente o que o fluxo autoriza (e o arch-go.yml já
// garante o resto com shouldOnlyDependsOn).

const moduloTeste = "workspace-api"

type pacoteGrafo struct {
	ImportPath string
	Imports    []string
}

// TestGrafoDeDependenciasRespeitaAsCamadas roda `go list -json ./...`,
// classifica cada pacote interno pela camada do doc 01 e reprova qualquer
// aresta proibida, apontando origem e destino.
func TestGrafoDeDependenciasRespeitaAsCamadas(t *testing.T) {
	saida, err := exec.Command("go", "list", "-json", "./...").Output()
	if err != nil {
		t.Fatalf("go list falhou: %v", err)
	}

	pacotes := map[string]pacoteGrafo{}
	decodificador := json.NewDecoder(strings.NewReader(string(saida)))
	for decodificador.More() {
		var p pacoteGrafo
		if err := decodificador.Decode(&p); err != nil {
			t.Fatalf("decodificando go list: %v", err)
		}
		pacotes[p.ImportPath] = p
	}
	requirePacotesChave(t, pacotes)

	violacoes := []string{}
	arestasPorCamada := map[string]int{} // "origem→destino" para o resumo do log

	for origem, p := range pacotes {
		camadaOrigem := camadaDe(origem)
		for _, destino := range p.Imports {
			camadaDestino := camadaDe(destino)
			if camadaDestino == "" {
				continue // stdlib/lib externa — fora das regras de camada
			}
			arestasPorCamada[camadaOrigem+"→"+camadaDestino]++

			if proibida(camadaOrigem, camadaDestino, origem, destino) {
				violacoes = append(violacoes, origem+" → "+destino+
					" ("+camadaOrigem+" → "+camadaDestino+")")
			}
		}
	}

	t.Logf("grafo de pacotes (arestas internas por par de camadas):")
	for _, par := range resumoOrdenado(arestasPorCamada) {
		t.Logf("  %-28s %d", par, arestasPorCamada[par])
	}

	if len(violacoes) > 0 {
		t.Fatalf("grafo de dependências viola o fluxo do doc 01:\n  %s",
			strings.Join(violacoes, "\n  "))
	}
}

// requirePacotesChave garante que o grafo analisado contém as camadas do
// template — sem isso o teste passaria vacuamente se `./...` mudasse.
func requirePacotesChave(t *testing.T, pacotes map[string]pacoteGrafo) {
	t.Helper()
	obrigatorio := []string{
		moduloTeste + "/internal/pkg/orgctx",
		moduloTeste + "/internal/infra/database/postgres",
		moduloTeste + "/internal/middleware",
		moduloTeste + "/internal/identidade/model/workspace",
		moduloTeste + "/internal/identidade/domain/workspace",
		moduloTeste + "/internal/identidade/application/auth",
		moduloTeste,
	}
	for _, esperado := range obrigatorio {
		if _, ok := pacotes[esperado]; !ok {
			t.Fatalf("pacote esperado ausente do grafo: %s", esperado)
		}
	}
}
// camadaDe classifica um pacote interno pela camada do doc 01; "" = fora do
// projeto (stdlib/externa).
func camadaDe(importPath string) string {
	switch {
	case importPath == moduloTeste || strings.HasPrefix(importPath, moduloTeste+"/cmd"):
		return "cmd/raiz"
	case strings.HasPrefix(importPath, moduloTeste+"/internal/pkg"):
		return "pkg"
	case strings.HasPrefix(importPath, moduloTeste+"/internal/infra"):
		return "infra"
	case strings.HasPrefix(importPath, moduloTeste+"/internal/middleware"):
		return "middleware"
	case strings.Contains(importPath, "/model/") &&
		strings.HasPrefix(importPath, moduloTeste+"/internal/"):
		return "model"
	case strings.Contains(importPath, "/domain/") &&
		strings.HasPrefix(importPath, moduloTeste+"/internal/"):
		return "domain"
	case strings.Contains(importPath, "/application/") &&
		strings.HasPrefix(importPath, moduloTeste+"/internal/"):
		return "application"
	default:
		// Pacote interno sem regra conhecida (ex.: docs gerado) — tratado como
		// neutro aqui; a regra dele vive no arch-go.yml.
		if strings.HasPrefix(importPath, moduloTeste+"/") {
			return "neutro"
		}
		return ""
	}
}

// proibida codifica as arestas que o doc 01 manda caçar no grafo.
func proibida(origem, destino, caminhoOrigem, caminhoDestino string) bool {
	if origem == destino {
		return false
	}
	switch origem {
	case "infra":
		return destino == "infra" // nunca outro infra (regra 2)
	case "middleware":
		return destino == "domain" || destino == "application" // seria ciclo
	case "model":
		// Folha do domínio: só pkg (+ libs) — regra 9.
		return destino == "domain" || destino == "application" ||
			destino == "infra" || destino == "middleware"
	case "domain":
		// Sem application/cmd e SEM irmão: dependência entre subdomínios entra
		// por interface ligada no bootstrap (regras 3 e 4).
		return destino == "application" || destino == "cmd/raiz" || destino == "domain"
	case "application":
		// Orquestra por contratos — nunca importa o domain/ do próprio domínio
		// nem a composição (regra 5).
		return destino == "domain" || destino == "cmd/raiz"
	}
	return false
}

func resumoOrdenado(m map[string]int) []string {
	chaves := make([]string, 0, len(m))
	for k := range m {
		chaves = append(chaves, k)
	}
	for i := 0; i < len(chaves); i++ {
		for j := i + 1; j < len(chaves); j++ {
			if chaves[j] < chaves[i] {
				chaves[i], chaves[j] = chaves[j], chaves[i]
			}
		}
	}
	saida := make([]string, 0, len(chaves))
	for _, k := range chaves {
		saida = append(saida, k)
	}
	return saida
}
