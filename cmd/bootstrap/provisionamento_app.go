// Ligação da aplicação provisionamento (UX5) com os subdomínios: adaptadores
// que resolvem os singletons NA CHAMADA (regra do AGENTS.md do bootstrap) e
// traduzem o vocabulário de erro dos irmãos para o contrato da aplicação —
// ela nunca importa domain/ (regra 5 de agents/01).
package bootstrap

import (
	"context"
	"errors"

	"github.com/google/uuid"

	aplicacaoprovisionamento "workspace-api/internal/identidade/application/provisionamento"
	dominioUsuario "workspace-api/internal/identidade/domain/user"
	dominioWorkspace "workspace-api/internal/identidade/domain/workspace"
	modeluser "workspace-api/internal/identidade/model/user"
	modelworkspace "workspace-api/internal/identidade/model/workspace"
	"workspace-api/internal/pkg/orgctx"
)

// --- Contrato Organizacoes (estado da organization alvo) ----------------------

// O estadoOrganizacaoAlvo{} (workspace.go) já implementa a forma
// Estado(existe, ativa) exigida pela aplicação — conformidade ESTRUTURAL,
// sem import entre os consumidores.
var _ aplicacaoprovisionamento.Organizacoes = estadoOrganizacaoAlvo{}

// --- Contrato Workspaces -------------------------------------------------------

// workspacesProvisionamento pergunta e cria PELO service do workspace: a
// listagem é escopada na organization alvo (fail-closed do ctx) e a criação
// passa por OrganizationPedida, cuja autorização cross-tenant (UX4) mora no
// próprio subdomínio.
type workspacesProvisionamento struct{}

func (workspacesProvisionamento) TemWorkspaces(ctx context.Context, organizationUUID uuid.UUID) (bool, error) {
	_, total, err := dominioWorkspace.MustUse().Service.List(
		orgctx.WithOrganization(ctx, organizationUUID), modelworkspace.ListFilter{})
	if err != nil {
		return false, err
	}
	return total > 0, nil
}

func (workspacesProvisionamento) Criar(ctx context.Context, organizationUUID uuid.UUID, slug string) (uuid.UUID, error) {
	pedida := organizationUUID
	w, err := dominioWorkspace.MustUse().Service.Create(ctx, modelworkspace.CreateInput{
		OrganizationPedida: &pedida,
		Nome:               slug, // nome inicial = slug, mesma escolha do seed (R3)
		Slug:               slug,
	})
	if err != nil {
		switch {
		case errors.Is(err, dominioWorkspace.ErrSlugEmUso):
			return uuid.Nil, aplicacaoprovisionamento.ErrSlugIndisponivel
		case errors.Is(err, dominioWorkspace.ErrForaDoEscopo):
			// Chamador com a permissão da rota mas SEM posse de *:*: papel
			// customizado mal concedido — recusa explícita.
			return uuid.Nil, aplicacaoprovisionamento.ErrSemPoderPlataforma
		}
		return uuid.Nil, err
	}
	return w.UUID, nil
}

var _ aplicacaoprovisionamento.Workspaces = workspacesProvisionamento{}

// --- Contrato Usuarios ----------------------------------------------------------

// usuariosProvisionamento cria/reconhece o admin e atribui o papel PELO
// service do user — política de senha, bcrypt e validação do tripé ficam
// dentro dele. Os ctx chegam JÁ ESCOPADOS na organization alvo (a unicidade
// de e-mail e as queries fail-closed dependem desse escopo).
type usuariosProvisionamento struct{}

func (usuariosProvisionamento) UUIDPorEmail(ctx context.Context, email string) (uuid.UUID, bool, error) {
	u, err := dominioUsuario.MustUse().Repository.BuscarPorEmail(ctx, modeluser.Email(email))
	if err != nil {
		if errors.Is(err, dominioUsuario.ErrNotFound) {
			return uuid.Nil, false, nil
		}
		return uuid.Nil, false, err
	}
	return u.UUID, true, nil
}

func (usuariosProvisionamento) CriarAdmin(ctx context.Context, nome, email, senha string) (uuid.UUID, error) {
	u, err := dominioUsuario.MustUse().Service.Create(ctx, dominioUsuario.EntradaCriacao{
		Dados: modeluser.CreateInput{Nome: nome, Email: email},
		Senha: senha,
	})
	if err != nil {
		if errors.Is(err, dominioUsuario.ErrEmailEmUso) {
			return uuid.Nil, aplicacaoprovisionamento.ErrEmailEmUso
		}
		return uuid.Nil, err
	}
	return u.UUID, nil
}

func (usuariosProvisionamento) AtribuirPapel(ctx context.Context, usuarioUUID, workspaceUUID, papelUUID uuid.UUID) error {
	_, err := dominioUsuario.MustUse().Service.AtribuirPapel(ctx, usuarioUUID, workspaceUUID, papelUUID)
	if err != nil {
		if errors.Is(err, dominioUsuario.ErrAtribuicaoDuplicada) {
			// Duplicata = tentativa anterior gravou a atribuição e falhou
			// depois: idempotência, nunca sobe ao chamador.
			return aplicacaoprovisionamento.ErrAtribuicaoExistente
		}
		return err
	}
	return nil
}

var _ aplicacaoprovisionamento.Usuarios = usuariosProvisionamento{}

// --- Contrato Papeis -------------------------------------------------------------

// papeisProvisionamento resolve o papel seed pelo NOME canônico via service
// do user (leitura global das tabelas de papéis — exceção documentada).
type papeisProvisionamento struct{}

func (papeisProvisionamento) PorNome(ctx context.Context, nome string) (uuid.UUID, error) {
	papeis, err := dominioUsuario.MustUse().Service.Papeis(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	for _, p := range papeis {
		if p.Nome == nome {
			return p.UUID, nil
		}
	}
	return uuid.Nil, aplicacaoprovisionamento.ErrPapelAusente
}

var _ aplicacaoprovisionamento.Papeis = papeisProvisionamento{}
