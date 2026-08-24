package provisionamento

// ProvisionamentoRequestDto — entrada de POST /api/domain/identidade/
// organizations/{uuid}/provisionamento. A SENHA é definida pelo chamador
// (nunca gerada em claro pelo servidor) e NUNCA sai em resposta nem log.
type ProvisionamentoRequestDto struct {
	Nome  string `json:"nome" binding:"required,min=2,max=120"`
	Email string `json:"email" binding:"required,email"`
	Senha string `json:"senha" binding:"required,min=8,max=72"`
	Slug  string `json:"slug" binding:"required,min=3,max=63,slugdns"` // slugdns: tag do pkg/validator
}
