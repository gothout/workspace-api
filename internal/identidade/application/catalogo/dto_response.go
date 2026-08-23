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

// --- CONTRATO — GET /api/system/eventos --------------------------------------
//
// Mapa COMPLETO dos eventos de auditoria do sistema (doc 04): o agregado dos
// CatalogoEventos() de todos os subdomínios, entregue pelo bootstrap — nada
// é hardcoded aqui.

// EventosResponseDto agrupa os eventos por domínio/subdomínio.
type EventosResponseDto struct {
	Eventos []GrupoEventosDto `json:"eventos"`
}

// GrupoEventosDto reúne os eventos de um par dominio/subdominio.
type GrupoEventosDto struct {
	Dominio    string      `json:"dominio"`
	Subdominio string      `json:"subdominio"`
	Eventos    []EventoDto `json:"eventos"`
}

// EventoDto é UM evento de auditoria catalogado.
type EventoDto struct {
	Acao      string   `json:"acao"`
	Descricao string   `json:"descricao"`
	Campos    []string `json:"campos"`
}

// NovoEventosResponseDto monta o mapa a partir do catálogo AGREGADO, com
// saída determinística: grupos por (dominio, subdominio) e eventos por ação,
// tudo ordenado.
func NovoEventosResponseDto(catalogo []EventoMeta) EventosResponseDto {
	porGrupo := map[string]map[string][]EventoMeta{}
	for _, meta := range catalogo {
		if porGrupo[meta.Dominio] == nil {
			porGrupo[meta.Dominio] = map[string][]EventoMeta{}
		}
		porGrupo[meta.Dominio][meta.Subdominio] = append(porGrupo[meta.Dominio][meta.Subdominio], meta)
	}
	dominios := make([]string, 0, len(porGrupo))
	for dominio := range porGrupo {
		dominios = append(dominios, dominio)
	}
	sort.Strings(dominios)
	grupos := make([]GrupoEventosDto, 0)
	for _, dominio := range dominios {
		subdominios := make([]string, 0, len(porGrupo[dominio]))
		for subdominio := range porGrupo[dominio] {
			subdominios = append(subdominios, subdominio)
		}
		sort.Strings(subdominios)
		for _, subdominio := range subdominios {
			nativos := porGrupo[dominio][subdominio]
			eventos := make([]EventoDto, 0, len(nativos))
			for _, meta := range nativos {
				campos := meta.Campos
				if campos == nil {
					campos = []string{}
				}
				eventos = append(eventos, EventoDto{
					Acao:      meta.Acao,
					Descricao: meta.Descricao,
					Campos:    campos,
				})
			}
			sort.Slice(eventos, func(i, j int) bool { return eventos[i].Acao < eventos[j].Acao })
			grupos = append(grupos, GrupoEventosDto{
				Dominio:    dominio,
				Subdominio: subdominio,
				Eventos:    eventos,
			})
		}
	}
	return EventosResponseDto{Eventos: grupos}
}
