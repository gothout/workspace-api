// Contratos com o mundo externo (ACL — regras 3 e 4 de agents/01): ativar um
// módulo num workspace cruza TRÊS agregados irmãos (workspace, módulo,
// licença) — todos entram por interface declarada AQUI, implementados pelos
// subdomínios donos e ligados no cmd/bootstrap (adaptadores resolvem os
// singletons NA CHAMADA). Nil em qualquer peça = fail-closed na ativação.
package ativacao

import (
	"context"
	"errors"

	"github.com/google/uuid"

	modelmodulo "workspace-api/internal/licensing/model/modulo"
)

// ErrModuloNaoEncontrado e ErrWorkspaceNaoEncontrado são o vocabulário DOS
// CONTRATOS: os adaptadores traduzem os ErrNotFound dos irmãos para eles —
// o consumidor nunca importa o domínio alheio.
var (
	ErrModuloNaoEncontrado    = errors.New("módulo não encontrado no catálogo")
	ErrWorkspaceNaoEncontrado = errors.New("workspace não encontrado no escopo")
)

// BuscadorModulos resolve o slug informado no catálogo; a ativação só aceita
// módulo ATIVO — catálogo desativado não entra em novo workspace.
type BuscadorModulos interface {
	BuscarPorSlug(ctx context.Context, slug string) (*modelmodulo.Modulo, error)
}

// VerificadorLicencas pergunta ao agregado licença se a organization tem
// concessão viva do módulo — sem licença NÃO há ativação.
type VerificadorLicencas interface {
	Existe(ctx context.Context, organizationUUID, moduloUUID uuid.UUID) (bool, error)
}

// ValidadorWorkspaces confere que o workspace alvo pertence à organization e
// está vivo — mesmo desenho do contrato homônimo do domínio identidade.
type ValidadorWorkspaces interface {
	Pertence(ctx context.Context, organizationUUID, workspaceUUID uuid.UUID) (bool, error)
}

// InvalidadorAcessos observa escritas de ATIVAÇÃO para invalidar o cache de
// módulos liberados do par exato. Declaração do consumidor; implementação
// Redis no bootstrap. Nil = sem cache — operação normal, só mais cara.
type InvalidadorAcessos interface {
	// InvalidarPar derruba app:{org}:{ws}.
	InvalidarPar(ctx context.Context, organizationUUID, workspaceUUID string)
}
