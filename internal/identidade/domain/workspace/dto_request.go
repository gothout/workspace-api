package workspace

import (
	"github.com/google/uuid"

	modelworkspace "workspace-api/internal/identidade/model/workspace"
)

// CreateWorkspaceRequestDto — entrada de POST /api/domain/identidade/workspaces.
// organization_uuid é OPCIONAL e SÓ tem efeito para chamador PLATAFORMA
// (posse exata de `*:*`): é a criação cross-tenant auditada da UX4 — o
// primeiro workspace de uma organization recém-criada. Para os demais
// chamadores, apontar organization alheia recusa com fora_do_escopo (404);
// apontar a própria é equivalente ao escopo do ctx.
type CreateWorkspaceRequestDto struct {
	Nome             string     `json:"nome" binding:"required,min=2,max=120"`
	Slug             string     `json:"slug" binding:"required,min=3,max=63,slugdns"` // slugdns: tag do pkg/validator
	OrganizationUUID *uuid.UUID `json:"organization_uuid" binding:"omitempty,uuid"`
}

// ParaEntrada converte o DTO no input do service — controller nunca monta entidade.
func (d CreateWorkspaceRequestDto) ParaEntrada() modelworkspace.CreateInput {
	return modelworkspace.CreateInput{Nome: d.Nome, Slug: d.Slug, OrganizationPedida: d.OrganizationUUID}
}

// UpdateWorkspaceRequestDto — entrada de PATCH; ponteiros distinguem
// "ausente" de "vazio". Inativar é transição válida; reativação é ação própria.
type UpdateWorkspaceRequestDto struct {
	Nome   *string `json:"nome" binding:"omitempty,min=2,max=120"`
	Status *string `json:"status" binding:"omitempty,oneof=ativo inativo"`
}

// ParaEntrada valida e converte para UpdateInput; status fora do conjunto = ErrInvalidInput.
func (d UpdateWorkspaceRequestDto) ParaEntrada() (modelworkspace.UpdateInput, error) {
	in := modelworkspace.UpdateInput{Nome: d.Nome}
	if d.Status != nil {
		s := modelworkspace.StatusWorkspace(*d.Status)
		if !s.Valido() {
			return modelworkspace.UpdateInput{}, ErrInvalidInput
		}
		in.Status = &s
	}
	return in, nil
}
