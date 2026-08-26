package auth

import (
	"github.com/google/uuid"

	modelativacao "workspace-api/internal/licensing/model/ativacao"
)

// SessaoResponseDto — saída única dos três verbos que abrem/renovam sessão.
// Tokens viajam no CORPO (o template não usa cookies): a responsabilidade de
// armazenamento é do cliente. O usuário devolvido é o resumo público — sem
// hash, sem estado interno além do essencial.
type SessaoResponseDto struct {
	AccessToken  string        `json:"access_token"`
	RefreshToken string        `json:"refresh_token"`
	TokenType    string        `json:"token_type"`
	Usuario      UsuarioResumo `json:"usuario"`
	// Aplicacoes lista os módulos liberados do par (org, ws) resolvido pelo
	// Host — o front decide seletor (2+) ou entrada direta (1). Vazia = sem
	// módulo liberado (painel core). Só o LOGIN carrega: refresh não conhece
	// Host; o front revalida por /minhas-aplicacoes.
	Aplicacoes []modelativacao.AplicacaoDisponivelDto `json:"aplicacoes"`
}

type UsuarioResumo struct {
	UUID  uuid.UUID `json:"uuid"`
	Nome  string    `json:"nome"`
	Email string    `json:"email"`
}
