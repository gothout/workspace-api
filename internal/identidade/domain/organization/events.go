package organization

import (
	"fmt"

	orgmodel "workspace-api/internal/identidade/model/organization"
)

// Catálogo de EVENTOS de auditoria do subdomínio (doc 04) — o mapping que
// alimenta GET /api/system/eventos. Mesmo padrão do errors.go (code estável)
// e do permissions.go (Catalogo()): ação estável + descrição PT-BR + campos
// do payload além dos campos de identidade padrão (dominio, subdominio, acao,
// success, ray_trace, organization_uuid/user_uuid conforme o evento).
//
// Ação nova emitida pelo service SEM entrada aqui reprova em teste/boot:
// o auditar() chama validarAcaoCatalogada e PANICA — evento fora do catálogo
// é vocabulário que o front não conhece, nunca pode nascer silencioso.
var catalogoEventos = []EventoMeta{
	{Acao: "criar", Descricao: "Organization criada na plataforma."},
	{Acao: "editar", Descricao: "Dados da organization alterados; inativação carrega a cascata de acessos encerrados.", Campos: []string{"status", "sessoes_encerradas", "apikeys_revogadas"}},
	{Acao: "reativar", Descricao: "Organization reativada; workspaces seguem suspensos até ação explícita sobre cada um."},
	{Acao: "remover", Descricao: "Organization removida com cascata completa (workspaces suspensos, chaves revogadas, sessões encerradas).", Campos: []string{"sessoes_encerradas", "apikeys_revogadas"}},
	{Acao: "definir_dominio", Descricao: "Domínio custom white-label apontado para a organization."},
	{Acao: "remover_dominio", Descricao: "Domínio custom da organization removido (o valor não se libera para outra tenant)."},
	{Acao: "criar_apikey", Descricao: "Chave de API criada para a organization; o segredo em claro nunca entra no evento.", Campos: []string{"apikey_uuid", "escopo_organization"}},
	{Acao: "revogar_apikey", Descricao: "Chave de API da organization revogada.", Campos: []string{"apikey_uuid"}},
	{Acao: "cascata_organization_inativada", Descricao: "Cascata de inativação/remoção encerrou acessos vivos antes de persistir o novo estado.", Campos: []string{"apikeys_revogadas", "sessoes_encerradas"}},
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
		orgmodel.Dominio, orgmodel.Subdominio, acao, orgmodel.Subdominio))
}
