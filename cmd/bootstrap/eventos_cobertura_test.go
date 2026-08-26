package bootstrap

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	aplicacaoauth "workspace-api/internal/identidade/application/auth"
	aplicacaoprovisionamento "workspace-api/internal/identidade/application/provisionamento"
	dominioOrganizacao "workspace-api/internal/identidade/domain/organization"
	dominioUsuario "workspace-api/internal/identidade/domain/user"
	dominioWorkspace "workspace-api/internal/identidade/domain/workspace"

	"workspace-api/internal/pkg/errobserve"
)

// Teste de COBERTURA DE EVENTOS (E3) — espelho executável do teste de
// cobertura do Swagger: varre o CÓDIGO-FONTE dos subdomínios/aplicações que
// auditam e compara, NOS DOIS SENTIDOS, as ações efetivamente emitidas com
// as entradas do events.go de cada um. Reprova:
//
//   - ação emitida (auditar() ou slog com chave "acao") SEM entrada no
//     events.go do subdomínio — vocabulário que o front não conheceria;
//   - entrada no events.go que NENHUMA escrita emite — mapping mentiroso;
//   - pacote que emite eventos SEM events.go/CatalogoEventos exportado —
//     subdomínio novo fora do padrão nasce reprovado;
//   - catálogo malformado (ação/descrição vazia, ação duplicada).
//
// Padrão dos arquivos varridos: os 4 emissores conhecidos são comparados
// contra os CatalogoEventos() importados AQUI (bootstrap conhece todas as
// camadas); qualquer OUTRO diretório sob internal/identidade/{domain,
// application} que emita eventos precisa expor o próprio CatalogoEventos —
// por isso este teste mora no bootstrap e não nos próprios pacotes.
type metaEvento struct {
	Acao      string
	Descricao string
	Campos    []string
}

var emissoresConhecidos = []struct {
	dir      string // relativo à raiz do módulo
	grupo    string // dominio/subdominio dona do vocabulário no agregado
	catalogo func() []metaEvento
}{
	{dir: "internal/identidade/domain/organization", grupo: "identidade/organization", catalogo: metaOrganizacao},
	{dir: "internal/identidade/domain/workspace", grupo: "identidade/workspace", catalogo: metaWorkspace},
	{dir: "internal/identidade/domain/user", grupo: "identidade/user", catalogo: metaUsuario},
	{dir: "internal/identidade/application/auth", grupo: "identidade/auth", catalogo: metaAuth},
	{dir: "internal/identidade/application/provisionamento", grupo: "identidade/provisionamento", catalogo: metaProvisionamento},
}

// Conversões dos catálogos NATIVOS (tipos homônimos por pacote) para a forma
// local do teste — mesmo espírito do agregador do bootstrap.
func metaOrganizacao() []metaEvento {
	return converterMeta(dominioOrganizacao.CatalogoEventos(), func(m dominioOrganizacao.EventoMeta) metaEvento {
		return metaEvento{Acao: m.Acao, Descricao: m.Descricao, Campos: m.Campos}
	})
}

func metaWorkspace() []metaEvento {
	return converterMeta(dominioWorkspace.CatalogoEventos(), func(m dominioWorkspace.EventoMeta) metaEvento {
		return metaEvento{Acao: m.Acao, Descricao: m.Descricao, Campos: m.Campos}
	})
}

func metaUsuario() []metaEvento {
	return converterMeta(dominioUsuario.CatalogoEventos(), func(m dominioUsuario.EventoMeta) metaEvento {
		return metaEvento{Acao: m.Acao, Descricao: m.Descricao, Campos: m.Campos}
	})
}

func metaAuth() []metaEvento {
	return converterMeta(aplicacaoauth.CatalogoEventos(), func(m aplicacaoauth.EventoMeta) metaEvento {
		return metaEvento{Acao: m.Acao, Descricao: m.Descricao, Campos: m.Campos}
	})
}

func metaProvisionamento() []metaEvento {
	return converterMeta(aplicacaoprovisionamento.CatalogoEventos(), func(m aplicacaoprovisionamento.EventoMeta) metaEvento {
		return metaEvento{Acao: m.Acao, Descricao: m.Descricao, Campos: m.Campos}
	})
}

func converterMeta[T any](itens []T, para func(T) metaEvento) []metaEvento {
	lista := make([]metaEvento, 0, len(itens))
	for _, item := range itens {
		lista = append(lista, para(item))
	}
	return lista
}

func TestCoberturaDeEventosNosDoisSentidos(t *testing.T) {
	raiz := filepath.Join("..", "..")

	// Varredura genérica: TODO diretório de subdomínio/aplicação que emite
	// eventos deve estar na lista dos conhecidos (que têm events.go +
	// CatalogoEventos exportado e são comparados adiante).
	emitentes := map[string][]string{}
	pastas, err := os.ReadDir(filepath.Join(raiz, "internal", "identidade"))
	require.NoError(t, err)
	for _, familia := range pastas {
		if !familia.IsDir() || (familia.Name() != "domain" && familia.Name() != "application") {
			continue
		}
		subdirs, err := os.ReadDir(filepath.Join(raiz, "internal", "identidade", familia.Name()))
		require.NoError(t, err)
		for _, sub := range subdirs {
			if !sub.IsDir() {
				continue
			}
			caminho := "internal/identidade/" + familia.Name() + "/" + sub.Name()
			usadas, err := acoesEmitidasNoFonte(filepath.Join(raiz, caminho))
			require.NoError(t, err, "parse dos fontes de %s", caminho)
			if len(usadas) > 0 {
				emitentes[caminho] = usadas
			}
		}
	}

	conhecidos := map[string]bool{}
	for _, alvo := range emissoresConhecidos {
		conhecidos[alvo.dir] = true

		metas := alvo.catalogo()
		declaradas := make([]string, 0, len(metas))
		for _, meta := range metas {
			declaradas = append(declaradas, meta.Acao)
		}
		usadas := normalizar(emitentes[alvo.dir])
		delete(emitentes, alvo.dir)

		faltando := diferenca(usadas, declaradas)
		sobrando := diferenca(declaradas, usadas)
		assert.Empty(t, faltando,
			"%s: ação emitida sem entrada no events.go do subdomínio — declare-a com descrição PT-BR e campos do payload: %v",
			alvo.dir, faltando)
		assert.Empty(t, sobrando,
			"%s: entrada no events.go que nenhuma escrita emite — remova-a ou implemente a emissão: %v",
			alvo.dir, sobrando)

		validarCatalogoMalformado(t, alvo.dir, metas)
	}
	assert.Empty(t, emitentes,
		"pacotes que emitem eventos de auditoria sem events.go/CatalogoEventos no lugar certo: %v — crie o events.go seguindo o padrão do doc 04", chaves(emitentes))
}

// TestAgregadorEventosConverteCatalogosNativos prova que o agregador ligado
// no boot preserva TODAS as entradas nativas e preenche dominio/subdominio
// corretamente — sem isso GET /api/system/eventos agruparia errado em
// silêncio. Desde a evolução errobserve, o agregado carrega TAMBÉM o
// vocabulário de erros observados (um evento por código, com severidade) e o
// namespace reservado sistema/plataforma — aqui se confere que cada grupo tem
// os nativos DE AUDITORIA intactos MAIS os códigos de erro esperados.
func TestAgregadorEventosConverteCatalogosNativos(t *testing.T) {
	agregado := novoAgregadorEventos().CatalogoEventos()

	porGrupo := map[string]int{}
	porAcaoGrupo := map[string]bool{}
	for _, meta := range agregado {
		assert.NotEmpty(t, meta.Dominio, "evento %q sem domínio na conversão", meta.Acao)
		assert.NotEmpty(t, meta.Subdominio, "evento %q sem subdomínio na conversão", meta.Acao)
		assert.NotEmpty(t, meta.Acao)
		assert.NotEmpty(t, meta.Descricao)
		if meta.Campos != nil {
			for _, campo := range meta.Campos {
				assert.NotEmpty(t, campo, "evento %q com campo vazio", meta.Acao)
			}
		}
		porGrupo[meta.Dominio+"/"+meta.Subdominio]++
		porAcaoGrupo[meta.Dominio+"/"+meta.Subdominio+"/"+meta.Acao] = true
	}

	nativos := []struct {
		dir   string
		itens int
	}{
		{dir: "identidade/organization", itens: len(dominioOrganizacao.CatalogoEventos())},
		{dir: "identidade/workspace", itens: len(dominioWorkspace.CatalogoEventos())},
		{dir: "identidade/user", itens: len(dominioUsuario.CatalogoEventos())},
		{dir: "identidade/auth", itens: len(aplicacaoauth.CatalogoEventos())},
		{dir: "identidade/provisionamento", itens: len(aplicacaoprovisionamento.CatalogoEventos())},
	}
	totalAuditados := 0
	for _, nativo := range nativos {
		assert.GreaterOrEqual(t, porGrupo[nativo.dir], nativo.itens,
			"grupo %s perdeu entradas de auditoria na conversão do agregador", nativo.dir)
		totalAuditados += nativo.itens
	}
	// Cada ação auditada nativa segue presente no grupo dona dela.
	for _, alvo := range emissoresConhecidos {
		for _, meta := range alvo.catalogo() {
			assert.True(t, porAcaoGrupo[alvo.grupo+"/"+meta.Acao],
				"ação auditada %q do grupo %s sumiu do agregado", meta.Acao, alvo.grupo)
		}
	}

	// Vocabulário de ERROS observados (errobserve): um evento por código de
	// cada observador + o namespace RESERVADO visível.
	totalErrosObservados := 0
	for _, grupo := range errobserve.CatalogoGlobal() {
		totalErrosObservados += len(grupo.Erros)
		for _, meta := range grupo.Erros {
			chave := grupo.Dominio + "/" + grupo.Subdominio + "/" + meta.Codigo
			assert.True(t, porAcaoGrupo[chave],
				"código de erro %q ausente no agregado (grupo %s)", meta.Codigo, chave)
			assert.Contains(t, []string{"warn", "error", "critical"}, meta.Severidade,
				"código %q com severidade fora do conjunto fechado", meta.Codigo)
		}
	}
	for _, meta := range errobserve.CatalogoSistema() {
		assert.True(t, porAcaoGrupo[errobserve.NamespaceReservado+"/plataforma/"+meta.Codigo],
			"evento de plataforma %q ausente no agregado — namespace reservado deve ficar visível", meta.Codigo)
	}
	assert.Len(t, agregado, totalAuditados+totalErrosObservados+len(errobserve.CatalogoSistema()),
		"agregado = auditoria nativa + códigos de erro observados + vocabulário de plataforma")
}

// --- Varredura AST -----------------------------------------------------------

// acoesEmitidasNoFonte devolve as ações estáveis emitidas pelos fontes do
// diretório, por DUAS formas reconhecidas:
//   - chamada a `auditar(...)` com literal de string no 2º argumento
//     (`s.auditar(ctx, "criar", ...)`) — a porta da trilha assíncrona;
//   - par de literais `"acao", "x"` em argumentos de chamada (slog legado
//     das cascatas/suporte) — mesmo vocabulário fechado do payload.
func acoesEmitidasNoFonte(dir string) ([]string, error) {
	entradas, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	acoes := map[string]bool{}
	for _, entrada := range entradas {
		if entrada.IsDir() || !strings.HasSuffix(entrada.Name(), ".go") || strings.HasSuffix(entrada.Name(), "_test.go") {
			continue
		}
		arquivo := token.NewFileSet()
		arvore, err := parser.ParseFile(arquivo, filepath.Join(dir, entrada.Name()), nil, 0)
		if err != nil {
			return nil, err
		}
		ast.Inspect(arvore, func(no ast.Node) bool {
			chamada, ok := no.(*ast.CallExpr)
			if !ok {
				return true
			}
			if seletor, ok := chamada.Fun.(*ast.SelectorExpr); ok && seletor.Sel.Name == "auditar" {
				for i, arg := range chamada.Args {
					if i > 1 {
						break // ação é sempre ctx(0) → literal(1)
					}
					if lit, ok := arg.(*ast.BasicLit); ok && lit.Kind == token.STRING {
						acoes[literalParaTexto(lit.Value)] = true
					}
				}
			}
			for i := 0; i+1 < len(chamada.Args); i++ {
				chave, okChave := chamada.Args[i].(*ast.BasicLit)
				valor, okValor := chamada.Args[i+1].(*ast.BasicLit)
				if okChave && okValor && chave.Kind == token.STRING && valor.Kind == token.STRING &&
					literalParaTexto(chave.Value) == "acao" {
					acoes[literalParaTexto(valor.Value)] = true
				}
			}
			return true
		})
	}
	return chaves(acoes), nil
}

// literalParaTexto remove as aspas do literal Go ("criar" → criar).
func literalParaTexto(literal string) string {
	return strings.Trim(literal, "`\"")
}

// --- Sanidade do catálogo ----------------------------------------------------

func validarCatalogoMalformado(t *testing.T, dir string, metas []metaEvento) {
	t.Helper()
	vistos := map[string]bool{}
	for _, ev := range metas {
		assert.NotEmpty(t, ev.Acao, "%s: evento sem ação estável", dir)
		assert.NotEmpty(t, ev.Descricao, "%s: evento %q sem descrição PT-BR — o front exibe essa mensagem", dir, ev.Acao)
		for _, campo := range ev.Campos {
			assert.NotEmpty(t, campo, "%s: evento %q tem campo vazio na lista", dir, ev.Acao)
		}
		assert.False(t, vistos[ev.Acao], "%s: ação duplicada no catálogo de eventos: %q", dir, ev.Acao)
		vistos[ev.Acao] = true
	}
}

// --- Helpers -----------------------------------------------------------------

// normalizar devolve cópia ordenada (comparação e mensagens determinísticas).
func normalizar(valores []string) []string {
	lista := append([]string(nil), valores...)
	sort.Strings(lista)
	return lista
}

func diferenca(a, b []string) []string {
	emB := map[string]bool{}
	for _, v := range b {
		emB[v] = true
	}
	faltando := make([]string, 0)
	for _, v := range a {
		if !emB[v] {
			faltando = append(faltando, v)
		}
	}
	return normalizar(faltando)
}

func chaves[T any](m map[string]T) []string {
	lista := make([]string, 0, len(m))
	for k := range m {
		lista = append(lista, k)
	}
	sort.Strings(lista)
	return lista
}
