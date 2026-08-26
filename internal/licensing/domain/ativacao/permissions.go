package ativacao

import "net/http"

// Permissões granulares do subdomínio — string dominio:subdominio:acao.
// Ativar/desativar é decisão DA ORGANIZATION (admin_organization no seed);
// leitura vai também ao admin_workspace.
const (
	PermCriar   = "licensing:ativacao:criar"
	PermRemover = "licensing:ativacao:remover"
	PermLer     = "licensing:ativacao:ler"
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
	base := "/api/domain/licensing/workspaces/{workspace_uuid}/modulos"
	return []PermissaoMeta{
		{Permissao: PermCriar, Descricao: "Ativar módulo licenciado num workspace da organization", Rotas: []RotaMeta{{Rota: base, Metodo: http.MethodPost}}, GrupoMenu: "Licensing · Ativações"},
		{Permissao: PermLer, Descricao: "Listar os módulos ativados no workspace", Rotas: []RotaMeta{{Rota: base, Metodo: http.MethodGet}}, GrupoMenu: "Licensing · Ativações"},
		{Permissao: PermRemover, Descricao: "Desativar módulo no workspace", Rotas: []RotaMeta{{Rota: base + "/{uuid}", Metodo: http.MethodDelete}}, GrupoMenu: "Licensing · Ativações"},
	}
}
