package catalogo

import (
	"context"

	"workspace-api/internal/pkg/orgctx"
	"workspace-api/internal/pkg/rest_err"
)

// Service é a regra da aplicação: montar os DOIS contratos do front-end.
// Não persiste nada (aplicação não tem repository) e não conhece subdomínio
// de domain — consome o catálogo agregado por contrato e o registro global
// de erros do rest_err.
type Service interface {
	// MinhasPermissoes devolve a árvore dominio → subdominio → ações JÁ
	// FILTRADA pelas permissões efetivas do usuário no ctx — o MESMO conjunto
	// resolvido pelo middleware para o RequirePermission (orgctx.Permissoes),
	// nunca uma releitura própria.
	MinhasPermissoes(ctx context.Context) ArvoreResponseDto
	// MapaDeErros devolve TODOS os erros possíveis do sistema, agrupados —
	// reflexo direto do registro global do rest_err, sem hardcode.
	MapaDeErros() ErrosResponseDto
}

type serviceImpl struct{ permissoes ProvedorPermissoes }

func NewService(permissoes ProvedorPermissoes) Service {
	return &serviceImpl{permissoes: permissoes}
}

// MinhasPermissoes: ctx → permissões efetivas → filtro sobre o catálogo
// agregado. Leitura pura — leitura NÃO audita (doc 04).
func (s *serviceImpl) MinhasPermissoes(ctx context.Context) ArvoreResponseDto {
	return NovoArvoreResponseDto(s.permissoes.Catalogo(), orgctx.Permissoes(ctx))
}

func (s *serviceImpl) MapaDeErros() ErrosResponseDto {
	return NovoErrosResponseDto(rest_err.MapaErros())
}
