// Package migrations executa as migrations SQL de db/migrations sobre o
// Postgres (golang-migrate): `up` automático no boot com advisory lock, CLI
// completa para operação manual e validação sem conexão.
//
// O runner recebe a conexão por interface (FonteConexao) — nunca importa
// outro pacote de infra; quem liga é o cmd/bootstrap. Arquivos marcados
// `-- manual` no cabeçalho são ignorados em toda execução programática e
// aparecem no status como pendente-manual.
package migrations

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/golang-migrate/migrate/v4/source"
)

const (
	// PrefixoManual marca arquivos executados à mão (ex.: CREATE INDEX
	// CONCURRENTLY, que não aceita transação). Primeira linha do arquivo up.
	PrefixoManual = "-- manual"
)

// padraoArquivo é o nome canônico: NNNN_{dominio}_{subdominio}_{desc}.{up|down}.sql
var padraoArquivo = regexp.MustCompile(`^(\d{4})_([a-z0-9_]+)\.(up|down)\.sql$`)

// parMigracao é um par up/down do diretório, com a marcação manual resolvida.
type parMigracao struct {
	versao      uint
	nome        string
	caminhoUp   string
	caminhoDown string
	manual      bool
}

// listarPares varre dir e devolve os pares ordenados por versão. Falha se:
// arquivo .sql fora do padrão, par incompleto (up sem down ou vice-versa) ou
// número duplicado. Arquivos não-SQL (docs etc.) são ignorados.
func listarPares(dir string) ([]parMigracao, error) {
	entradas, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("migrations: falha ao ler diretório %s: %w", dir, err)
	}
	porVersao := map[uint]*parMigracao{}
	var ordem []uint
	for _, entrada := range entradas {
		if entrada.IsDir() {
			continue
		}
		grupos := padraoArquivo.FindStringSubmatch(entrada.Name())
		if grupos == nil {
			if filepath.Ext(entrada.Name()) == ".sql" {
				return nil, fmt.Errorf("migrations: arquivo .sql fora do padrão NNNN_{dominio}_{subdominio}_{desc}.{up|down}.sql: %s", entrada.Name())
			}
			continue
		}
		versao64, _ := strconv.ParseUint(grupos[1], 10, 32)
		versao := uint(versao64)
		direcao := grupos[3]
		par := porVersao[versao]
		if par == nil {
			par = &parMigracao{versao: versao, nome: grupos[2]}
			porVersao[versao] = par
			ordem = append(ordem, versao)
		} else if par.nome != grupos[2] {
			return nil, fmt.Errorf("migrations: versão %04d usada por nomes diferentes (%s e %s)", versao, par.nome, grupos[2])
		}
		caminho := filepath.Join(dir, entrada.Name())
		if direcao == "up" {
			if par.caminhoUp != "" {
				return nil, fmt.Errorf("migrations: versão %04d duplicada (up)", versao)
			}
			par.caminhoUp = caminho
		} else {
			if par.caminhoDown != "" {
				return nil, fmt.Errorf("migrations: versão %04d duplicada (down)", versao)
			}
			par.caminhoDown = caminho
		}
	}
	ordenar(ordem)
	pares := make([]parMigracao, 0, len(ordem))
	for _, v := range ordem {
		par := porVersao[v]
		if par.caminhoUp == "" || par.caminhoDown == "" {
			return nil, fmt.Errorf("migrations: versão %04d sem par completo (precisa de .up.sql e .down.sql)", par.versao)
		}
		manual, err := arquivoManual(par.caminhoUp)
		if err != nil {
			return nil, err
		}
		par.manual = manual
		pares = append(pares, *par)
	}
	return pares, nil
}

func ordenar(versoes []uint) {
	sort.Slice(versoes, func(i, j int) bool { return versoes[i] < versoes[j] })
}

// arquivoManual confere se a primeira linha não vazia do up é "-- manual".
func arquivoManual(caminho string) (bool, error) {
	conteudo, err := os.ReadFile(caminho)
	if err != nil {
		return false, fmt.Errorf("migrations: falha ao ler %s: %w", caminho, err)
	}
	return primeiraLinhaEhManual(conteudo), nil
}

// --- source.Driver do golang-migrate ---------------------------------------

// driverFonte serve os pares NÃO manuais ao golang-migrate — arquivos manual
// nunca entram em execução programática (up/down/goto).
type driverFonte struct {
	pares []parMigracao
}

var _ source.Driver = (*driverFonte)(nil)

func novoDriverFonte(pares []parMigracao) *driverFonte {
	executaveis := make([]parMigracao, 0, len(pares))
	for _, p := range pares {
		if !p.manual {
			executaveis = append(executaveis, p)
		}
	}
	return &driverFonte{pares: executaveis}
}

func (d *driverFonte) Open(string) (source.Driver, error) {
	return nil, fmt.Errorf("migrations: driver de fonte não abre por URL; use novoDriverFonte")
}

func (d *driverFonte) Close() error { return nil }

func (d *driverFonte) First() (uint, error) {
	if len(d.pares) == 0 {
		return 0, os.ErrNotExist
	}
	return d.pares[0].versao, nil
}

func (d *driverFonte) Prev(versao uint) (uint, error) {
	for i := len(d.pares) - 1; i >= 0; i-- {
		if d.pares[i].versao < versao {
			return d.pares[i].versao, nil
		}
	}
	return 0, os.ErrNotExist
}

func (d *driverFonte) Next(versao uint) (uint, error) {
	for _, p := range d.pares {
		if p.versao > versao {
			return p.versao, nil
		}
	}
	return 0, os.ErrNotExist
}

func (d *driverFonte) ReadUp(versao uint) (io.ReadCloser, string, error) {
	par, ok := d.buscar(versao)
	if !ok {
		return nil, "", os.ErrNotExist
	}
	arq, err := os.Open(par.caminhoUp)
	if err != nil {
		return nil, "", fmt.Errorf("migrations: falha ao abrir %s: %w", par.caminhoUp, err)
	}
	return arq, par.nome, nil
}

func (d *driverFonte) ReadDown(versao uint) (io.ReadCloser, string, error) {
	par, ok := d.buscar(versao)
	if !ok {
		return nil, "", os.ErrNotExist
	}
	arq, err := os.Open(par.caminhoDown)
	if err != nil {
		return nil, "", fmt.Errorf("migrations: falha ao abrir %s: %w", par.caminhoDown, err)
	}
	return arq, par.nome, nil
}

func (d *driverFonte) buscar(versao uint) (parMigracao, bool) {
	for _, p := range d.pares {
		if p.versao == versao {
			return p, true
		}
	}
	return parMigracao{}, false
}

// primeiraLinhaEhManual confere a marcação ignorando linhas em branco.
func primeiraLinhaEhManual(conteudo []byte) bool {
	for _, linha := range strings.Split(string(conteudo), "\n") {
		aparada := strings.TrimSpace(linha)
		if aparada == "" {
			continue
		}
		return aparada == PrefixoManual
	}
	return false
}
