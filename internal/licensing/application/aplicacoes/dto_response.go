package aplicacoes

import (
	modelativacao "workspace-api/internal/licensing/model/ativacao"
)

// MinhasAplicacoesResponseDto é a resposta do seletor: lista simples de
// aplicações liberadas (slug + nome) para o front decidir painel ou entrada.
type MinhasAplicacoesResponseDto struct {
	Aplicacoes []modelativacao.AplicacaoDisponivelDto `json:"aplicacoes"`
}
