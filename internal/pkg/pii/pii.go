// Package pii mascara dados pessoais antes de irem para o log — a auditoria
// precisa registrar QUEM agiu sem preservar o identificador direto (o e-mail
// é PII: vaza em dump de log, agregador ou terminal compartilhado).
package pii

import "strings"

// MascaraEmail esconde o local part do e-mail mantendo a inicial e o
// domínio inteiro (o domínio serve à investigação — qual tenant — sem
// expor o identificador da pessoa). Entrada que nem parece e-mail (login
// falho com lixo arbitrário) vira "***" — nunca ecoa input não validado.
func MascaraEmail(email string) string {
	parts := strings.SplitN(strings.TrimSpace(email), "@", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "***"
	}
	return string(parts[0][0]) + "***@" + parts[1]
}
