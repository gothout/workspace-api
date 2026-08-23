package logs

import "net/http"

// Permissões granulares da aplicação — string dominio:subdominio:acao.
// Duas ações porque o RECORTE é parte da permissão: quem só tem PermLer lê
// o próprio workspace resolvido no ctx; PermLerOrganization amplia para a
// organization inteira. A plataforma (super_admin) passa pelo curinga *:*.
const (
	PermLer             = "identidade:logs:ler"
	PermLerOrganization = "identidade:logs:ler_organization"
)

// PermissaoMeta/RotaMeta — mesmos metadados dos permissions.go dos
// subdomínios; o bootstrap converte para a forma única da aplicação catalogo
// (tipos homônimos por pacote, conversão explícita no agregador).
type PermissaoMeta struct {
	Permissao string
	Descricao string
	Rotas     []RotaMeta
	GrupoMenu string
}

type RotaMeta struct {
	Rota   string
	Metodo string
}

func rotasDeLeitura() []RotaMeta {
	return []RotaMeta{
		{Rota: "/api/application/identidade/logs/auditoria", Metodo: http.MethodGet},
		{Rota: "/api/application/identidade/logs/acesso", Metodo: http.MethodGet},
		{Rota: "/api/application/identidade/logs/erros", Metodo: http.MethodGet},
		{Rota: "/api/application/identidade/logs/opcoes-filtro", Metodo: http.MethodGet},
	}
}

// Catalogo devolve TODAS as permissões da aplicação com metadados — alimenta
// a árvore de permissões do front-end via agregador do bootstrap.
func Catalogo() []PermissaoMeta {
	return []PermissaoMeta{
		{
			Permissao: PermLer,
			Descricao: "Consultar os logs (auditoria, acesso e erros) do workspace ativo",
			Rotas:     rotasDeLeitura(),
			GrupoMenu: "Identidade · Logs",
		},
		{
			Permissao: PermLerOrganization,
			Descricao: "Consultar os logs de toda a organization (todos os workspaces dela)",
			Rotas:     rotasDeLeitura(),
			GrupoMenu: "Identidade · Logs",
		},
	}
}
