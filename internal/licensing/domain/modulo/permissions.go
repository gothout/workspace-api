package modulo

import "net/http"

// Permissões granulares do subdomínio — string dominio:subdominio:acao.
// Escrita é EXCLUSIVA do super_admin (curinga *:*); leitura vai aos papéis de
// administração no seed — ver cmd/bootstrap/seed.go.
const (
	PermCriar   = "licensing:modulo:criar"
	PermLer     = "licensing:modulo:ler"
	PermEditar  = "licensing:modulo:editar"
	PermRemover = "licensing:modulo:remover"
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
	return []PermissaoMeta{
		{Permissao: PermCriar, Descricao: "Criar módulo no catálogo da plataforma (super_admin)", Rotas: []RotaMeta{{Rota: "/api/domain/licensing/modulos", Metodo: http.MethodPost}}, GrupoMenu: "Licensing · Módulos"},
		{Permissao: PermLer, Descricao: "Listar e consultar o catálogo de módulos", Rotas: []RotaMeta{{Rota: "/api/domain/licensing/modulos", Metodo: http.MethodGet}, {Rota: "/api/domain/licensing/modulos/{uuid}", Metodo: http.MethodGet}}, GrupoMenu: "Licensing · Módulos"},
		{Permissao: PermEditar, Descricao: "Editar dados do módulo e alternar ativo/inativo (super_admin)", Rotas: []RotaMeta{{Rota: "/api/domain/licensing/modulos/{uuid}", Metodo: http.MethodPatch}}, GrupoMenu: "Licensing · Módulos"},
		{Permissao: PermRemover, Descricao: "Remover módulo sem licenças vivas (super_admin)", Rotas: []RotaMeta{{Rota: "/api/domain/licensing/modulos/{uuid}", Metodo: http.MethodDelete}}, GrupoMenu: "Licensing · Módulos"},
	}
}
