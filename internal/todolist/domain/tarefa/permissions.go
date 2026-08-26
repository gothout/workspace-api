package tarefa

import "net/http"

// Permissões granulares do subdomínio — string dominio:subdominio:acao.
// Exigidas DENTRO do módulo: o RequireAplicacao("todolist") garante o acesso
// ao app, estas garantem a ação dentro dele.
const (
	PermCriar   = "todolist:tarefa:criar"
	PermLer     = "todolist:tarefa:ler"
	PermEditar  = "todolist:tarefa:editar"
	PermRemover = "todolist:tarefa:remover"
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
	base := "/api/domain/todolist/tasks"
	return []PermissaoMeta{
		{Permissao: PermCriar, Descricao: "Criar tarefa no todolist do workspace", Rotas: []RotaMeta{{Rota: base, Metodo: http.MethodPost}}, GrupoMenu: "Todolist"},
		{Permissao: PermLer, Descricao: "Listar e consultar tarefas do todolist", Rotas: []RotaMeta{{Rota: base, Metodo: http.MethodGet}, {Rota: base + "/{uuid}", Metodo: http.MethodGet}}, GrupoMenu: "Todolist"},
		{Permissao: PermEditar, Descricao: "Editar tarefa e alternar concluída/pendente", Rotas: []RotaMeta{{Rota: base + "/{uuid}", Metodo: http.MethodPatch}}, GrupoMenu: "Todolist"},
		{Permissao: PermRemover, Descricao: "Remover tarefa do todolist", Rotas: []RotaMeta{{Rota: base + "/{uuid}", Metodo: http.MethodDelete}}, GrupoMenu: "Todolist"},
	}
}
