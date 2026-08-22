package organization

import "net/http"

// Permissões granulares do subdomínio — string dominio:subdominio:acao.
// EXCEÇÃO do seed (doc 03): admin_organization só recebe gerenciar_apikeys —
// o curinga dele cobre workspace/user; administrar organizations é
// super_admin/suporte auditado.
const (
	PermCriar            = "identidade:organization:criar"
	PermLer              = "identidade:organization:ler"
	PermEditar           = "identidade:organization:editar"
	PermRemover          = "identidade:organization:remover"
	PermGerenciarDominio = "identidade:organization:gerenciar_dominio"
	PermGerenciarApikeys = "identidade:organization:gerenciar_apikeys"
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

const grupoMenu = "Identidade · Organizations"

// Catalogo devolve TODAS as permissões do subdomínio com metadados.
// Sem ele o subdomínio não aparece no endpoint de permissões e o checklist não fecha.
func Catalogo() []PermissaoMeta {
	return []PermissaoMeta{
		{
			Permissao: PermCriar,
			Descricao: "Criar organization na plataforma",
			Rotas:     []RotaMeta{{Rota: "/api/domain/identidade/organizations", Metodo: http.MethodPost}},
			GrupoMenu: grupoMenu,
		},
		{
			Permissao: PermLer,
			Descricao: "Listar e consultar organizations",
			Rotas: []RotaMeta{
				{Rota: "/api/domain/identidade/organizations", Metodo: http.MethodGet},
				{Rota: "/api/domain/identidade/organizations/{uuid}", Metodo: http.MethodGet},
			},
			GrupoMenu: grupoMenu,
		},
		{
			Permissao: PermEditar,
			Descricao: "Editar dados da organization e reativá-la",
			Rotas: []RotaMeta{
				{Rota: "/api/domain/identidade/organizations/{uuid}", Metodo: http.MethodPatch},
				{Rota: "/api/domain/identidade/organizations/{uuid}/acoes/reativar", Metodo: http.MethodPost},
			},
			GrupoMenu: grupoMenu,
		},
		{
			Permissao: PermRemover,
			Descricao: "Remover organization",
			Rotas:     []RotaMeta{{Rota: "/api/domain/identidade/organizations/{uuid}", Metodo: http.MethodDelete}},
			GrupoMenu: grupoMenu,
		},
		{
			Permissao: PermGerenciarDominio,
			Descricao: "Apontar ou remover o domínio custom white-label da organization",
			Rotas: []RotaMeta{
				{Rota: "/api/domain/identidade/organizations/{uuid}/dominio", Metodo: http.MethodPut},
				{Rota: "/api/domain/identidade/organizations/{uuid}/dominio", Metodo: http.MethodDelete},
			},
			GrupoMenu: grupoMenu,
		},
		{
			Permissao: PermGerenciarApikeys,
			Descricao: "Gerir as chaves de API da organization",
			Rotas: []RotaMeta{
				{Rota: "/api/domain/identidade/organizations/{uuid}/api-keys", Metodo: http.MethodPost},
				{Rota: "/api/domain/identidade/organizations/{uuid}/api-keys", Metodo: http.MethodGet},
				{Rota: "/api/domain/identidade/organizations/{uuid}/api-keys/{apikey_uuid}", Metodo: http.MethodDelete},
			},
			GrupoMenu: grupoMenu,
		},
	}
}
