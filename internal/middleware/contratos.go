// Package middleware monta a cadeia obrigatória de autenticação, resolução de
// workspace e autorização granular da plataforma (especificação em agents/03):
//
//	SetContextAuthorization() → quem está falando? (JWT ou X-Api-Key) — 401
//	ResolveWorkspace()        → qual workspace? (Host; X-Workspace-Id fora dele) — 400/404/403
//	RequirePermission(perm)   → pode fazer ISTO? — 403
//
// Regra que define o desenho: NENHUM import de internal/{dominio}/domain nem
// de application (os controllers importam ESTE pacote — voltar fecharia
// ciclo). Tudo o que a cadeia precisa do negócio entra por interfaces
// declaradas em contratos.go, implementadas por adaptadores do cmd/bootstrap
// que resolvem os singletons NA CHAMADA.
//
// Fail-closed: sem middleware.New no boot, toda função de pacote devolve
// handlers que respondem 403 — a cadeia nunca abre por acidente, e peça
// faltando é erro de boot, não rota aberta em runtime.
package middleware

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// ErrNaoEncontrado é o vocabulário do CONTRATO: os adaptadores traduzem o
// erro do subdomínio para ele. A resposta ao cliente não distingue
// inexistente de inativo — não vaza existência.
var ErrNaoEncontrado = errors.New("não encontrado no escopo da resolução")

// --- Contratos com o negócio (ACL: o consumidor dita o contrato) -----------

// WorkspaceResolvido é o MÍNIMO que a resolução precisa saber de um workspace:
// identificadores e vitalidade. Nada de modelo de domínio atravessando aqui.
type WorkspaceResolvido struct {
	UUID             uuid.UUID
	OrganizationUUID uuid.UUID
	Slug             string
	Ativo            bool
}

// DominioCustom é um domínio white-label registrado por uma organization.
type DominioCustom struct {
	Dominio          string // lowercase, sem porta — como casado contra o Host
	OrganizationUUID uuid.UUID
}

// ResolvedorWorkspaces pergunta ao subdomínio workspace o que o Host exige:
// a busca por slug é a EXCEÇÃO global documentada (resolução acontece antes
// de existir escopo; resultado nunca vaza para rotas de administração).
type ResolvedorWorkspaces interface {
	// BuscarPorSlug devolve ErrNaoEncontrado quando INEXISTENTE; inativo
	// volta com Ativo=false — a CADEIA responde o MESMO 404 para os dois
	// (não vaza existência; R7 alinhou este doc ao comportamento real do
	// adaptador, que devolve o registro com o status dele).
	BuscarPorSlug(ctx context.Context, slug string) (*WorkspaceResolvido, error)
	// BuscarPorUUID atende o fallback X-Workspace-Id (acesso direto/dev),
	// com as mesmas regras de BuscarPorSlug: inexistente = ErrNaoEncontrado;
	// inativo = Ativo=false e o mesmo 404 na cadeia.
	BuscarPorUUID(ctx context.Context, id uuid.UUID) (*WorkspaceResolvido, error)
	// Fixos devolve os rótulos de endereço fixo da plataforma (www, api,
	// painel...) — Host com um deles NUNCA resolve workspace.
	Fixos() []string
}

// ProvedorDominiosCustom expõe os domínios white-label registrados pelas
// organizations — consumido pela resolução do Host e pelo CORS.
type ProvedorDominiosCustom interface {
	Listar(ctx context.Context) ([]DominioCustom, error)
}

// IdentidadeChave é o resultado da validação de uma X-Api-Key: o "vínculo"
// da chave É o escopo dela (agents/03).
type IdentidadeChave struct {
	OrganizationUUID     uuid.UUID
	WorkspacesPermitidos []uuid.UUID // escopo em lista de workspaces…
	EscopoOrganization   bool        // …ou organization inteira
	Permissoes           []string    // explícitas na chave
}

// ResolvedorApiKeys valida uma chave de API e devolve o vínculo dela.
// Implementação entra na F2 (subdomínio organization); até lá toda
// X-Api-Key falha fechada.
type ResolvedorApiKeys interface {
	BuscarPorChave(ctx context.Context, chave string) (*IdentidadeChave, error)
}

// ResolvedorPermissoes cruza user × workspace × papel para a autorização.
// O acesso de suporte é a exceção auditada AO VÍNCULO: TemVinculo responde
// true também nele — a concessão sai no log e PermissoesEfetivas já carrega
// as permissões do papel de suporte.
type ResolvedorPermissoes interface {
	// TemVinculo confere atribuição direta OU concessão de suporte
	// (admin_organization na própria organization; super_admin em qualquer
	// uma). Consulta só nos caminhos de falha do vínculo quente.
	TemVinculo(ctx context.Context, usuarioUUID, organizationUUID, workspaceUUID uuid.UUID) (bool, error)
	// PermissoesEfetivas devolve a união das permissões dos papéis do usuário
	// no workspace ativo — curingas inclusos (identidade:user:* e *:*).
	PermissoesEfetivas(ctx context.Context, usuarioUUID, organizationUUID, workspaceUUID uuid.UUID) ([]string, error)
}

// ResolvedorAplicacoes pergunta ao domínio licensing quais módulos o par
// (organization, workspace) tem liberados — licença viva ∩ ativação viva ∩
// módulo ativo. Alimenta o passo RequireAplicacao da cadeia.
type ResolvedorAplicacoes interface {
	// Liberadas devolve os SLUGS dos módulos usáveis no par. Falha de
	// infraestrutura sobe INTACTA (nunca vira negativa: 500, não 403).
	Liberadas(ctx context.Context, organizationUUID, workspaceUUID uuid.UUID) ([]string, error)
}
