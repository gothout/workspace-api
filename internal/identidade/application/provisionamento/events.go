package provisionamento

import "fmt"

// Catálogo de EVENTOS de auditoria da aplicação (doc 04) — mesmo padrão do
// errors.go e do events.go dos subdomínios/aplicação auth. O provisionar em
// si é composto por escritas que os SUBDOMÍNIOS já auditam (workspace.criar,
// user.criar, user.atribuir_papel); o evento daqui é o registro da ORQUESTRAÇÃO:
// quem provisionou, para qual organization, com quais identificadores.
//
// E-mail entra SEMPRE mascarado (pkg/pii); senha NUNCA entra — nem em claro,
// nem hasheada.
var catalogoEventos = []EventoMeta{
	{Acao: "provisionar", Descricao: "Organization provisionada pela plataforma: admin inicial criado/reconhecido e workspace inicial atribuído com o papel admin_organization.", Campos: []string{"slug", "admin_uuid", "workspace_uuid", "email_mascarado"}},
}

// EventoMeta — metadados de UM evento de auditoria no formato nativo da
// aplicação. O bootstrap converte para a forma única da aplicação catalogo.
type EventoMeta struct {
	Acao      string   // valor estável emitido no campo acao
	Descricao string   // PT-BR: o que o evento significa
	Campos    []string // chaves extras do payload (além das padrão); vazias = só padrão
}

// CatalogoEventos devolve TODOS os eventos desta aplicação com metadados.
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
		Dominio, Subdominio, acao, Subdominio))
}
