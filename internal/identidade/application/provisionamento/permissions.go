package provisionamento

import "net/http"

// PermExecutar é a permissão granular da rota de provisionamento — função da
// PLATAFORMA: nenhum papel humano do seed a recebe (o super_admin é coberto
// pelo curinga global *:*). Papel customizado que a conceda ainda assim não
// atravessa tenants: a criação cross-tenant reconfere a posse de *:* dentro
// do subdomínio workspace (UX4).
const (
	PermExecutar = "identidade:provisionamento:executar"
)

// PermissaoMeta — metadados da permissão para o catálogo consultável (doc 03);
// mesma forma em todos os emissores, agregada no bootstrap.
type PermissaoMeta struct {
	Permissao string     // valor exato exigido pela rota
	Descricao string     // PT-BR: o que a permissão libera
	Rotas     []RotaMeta // pares rota+método que exigem esta permissão
	GrupoMenu string     // agrupamento sugerido para o menu do front-end
}

// RotaMeta — UM par rota+método; a árvore do endpoint emite uma ação por par.
type RotaMeta struct {
	Rota   string
	Metodo string
}

const grupoMenu = "Identidade · Provisionamento"

// Catalogo devolve TODAS as permissões da aplicação com metadados.
func Catalogo() []PermissaoMeta {
	return []PermissaoMeta{
		{
			Permissao: PermExecutar,
			Descricao: "Provisionar o admin inicial e o workspace inicial de uma organization (restrito à plataforma)",
			Rotas: []RotaMeta{
				{Rota: "/api/domain/identidade/organizations/{uuid}/provisionamento", Metodo: http.MethodPost},
			},
			GrupoMenu: grupoMenu,
		},
	}
}
