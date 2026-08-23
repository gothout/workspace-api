package auth

import "fmt"

// Catálogo de EVENTOS de auditoria da aplicação (doc 04) — o mapping que
// alimenta GET /api/system/eventos. Mesmo padrão do errors.go (code estável):
// ação estável + descrição PT-BR + campos do payload além dos campos padrão
// (dominio, subdominio, acao, success, ray_trace).
//
// A ação nova emitida pelo service SEM entrada aqui reprova em teste/boot:
// o auditar() chama validarAcaoCatalogada e PANICA — evento fora do catálogo
// é vocabulário que o front não conhece, nunca pode nascer silencioso.
//
// E-mail entra SEMPRE mascarado (pkg/pii) e os uuids de usuário/organization
// viajam como detalhes (a identidade do token ainda não existe no login —
// é o resultado dele).
var catalogoEventos = []EventoMeta{
	{Acao: "login", Descricao: "Tentativa de login: sucesso carrega os identificadores da sessão aberta; falha carrega e-mail mascarado (e campos de lockout quando preso).", Campos: []string{"email", "user_uuid", "organization_uuid", "limite_excedido", "espera_seg"}},
	{Acao: "refresh", Descricao: "Renovação de sessão com rotação: o jti anterior é revogado antes do par novo.", Campos: []string{"user_uuid", "organization_uuid", "jti_anterior"}},
	{Acao: "logout", Descricao: "Sessão encerrada por jti — idempotente (repetir logout do mesmo token é sucesso).", Campos: []string{"user_uuid", "organization_uuid", "jti"}},
}

// EventoMeta — metadados de UM evento de auditoria no formato nativo da
// aplicação. O bootstrap converte para a forma única da aplicação catalogo
// (tipos homônimos por pacote, como os PermissaoMeta).
type EventoMeta struct {
	Acao      string   // valor estável emitido no campo acao
	Descricao string   // PT-BR: o que o evento significa
	Campos    []string // chaves extras do payload (além das padrão); vazias = só padrão
}

// CatalogoEventos devolve TODOS os eventos desta aplicação com metadados.
// Sem ele ela não aparece em GET /api/system/eventos.
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
// rest_err recusar code duplicado.
func validarAcaoCatalogada(acao string) {
	if _, ok := acoesCatalogadas[acao]; ok {
		return
	}
	panic(fmt.Sprintf("evento de auditoria não catalogado: %s.%s.%s — declare-o no events.go do subdomínio (%s/events.go)",
		Dominio, Subdominio, acao, Subdominio))
}
