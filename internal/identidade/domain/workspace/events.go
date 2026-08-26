package workspace

import (
	"fmt"

	modelworkspace "workspace-api/internal/identidade/model/workspace"
)

// Catálogo de EVENTOS de auditoria do subdomínio (doc 04) — o mapping que
// alimenta GET /api/system/eventos. Mesmo padrão do errors.go (code estável)
// e do permissions.go (Catalogo()): ação estável + descrição PT-BR + campos
// do payload além dos campos de identidade padrão (dominio, subdominio, acao,
// success, ray_trace, organization_uuid/workspace_uuid/user_uuid conforme o
// evento).
//
// Ação nova emitida pelo service SEM entrada aqui reprova em teste/boot:
// o auditar() chama validarAcaoCatalogada e PANICA — evento fora do catálogo
// é vocabulário que o front não conhece, nunca pode nascer silencioso.
var catalogoEventos = []EventoMeta{
	{Acao: "criar", Descricao: "Workspace criado; criação cross-tenant da plataforma indicada em cross_tenant (UX4).", Campos: []string{"slug", "cross_tenant"}},
	{Acao: "editar", Descricao: "Dados do workspace alterados; inativação indicada no campo opcional inativo.", Campos: []string{"slug", "inativo"}},
	{Acao: "reativar", Descricao: "Workspace reativado; resolução pelo Host volta imediatamente (cache invalidado).", Campos: []string{"slug"}},
	{Acao: "remover", Descricao: "Workspace removido; o slug NÃO se libera para outro tenant.", Campos: []string{"slug"}},
	{Acao: "cascata_organization_inativada", Descricao: "Workspaces suspensos em cascata pela inativação/remoção da organization dona.", Campos: []string{"quantidade"}},
}

// EventoMeta — metadados de UM evento de auditoria no formato nativo do
// subdomínio. O bootstrap converte para a forma única da aplicação catalogo
// (tipos homônimos por pacote, como os PermissaoMeta).
type EventoMeta struct {
	Acao      string   // valor estável emitido no campo acao
	Descricao string   // PT-BR: o que o evento significa
	Campos    []string // chaves extras do payload (além das de identidade); vazias = só identidade
}

// CatalogoEventos devolve TODOS os eventos do subdomínio com metadados.
// Sem ele o subdomínio não aparece em GET /api/system/eventos.
func CatalogoEventos() []EventoMeta { return catalogoEventos }

// acoesCatalogadas é o índice de consulta rápida usado pela validação.
var acoesCatalogadas = func() map[string]struct{} {
	m := make(map[string]struct{}, len(catalogoEventos))
	for _, ev := range catalogoEventos {
		m[ev.Acao] = struct{}{}
	}
	return m
}()

// validarAcaoCatalogada reprova em teste/boot ação sem entrada no catálogo:
// pânico com mensagem acionável — mesmo espírito do RegistroCatalogo do
// rest_err recusar code duplicado. Nunca deve disparar em produção porque
// nenhum caminho novo fecha checklist sem events.go atualizado.
func validarAcaoCatalogada(acao string) {
	if _, ok := acoesCatalogadas[acao]; ok {
		return
	}
	panic(fmt.Sprintf("evento de auditoria não catalogado: %s.%s.%s — declare-o no events.go do subdomínio (%s/events.go)",
		modelworkspace.Dominio, modelworkspace.Subdominio, acao, modelworkspace.Subdominio))
}
