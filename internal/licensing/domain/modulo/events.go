package modulo

import (
	"fmt"

	modelmodulo "workspace-api/internal/licensing/model/modulo"
)

// Catálogo de EVENTOS de auditoria do subdomínio — o mapping que alimenta
// GET /api/system/eventos. Ação nova emitida pelo service SEM entrada aqui
// reprova em teste/boot: o auditar() chama validarAcaoCatalogada e PANICA.
var catalogoEventos = []EventoMeta{
	{Acao: "criar", Descricao: "Módulo criado no catálogo da plataforma.", Campos: []string{"slug"}},
	{Acao: "editar", Descricao: "Dados do módulo alterados; desativação/ativação indicadas nos campos opcionais.", Campos: []string{"slug", "inativo", "ativado"}},
	{Acao: "remover", Descricao: "Módulo removido do catálogo; o slug NÃO se libera para outro registro.", Campos: []string{"slug"}},
}

// EventoMeta — metadados de UM evento de auditoria no formato nativo do
// subdomínio. O bootstrap converte para a forma única da aplicação catalogo.
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
		modelmodulo.Dominio, modelmodulo.Subdominio, acao, modelmodulo.Subdominio))
}
