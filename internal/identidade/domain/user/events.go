package user

import (
	"fmt"

	modeluser "workspace-api/internal/identidade/model/user"
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
//
// E-mail entra SEMPRE mascarado (pkg/pii) — o catálogo descreve a chave, não
// o conteúdo: PII nunca repousa crua em trilha de auditoria.
var catalogoEventos = []EventoMeta{
	{Acao: "criar", Descricao: "Usuário criado na organization.", Campos: []string{"email"}},
	{Acao: "editar", Descricao: "Dados do usuário alterados; inativação encerra as sessões abertas.", Campos: []string{"status", "sessoes_encerradas"}},
	{Acao: "remover", Descricao: "Usuário removido com sessões encerradas.", Campos: []string{"email", "sessoes_encerradas"}},
	{Acao: "encerrar_sessao", Descricao: "Sessão (refresh token) revogada por jti — logout, rotação ou cascata.", Campos: []string{"jti"}},
	{Acao: "atribuir_papel", Descricao: "Papel atribuído ao usuário num workspace da organization.", Campos: []string{"atribuicao_uuid", "workspace_uuid", "papel_uuid"}},
	{Acao: "remover_atribuicao", Descricao: "Atribuição de papel removida do usuário.", Campos: []string{"atribuicao_uuid"}},
	{Acao: "suporte_concedido", Descricao: "[SUPORTE] acesso concedido sem atribuição direta (super_admin/admin_organization), auditado com o papel usado.", Campos: []string{"papel", "workspace_uuid"}},
	{Acao: "cascata_organization_inativada", Descricao: "Sessões dos usuários encerradas em cascata pela inativação/remoção da organization.", Campos: []string{"quantidade"}},
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
		modeluser.Dominio, modeluser.Subdominio, acao, modeluser.Subdominio))
}
