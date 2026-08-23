package auth

import (
	"context"

	"workspace-api/internal/pkg/errobserve"
)

// serviceObservado decora o Service da aplicação observando TODO erro que
// sobe ao chamador (evolução errobserve): o erro sai INTACTO — a telemetria
// nunca muda a resposta ao cliente (e nunca quebra a indistinguibilidade do
// login, pois o corpo devolvido não é tocado). Montado DENTRO do NewService.
type serviceObservado struct {
	Service
	obs *errobserve.Observador
}

func (s serviceObservado) observar(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	return s.obs.Observe(ctx, err)
}

func (s serviceObservado) Login(ctx context.Context, host string, in LoginEntrada) (*SessaoResponseDto, error) {
	sessao, err := s.Service.Login(ctx, host, in)
	return sessao, s.observar(ctx, err)
}

func (s serviceObservado) Refresh(ctx context.Context, refreshToken string) (*SessaoResponseDto, error) {
	sessao, err := s.Service.Refresh(ctx, refreshToken)
	return sessao, s.observar(ctx, err)
}

func (s serviceObservado) Logout(ctx context.Context, refreshToken string) error {
	return s.observar(ctx, s.Service.Logout(ctx, refreshToken))
}
