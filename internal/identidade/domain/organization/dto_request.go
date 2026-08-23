package organization

import (
	"time"

	"github.com/google/uuid"

	orgmodel "workspace-api/internal/identidade/model/organization"
)

// CreateOrganizationRequestDto — entrada de POST /api/domain/identidade/organizations.
type CreateOrganizationRequestDto struct {
	Nome      string `json:"nome" binding:"required,min=2,max=120"`
	Documento string `json:"documento" binding:"omitempty,max=64"`
}

// ParaEntrada converte o DTO no input do service — controller nunca monta entidade.
func (d CreateOrganizationRequestDto) ParaEntrada() orgmodel.CreateInput {
	return orgmodel.CreateInput{Nome: d.Nome, Documento: d.Documento}
}

// UpdateOrganizationRequestDto — entrada de PATCH; ponteiros distinguem
// "ausente" de "vazio". Inativar é transição válida; reativação é ação própria.
type UpdateOrganizationRequestDto struct {
	Nome   *string `json:"nome" binding:"omitempty,min=2,max=120"`
	Status *string `json:"status" binding:"omitempty,oneof=ativo inativo"`
}

// ParaEntrada valida e converte para UpdateInput; status fora do conjunto = ErrInvalidInput.
func (d UpdateOrganizationRequestDto) ParaEntrada() (orgmodel.UpdateInput, error) {
	in := orgmodel.UpdateInput{Nome: d.Nome}
	if d.Status != nil {
		s := orgmodel.Status(*d.Status)
		if !s.Valido() {
			return orgmodel.UpdateInput{}, ErrInvalidInput
		}
		in.Status = &s
	}
	return in, nil
}

// DefinirDominioRequestDto — entrada de PUT .../{uuid}/dominio. A validação de
// formato DNS/base_domain/public suffix vive no VO (ParseDominio); aqui só o
// binding recusa cedo.
type DefinirDominioRequestDto struct {
	Dominio string `json:"dominio" binding:"required,max=253"`
}

// CreateApiKeyRequestDto — entrada de POST .../{uuid}/api-keys. A chave em
// claro NUNCA entra por aqui: ela é GERADA pelo service e devolvida uma única vez.
type CreateApiKeyRequestDto struct {
	Nome                 string      `json:"nome" binding:"required,min=3,max=120"`
	EscopoOrganization   bool        `json:"escopo_organization"`
	WorkspacesPermitidos []uuid.UUID `json:"workspaces_permitidos" binding:"omitempty,dive,uuid"`
	Permissoes           []string    `json:"permissoes" binding:"required,dive,required,max=64"`
	ExpiresAt            *time.Time  `json:"expires_at" binding:"omitempty,gt=0"`
}

// ParaEntrada converte para o input do service.
func (d CreateApiKeyRequestDto) ParaEntrada() ApiKeyEntrada {
	return ApiKeyEntrada{
		Nome:                 d.Nome,
		EscopoOrganization:   d.EscopoOrganization,
		WorkspacesPermitidos: d.WorkspacesPermitidos,
		Permissoes:           d.Permissoes,
		ExpiresAt:            d.ExpiresAt,
	}
}
