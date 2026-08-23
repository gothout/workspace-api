// Contratos com o mundo externo (ACL — regra 5 de agents/01): o consumidor
// dita o contrato, o cmd/bootstrap liga adaptadores que resolvem os
// singletons NA CHAMADA e traduzem o vocabulário de erro dos subdomínios.
//
// As sentinelas daqui são o VOCABULÁRIO DA AUTENTICAÇÃO: o adaptador de
// Usuarios traduz identidade.user.credenciais_invalidas para
// ErrCredenciaisInvalidas — é o que garante corpo idêntico para "não
// existe", "senha errada" E "host sem organization resolvível".
package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	modeluser "workspace-api/internal/identidade/model/user"
	"workspace-api/internal/infra/jwt"
)

var (
	ErrCredenciaisInvalidas = errors.New("auth: credenciais inválidas")
	ErrSessaoInvalida       = errors.New("auth: sessão inválida ou expirada")
)

// Usuarios é a face do subdomínio user de que o login precisa — e SÓ ela.
// A comparação de senha mora lá (método do service); aqui chega só o
// veredito. Os métodos recebem ctx JÁ ESCOPADO na organization resolvida
// pelo Host (a própria aplicação injeta antes de chamar).
type Usuarios interface {
	// Autenticar devolve ErrCredenciaisInvalidas-traduzido quando usuário
	// não existe, senha errada OU conta inativa — indistinguível por lá.
	Autenticar(ctx context.Context, email, senha string) (*modeluser.User, error)
	// PorUUID recarrega o usuário na troca de tokens (nome/e-mail podem ter
	// mudado; conta inativa encerra a sessão).
	PorUUID(ctx context.Context, id uuid.UUID) (*modeluser.User, error)
	// RegistrarRefreshToken persiste a linha do jti (revogação por jti).
	RegistrarRefreshToken(ctx context.Context, usuarioUUID uuid.UUID, jti string, expiraEm time.Time) error
	// RefreshTokenAtivo confirma que a linha existe, é do usuário e não foi
	// revogada — segunda linha de defesa além da assinatura.
	RefreshTokenAtivo(ctx context.Context, usuarioUUID uuid.UUID, jti string) (bool, error)
	// EncerrarSessao revoga marcando revogado_em (logout persistido).
	EncerrarSessao(ctx context.Context, usuarioUUID uuid.UUID, jti string) error
}

// EmissorToken é a face do infra/jwt: emite o PAR access+refresh com as
// claims do contrato e valida assinatura/expiração/tipo.
type EmissorToken interface {
	// EmitirPar devolve access, refresh, o jti DO REFRESH e a expiração dele
	// (o jti é a chave da revogação persistida).
	EmitirPar(in jwt.EntradaToken) (acesso string, refresh string, jti string, expiraRefresh time.Time, err error)
	Validar(tokenTexto string) (*jwt.Claims, error)
}

// ResolvedorOrganization resolve a organization DONA do Host — a mesma fonte
// do ResolveWorkspace do middleware (subdomínio de workspace no domínio-base
// ou domínio custom white-label). Host sem organization resolvível devolve
// resolvido=false SEM erro: para o cliente isso vira o MESMO 401 genérico.
type ResolvedorOrganization interface {
	Resolver(ctx context.Context, host string) (organizationUUID uuid.UUID, resolvido bool, err error)
}

// VitalidadeOrganization pergunta se a organization DONA da sessão segue
// viva (R4): refresh/logout falham FECHADO com dona inativa/removida, mesmo
// que a linha do jti ainda esteja ativa — defesa em profundidade além da
// revogação em cascata disparada pela própria dona.
type VitalidadeOrganization interface {
	// Ativa responde se a organization existe e está ativa. Removida/inativa
	// = false SEM erro; falha de infraestrutura sobe para o chamador decidir.
	Ativa(ctx context.Context, organizationUUID uuid.UUID) (bool, error)
}
