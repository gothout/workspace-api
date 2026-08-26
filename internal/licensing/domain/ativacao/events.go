package ativacao

import (
	"fmt"

	modelativacao "workspace-api/internal/licensing/model/ativacao"
)

// Catálogo de EVENTOS de auditoria do subdomínio — alimenta GET
// /api/system/eventos. Ação nova SEM entrada aqui reprova em teste/boot.
var catalogoEventos = []EventoMeta{
	{Acao: "ativar", Descricao: "Módulo ativado no workspace dentro das licenças da organization.", Campos: []string{"workspace_uuid_alvo", "modulo_slug"}},
	{Acao: "desativar", Descricao: "Módulo desativado no workspace; usuários perdem o acesso imediatamente.", Campos: []string{"workspace_uuid_alvo", "modulo_slug"}},
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
		modelativacao.Dominio, modelativacao.Subdominio, acao, modelativacao.Subdominio))
}
