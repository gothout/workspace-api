package tarefa

import (
	"fmt"

	modeltarefa "workspace-api/internal/todolist/model/tarefa"
)

// Catálogo de EVENTOS de auditoria do subdomínio — alimenta GET
// /api/system/eventos. Ação nova SEM entrada aqui reprova em teste/boot.
var catalogoEventos = []EventoMeta{
	{Acao: "criar", Descricao: "Tarefa criada no todolist do workspace.", Campos: []string{"titulo"}},
	{Acao: "editar", Descricao: "Dados da tarefa alterados; conclusão/reabertura nos campos opcionais.", Campos: []string{"titulo", "concluida", "reaberta"}},
	{Acao: "remover", Descricao: "Tarefa removida do todolist.", Campos: []string{"titulo"}},
}

// EventoMeta — metadados de UM evento de auditoria no formato nativo.
type EventoMeta struct {
	Acao      string
	Descricao string
	Campos    []string
}

// CatalogoEventos devolve TODOS os eventos do subdomínio com metadados.
func CatalogoEventos() []EventoMeta { return catalogoEventos }

// acoesCatalogadas é o índice de consulta rápida usado pela validação.
var acoesCatalogadas = func() map[string]struct{} {
	m := make(map[string]struct{}, len(catalogoEventos))
	for _, ev := range catalogoEventos {
		m[ev.Acao] = struct{}{}
	}
	return m
}()

// validarAcaoCatalogada reprova em teste/boot ação sem entrada no catálogo.
func validarAcaoCatalogada(acao string) {
	if _, ok := acoesCatalogadas[acao]; ok {
		return
	}
	panic(fmt.Sprintf("evento de auditoria não catalogado: %s.%s.%s — declare-o no events.go do subdomínio (%s/events.go)",
		modeltarefa.Dominio, modeltarefa.Subdominio, acao, modeltarefa.Subdominio))
}
