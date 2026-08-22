package workspace

import "net/http"

// Permissões granulares do subdomínio — string dominio:subdominio:acao.
// Mesmas strings do seed (doc 03): o curinga identidade:workspace:* é do
// admin_organization e identidade:workspace:editar também é do
// admin_workspace — divergência entre seed e PermX é bug de contrato.
const (
	PermCriar   = "identidade:workspace:criar"
	PermLer     = "identidade:workspace:ler"
	PermEditar  = "identidade:workspace:editar"
	PermRemover = "identidade:workspace:remover"
)

// PermissaoMeta — metadados da permissão para o catálogo consultável (doc 03).
// Mesma forma em todos os subdomínios; o bootstrap agrega os Catalogo() no
// registro único do identidade/application/catalogo (domain não importa
// application — regra 3).
type PermissaoMeta struct {
	Permissao string     // valor exato exigido pela rota
	Descricao string     // PT-BR: o que a permissão libera
	Rotas     []RotaMeta // pares rota+método que exigem esta permissão
	GrupoMenu string     // agrupamento sugerido para o menu do front-end
}

// RotaMeta — UM par rota+método; a árvore do endpoint emite uma ação por par.
type RotaMeta struct {
	Rota   string // path com {uuid} onde couber
	Metodo string // método HTTP
}

const grupoMenu = "Identidade · Workspaces"

// Catalogo devolve TODAS as permissões do subdomínio com metadados.
// Sem ele o subdomínio não aparece no endpoint de permissões e o checklist não fecha.
func Catalogo() []PermissaoMeta {
	return []PermissaoMeta{
		{
			Permissao: PermCriar,
			Descricao: "Criar workspace na organization",
			Rotas:     []RotaMeta{{Rota: "/api/domain/identidade/workspaces", Metodo: http.MethodPost}},
			GrupoMenu: grupoMenu,
		},
		{
			Permissao: PermLer,
			Descricao: "Listar e consultar workspaces da organization",
			Rotas: []RotaMeta{
				{Rota: "/api/domain/identidade/workspaces", Metodo: http.MethodGet},
				{Rota: "/api/domain/identidade/workspaces/{uuid}", Metodo: http.MethodGet},
			},
			GrupoMenu: grupoMenu,
		},
		{
			Permissao: PermEditar,
			Descricao: "Editar dados do workspace, inativá-lo e reativá-lo",
			Rotas: []RotaMeta{
				{Rota: "/api/domain/identidade/workspaces/{uuid}", Metodo: http.MethodPatch},
				{Rota: "/api/domain/identidade/workspaces/{uuid}/acoes/reativar", Metodo: http.MethodPost},
			},
			GrupoMenu: grupoMenu,
		},
		{
			Permissao: PermRemover,
			Descricao: "Remover workspace",
			Rotas:     []RotaMeta{{Rota: "/api/domain/identidade/workspaces/{uuid}", Metodo: http.MethodDelete}},
			GrupoMenu: grupoMenu,
		},
	}
}
