package organization

import (
	"time"

	"github.com/google/uuid"

	orgmodel "workspace-api/internal/identidade/model/organization"
	"workspace-api/internal/pkg/pagination"
)

// OrganizationResponseDto — saída única do subdomínio; nunca expõe hash nem
// campos internos.
type OrganizationResponseDto struct {
	UUID      uuid.UUID `json:"uuid"`
	Nome      string    `json:"nome"`
	Documento string    `json:"documento"`
	Dominio   string    `json:"dominio,omitempty"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func NovoOrganizationResponseDto(o *orgmodel.Organization) OrganizationResponseDto {
	return OrganizationResponseDto{
		UUID:      o.UUID,
		Nome:      o.Nome,
		Documento: o.Documento,
		Dominio:   o.Dominio.String(),
		Status:    string(o.Status),
		CreatedAt: o.CreatedAt,
		UpdatedAt: o.UpdatedAt,
	}
}

func NovoOrganizationListaResponseDto(items []orgmodel.Organization, total int64, p pagination.Pagination) pagination.Response[OrganizationResponseDto] {
	dtos := make([]OrganizationResponseDto, 0, len(items))
	for i := range items {
		dtos = append(dtos, NovoOrganizationResponseDto(&items[i]))
	}
	return pagination.NovaResponse(dtos, total, p)
}

// ApiKeyResponseDto — listagem de chaves: só metadados, NUNCA o hash.
type ApiKeyResponseDto struct {
	UUID                 uuid.UUID  `json:"uuid"`
	OrganizationUUID     uuid.UUID  `json:"organization_uuid"`
	Nome                 string     `json:"nome"`
	EscopoOrganization   bool       `json:"escopo_organization"`
	WorkspacesPermitidos []string   `json:"workspaces_permitidos"`
	Permissoes           []string   `json:"permissoes"`
	ExpiresAt            *time.Time `json:"expires_at,omitempty"`
	Status               string     `json:"status"`
	CreatedAt            time.Time  `json:"created_at"`
}

func textos(l orgmodel.ListaTextos) []string {
	if len(l) == 0 {
		return []string{}
	}
	return []string(l)
}

func uuidsParaTextos(l orgmodel.ListaUUIDs) []string {
	lista := make([]string, 0, len(l))
	for _, u := range l {
		lista = append(lista, u.String())
	}
	return lista
}

func NovoApiKeyResponseDto(k *orgmodel.ApiKey) ApiKeyResponseDto {
	return ApiKeyResponseDto{
		UUID:                 k.UUID,
		OrganizationUUID:     k.OrganizationUUID,
		Nome:                 k.Nome,
		EscopoOrganization:   k.EscopoOrganization,
		WorkspacesPermitidos: uuidsParaTextos(k.WorkspacesPermitidos),
		Permissoes:           textos(k.Permissoes),
		ExpiresAt:            k.ExpiresAt,
		Status:               string(k.Status),
		CreatedAt:            k.CreatedAt,
	}
}

func NovoApiKeyListaResponseDto(items []orgmodel.ApiKey, total int64, p pagination.Pagination) pagination.Response[ApiKeyResponseDto] {
	dtos := make([]ApiKeyResponseDto, 0, len(items))
	for i := range items {
		dtos = append(dtos, NovoApiKeyResponseDto(&items[i]))
	}
	return pagination.NovaResponse(dtos, total, p)
}

// ApiKeyCriadaResponseDto é a ÚNICA resposta que carrega a chave em claro —
// exibida uma única vez; depois dela a chave nunca mais sai do servidor.
type ApiKeyCriadaResponseDto struct {
	ApiKeyResponseDto
	Chave string `json:"chave"`
}

func NovoApiKeyCriadaResponseDto(k *orgmodel.ApiKey, chave string) ApiKeyCriadaResponseDto {
	return ApiKeyCriadaResponseDto{ApiKeyResponseDto: NovoApiKeyResponseDto(k), Chave: chave}
}
