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
	modelativacao "workspace-api/internal/licensing/model/ativacao"
	"workspace-api/internal/infra/jwt"
)

var (
	ErrCredenciaisInvalidas = errors.New("auth: credenciais inválidas")
	ErrSessaoInvalida       = errors.New("auth: sessão inválida ou expirada")
	ErrLoginBloqueado       = errors.New("auth: muitas tentativas de login")
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
	// ValidarSemRevogacao confere assinatura/expiração/tipo SEM a denylist —
	// porta EXCLUSIVA do logout idempotente (R5): o token já revogado precisa
	// ter as claims lidas para o EncerrarSessao confirmar a revogação. Nunca
	// usar em caminho que concede acesso (refresh segue por Validar).
	ValidarSemRevogacao(tokenTexto string) (*jwt.Claims, error)
}

// ResolvedorOrganization resolve a organization DONA do Host — a mesma fonte
// do ResolveWorkspace do middleware (subdomínio de workspace no domínio-base
// ou domínio custom white-label). Host sem organization resolvível devolve
// resolvido=false SEM erro: para o cliente isso vira o MESMO 401 genérico.
type ResolvedorOrganization interface {
	Resolver(ctx context.Context, host string) (organizationUUID uuid.UUID, resolvido bool, err error)
}

// ResolvedorParLogin resolve organization E workspace do Host — usado pelo
// seletor de aplicações dentro do LOGIN (as aplicações liberadas são do par
// (org, ws), não da organização sozinha). Host de white-label raiz (sem
// rótulo) resolve só a organization: workspace devolve uuid.Nil e a lista
// sai vazia — o painel do parceiro é core, não módulo.
type ResolvedorParLogin interface {
	ResolverPar(ctx context.Context, host string) (organizationUUID, workspaceUUID uuid.UUID, resolvido bool, err error)
}

// ProvedorAcessos responde quais módulos o par (organization, workspace) tem
// liberados — DECORATIVO no login: falha do provedor NUNCA derruba a sessão
// (lista vazia + log); o endpoint /minhas-aplicacoes é a revalidação
// autoritativa. Nil = feature desligada.
type ProvedorAcessos interface {
	Aplicacoes(ctx context.Context, organizationUUID, workspaceUUID uuid.UUID) ([]modelativacao.AplicacaoDisponivelDto, error)
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

// LimitadorLogin é a face do rate-limit/lockout distribuído (evolução Redis,
// issue #8): trava por (e-mail, IP) após repetidas falhas. DEGRADÁVEL — nil
// nas Dependências = feature desligada (sem Redis não há lockout); falha de
// infra do limitador NUNCA impede login (segue sem lockout, com log), pois
// derrubar autenticação por causa de cache seria pior que a ausência dele.
type LimitadorLogin interface {
	// Autorizado responde se o par e-mail+IP está bloqueado e há quanto tempo
	// espera restante.
	Autorizado(ctx context.Context, email, ip string) (bloqueado bool, espera time.Duration, err error)
	// RegistrarFalha conta uma credencial recusada para o par.
	RegistrarFalha(ctx context.Context, email, ip string) error
	// RegistrarSucesso limpa o histórico do par após login bem-sucedido.
	RegistrarSucesso(ctx context.Context, email, ip string) error
}
