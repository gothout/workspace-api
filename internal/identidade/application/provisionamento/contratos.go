// Contratos com o mundo externo (ACL — regra 5 de agents/01): o consumidor
// dita o contrato, o cmd/bootstrap liga adaptadores que resolvem os
// singletons NA CHAMADA e traduzem o vocabulário de erro dos subdomínios.
//
// O provisionamento é ORQUESTRAÇÃO entre organization → workspace → user
// (papel atribuído por workspace): por isso mora em application/ e nunca
// importa subdomínio de domain/ — só os pacotes model/ (folha).
package provisionamento

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// ErrAtribuicaoExistente é o vocabulário INTERNO da aplicação: o adaptador
// do Usuarios traduz a duplicata do subdomínio para cá e o Provisionar trata
// como idempotência (tentativa anterior que gravou a atribuição e falhou
// depois) — nunca sobe ao chamador.
var ErrAtribuicaoExistente = errors.New("atribuição já existe")

// Organizacoes pergunta ao irmão organization o estado da organization ALVO:
// provisionar organization inexistente ou inativa é recusa (filho nunca fica
// mais vivo que o pai).
type Organizacoes interface {
	// Estado devolve existe/ativa. Inexistente = (false, false) SEM erro;
	// falha de infraestrutura sobe para o chamador.
	Estado(ctx context.Context, organizationUUID uuid.UUID) (existe bool, ativa bool, err error)
}

// Workspaces é a face do irmão workspace de que o provisionamento precisa:
// saber se a organization já tem workspace (definição de "já provisionada")
// e criar o workspace INICIAL nela. A autorização cross-tenant (só a
// plataforma cria em organization alheia) mora DENTRO do subdomínio (UX4).
type Workspaces interface {
	// TemWorkspaces responde se a organization tem QUALQUER workspace.
	TemWorkspaces(ctx context.Context, organizationUUID uuid.UUID) (bool, error)
	// Criar cria o workspace inicial na organization pedida com o slug
	// informado (o nome dele é o próprio slug, como no seed da R3). Slug já
	// tomado = ErrSlugIndisponivel; chamador sem poder de plataforma =
	// ErrSemPoderPlataforma (traduções do adaptador).
	Criar(ctx context.Context, organizationUUID uuid.UUID, slug string) (uuid.UUID, error)
}

// Usuarios é a face do irmão user: criar o admin inicial com a senha
// escolhida pelo chamador (hash/bcrypt moram no subdomínio), reconhecer o
// que já existe (recuperação de falha no meio do fluxo) e atribuir o papel.
// Os métodos de escrita recebem ctx JÁ ESCOPADO na organization alvo — a
// unicidade de e-mail e as queries fail-closed dependem desse escopo.
type Usuarios interface {
	// UUIDPorEmail devolve o uuid do usuário com o e-mail na organization do
	// ctx; ausente = (uuid.Nil, false) SEM erro.
	UUIDPorEmail(ctx context.Context, email string) (uuid.UUID, bool, error)
	// CriarAdmin cria o usuário ativo com senha; e-mail duplicado na
	// organization = ErrEmailEmUso.
	CriarAdmin(ctx context.Context, nome, email, senha string) (uuid.UUID, error)
	// AtribuirPapel liga usuário × workspace × papel (validação do tripé
	// dentro do subdomínio); duplicata vira idempotência no adaptador.
	AtribuirPapel(ctx context.Context, usuarioUUID, workspaceUUID, papelUUID uuid.UUID) error
}

// Papeis resolve o papel seed pelo NOME canônico (tabela global da
// plataforma — exceção documentada no doc 03). Ausente = ErrPapelAusente
// (seed não rodado).
type Papeis interface {
	PorNome(ctx context.Context, nome string) (uuid.UUID, error)
}
