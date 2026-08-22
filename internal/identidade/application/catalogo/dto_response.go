package catalogo

import (
	"sort"
	"strings"

	"workspace-api/internal/middleware"
	"workspace-api/internal/pkg/rest_err"
)

// --- CONTRATO — GET /api/application/identidade/catalogo/permissoes/minhas ---
//
// Árvore dominio → subdominio → ações, JÁ FILTRADA pelas permissões efetivas
// do usuário autenticado (doc 04). Ação ausente na árvore = controle escondido
// no front; super_admin (curinga *:*) recebe a árvore inteira.

// ArvoreResponseDto é a raiz da resposta.
type ArvoreResponseDto struct {
	Dominios []DominioDto `json:"dominios"`
}

// DominioDto agrupa os subdomínios de um domínio.
type DominioDto struct {
	Dominio     string               `json:"dominio"`
	Subdominios []SubdominioAcoesDto `json:"subdominios"`
}

// SubdominioAcoesDto lista as ações visíveis de um subdomínio.
type SubdominioAcoesDto struct {
	Subdominio string    `json:"subdominio"`
	Acoes      []AcaoDto `json:"acoes"`
}

// AcaoDto é UM par rota+método que o usuário pode executar.
type AcaoDto struct {
	Permissao string `json:"permissao"`
	Descricao string `json:"descricao"`
	Rota      string `json:"rota"`
	Metodo    string `json:"metodo"`
	GrupoMenu string `json:"grupo_menu"`
}

// NovoArvoreResponseDto monta a árvore a partir do catálogo agregado,
// mantendo só as permissões atendidas pelo conjunto efetivo do usuário —
// o MESMO matcher do RequirePermission (middleware.Atende), nunca releitura
// própria. Saída em ordem determinística; sem permissão = árvore vazia.
func NovoArvoreResponseDto(catalogo []PermissaoMeta, efetivas []string) ArvoreResponseDto {
	tipoDominio := map[string]map[string][]AcaoDto{}
	for _, meta := range catalogo {
		if !middleware.Atende(efetivas, meta.Permissao) {
			continue
		}
		dominio, subdominio, ok := partirPermissao(meta.Permissao)
		if !ok {
			continue // exigida malformada nunca é atendida — defesa extra aqui
		}
		if tipoDominio[dominio] == nil {
			tipoDominio[dominio] = map[string][]AcaoDto{}
		}
		for _, rota := range meta.Rotas {
			// Cada ação da árvore corresponde a UM par rota+método do Catalogo().
			tipoDominio[dominio][subdominio] = append(tipoDominio[dominio][subdominio], AcaoDto{
				Permissao: meta.Permissao,
				Descricao: meta.Descricao,
				Rota:      rota.Rota,
				Metodo:    rota.Metodo,
				GrupoMenu: meta.GrupoMenu,
			})
		}
	}
	return ArvoreResponseDto{Dominios: montarDominios(tipoDominio)}
}

// partirPermissao separa dominio e subdominio da string dominio:subdominio:acao.
func partirPermissao(permissao string) (dominio, subdominio string, ok bool) {
	partes := strings.Split(permissao, ":")
	if len(partes) != 3 || partes[0] == "" || partes[1] == "" || partes[2] == "" {
		return "", "", false
	}
	return partes[0], partes[1], true
}

func montarDominios(porDominio map[string]map[string][]AcaoDto) []DominioDto {
	dominios := make([]string, 0, len(porDominio))
	for dominio := range porDominio {
		dominios = append(dominios, dominio)
	}
	sort.Strings(dominios)
	lista := make([]DominioDto, 0, len(dominios))
	for _, dominio := range dominios {
		subdominios := make([]string, 0, len(porDominio[dominio]))
		for subdominio := range porDominio[dominio] {
			subdominios = append(subdominios, subdominio)
		}
		sort.Strings(subdominios)
		grupos := make([]SubdominioAcoesDto, 0, len(subdominios))
		for _, subdominio := range subdominios {
			acoes := porDominio[dominio][subdominio]
			sort.Slice(acoes, func(i, j int) bool {
				if acoes[i].Rota != acoes[j].Rota {
					return acoes[i].Rota < acoes[j].Rota
				}
				return acoes[i].Metodo < acoes[j].Metodo
			})
			grupos = append(grupos, SubdominioAcoesDto{Subdominio: subdominio, Acoes: acoes})
		}
		lista = append(lista, DominioDto{Dominio: dominio, Subdominios: grupos})
	}
	return lista
}

// --- CONTRATO — GET /api/system/errors --------------------------------------
//
// Mapa COMPLETO dos erros possíveis do sistema (doc 04): o registro global do
// rest_err alimentado pelo init() de cada subdomínio — nada é hardcoded aqui.

// ErrosResponseDto agrupa os erros por domínio/subdomínio.
type ErrosResponseDto struct {
	Erros []rest_err.GrupoErros `json:"erros"`
}

// NovoErrosResponseDto devolve o mapa completo do registro global em ordem
// determinística, sem duplicar entradas (dedupe já feito pelo rest_err).
func NovoErrosResponseDto(grupos []rest_err.GrupoErros) ErrosResponseDto {
	if grupos == nil {
		grupos = []rest_err.GrupoErros{}
	}
	return ErrosResponseDto{Erros: grupos}
}
