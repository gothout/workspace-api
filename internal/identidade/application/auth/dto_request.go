package auth

// LoginRequestDto — entrada de POST /api/application/identidade/auth/login.
// A organization NÃO vem do corpo: vem da resolução do Host pelo contrato
// ResolvedorOrganization — credencial só vale dentro do tenant endereçado.
type LoginRequestDto struct {
	Email string `json:"email" binding:"required,email,max=254"`
	Senha string `json:"senha" binding:"required,min=8,max=72"`
}

// TokenRequestDto — entrada de refresh e logout: o próprio refresh token
// carrega quem e onde (claims assinadas).
type TokenRequestDto struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}
