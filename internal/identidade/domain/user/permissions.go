package user

import "net/http"

// Permissões granulares do subdomínio — string dominio:subdominio:acao.
// Mesmas strings do seed (doc 03): identidade:user:* é do admin_organization
// e do admin_workspace; identidade:user:ler também é do somente_leitura —
// divergência entre seed e PermX é bug de contrato.
const (
	PermCriar         = "identidade:user:criar"
	PermLer           = "identidade:user:ler"
	PermEditar        = "identidade:user:editar"
	PermRemover       = "identidade:user:remover"
	PermAtribuirPapel = "identidade:user:atribuir_papel"
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

const grupoMenu = "Identidade · Usuários"

const prefixoPublico = "/api/domain/identidade/users"

// prefixoPublicoPapeis é o path da listagem de papéis globais (fora do CRUD
// de usuários) — exigida por PermAtribuirPapel: quem atribui papel precisa da
// referência para montar o Select.
const prefixoPublicoPapeis = "/api/domain/identidade/user/papeis"

// Catalogo devolve TODAS as permissões do subdomínio com metadados.
// Sem ele o subdomínio não aparece no endpoint de permissões e o checklist não fecha.
func Catalogo() []PermissaoMeta {
	return []PermissaoMeta{
		{
			Permissao: PermCriar,
			Descricao: "Criar usuário na organization",
			Rotas:     []RotaMeta{{Rota: prefixoPublico, Metodo: http.MethodPost}},
			GrupoMenu: grupoMenu,
		},
		{
			Permissao: PermLer,
			Descricao: "Listar e consultar usuários da organization e suas atribuições",
			Rotas: []RotaMeta{
				{Rota: prefixoPublico, Metodo: http.MethodGet},
				{Rota: prefixoPublico + "/{uuid}", Metodo: http.MethodGet},
				{Rota: prefixoPublico + "/{uuid}/atribuicoes", Metodo: http.MethodGet},
			},
			GrupoMenu: grupoMenu,
		},
		{
			Permissao: PermEditar,
			Descricao: "Editar dados do usuário e seu estado",
			Rotas:     []RotaMeta{{Rota: prefixoPublico + "/{uuid}", Metodo: http.MethodPatch}},
			GrupoMenu: grupoMenu,
		},
		{
			Permissao: PermRemover,
			Descricao: "Remover usuário da organization",
			Rotas:     []RotaMeta{{Rota: prefixoPublico + "/{uuid}", Metodo: http.MethodDelete}},
			GrupoMenu: grupoMenu,
		},
		{
			Permissao: PermAtribuirPapel,
			Descricao: "Atribuir e remover papéis do usuário em workspaces e listar os papéis disponíveis — dar poder a alguém não é editar um campo",
			Rotas: []RotaMeta{
				{Rota: prefixoPublico + "/{uuid}/atribuicoes", Metodo: http.MethodPost},
				{Rota: prefixoPublico + "/{uuid}/atribuicoes/{atribuicaoUuid}", Metodo: http.MethodDelete},
				{Rota: prefixoPublicoPapeis, Metodo: http.MethodGet},
			},
			GrupoMenu: grupoMenu,
		},
	}
}
