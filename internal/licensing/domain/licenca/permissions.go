package licenca

import "net/http"

// Permissões granulares do subdomínio — string dominio:subdominio:acao.
// ATRIBUIR/REMOVER são EXCLUSIVAS do super_admin (escrita cruza organizations);
// leitura da PRÓPRIA organization vai aos papéis admin no seed, e a leitura
// de qualquer organization (ler_plataforma) fica com o super_admin.
const (
	PermAtribuir       = "licensing:licenca:atribuir"
	PermRemover        = "licensing:licenca:remover"
	PermLer            = "licensing:licenca:ler"
	PermLerPlataforma  = "licensing:licenca:ler_plataforma"
)

// PermissaoMeta — metadados da permissão para o catálogo consultável (doc 03).
type PermissaoMeta struct {
	Permissao string
	Descricao string
	Rotas     []RotaMeta
	GrupoMenu string
}

// RotaMeta — UM par rota+método.
type RotaMeta struct {
	Rota   string
	Metodo string
}

// Catalogo devolve TODAS as permissões do subdomínio com metadados.
func Catalogo() []PermissaoMeta {
	base := "/api/domain/licensing/organizations/{organization_uuid}/licencas"
	return []PermissaoMeta{
		{Permissao: PermAtribuir, Descricao: "Conceder licença de módulo a uma organization (super_admin)", Rotas: []RotaMeta{{Rota: base, Metodo: http.MethodPost}}, GrupoMenu: "Licensing · Licenças"},
		{Permissao: PermRemover, Descricao: "Revogar licença de módulo de uma organization (super_admin)", Rotas: []RotaMeta{{Rota: base + "/{uuid}", Metodo: http.MethodDelete}}, GrupoMenu: "Licensing · Licenças"},
		{Permissao: PermLer, Descricao: "Listar as licenças da PRÓPRIA organization", Rotas: []RotaMeta{{Rota: base, Metodo: http.MethodGet}}, GrupoMenu: "Licensing · Licenças"},
		{Permissao: PermLerPlataforma, Descricao: "Listar licenças de QUALQUER organization (super_admin)", Rotas: []RotaMeta{{Rota: base, Metodo: http.MethodGet}}, GrupoMenu: "Licensing · Licenças"},
	}
}
