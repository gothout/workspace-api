// Revogação persistida do refresh token: a interface é declarada AQUI
// (infra não importa outro infra — regra 2 de agents/01) e a implementação,
// ligada no cmd/bootstrap, consulta identidade_user_refresh_token (F4).
//
// Ausência de verificador ligado = "nada revogado" — o template sobe sem o
// adaptador e a revogação passa a valer quando a tabela existir. A denylist
// Redis (evolução futura, issue #8) vira só CACHE desta revogação.
package jwt

// RevogadorDeRefresh confere se um jti de refresh já foi revogado (logout).
type RevogadorDeRefresh interface {
	// Revogado devolve true quando o jti consta como revogado. Linha ausente
	// = token nunca emitido ou ainda válido — a decisão fica por conta das
	// demais validações, não daqui.
	Revogado(jti string) (bool, error)
}

// DefinirRevogador liga o verificador ao manager do processo — chamado UMA
// vez pelo cmd/bootstrap durante o boot. Passar nil desliga a conferência.
func (m *Manager) DefinirRevogador(r RevogadorDeRefresh) { m.revogador = r }
