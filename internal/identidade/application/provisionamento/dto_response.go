package provisionamento

import (
	"github.com/google/uuid"
)

// ProvisionamentoResponseDto — resultado do provisionamento: os
// identificadores do que foi criado/reconhecido. A senha NUNCA aparece.
type ProvisionamentoResponseDto struct {
	OrganizationUUID uuid.UUID `json:"organization_uuid"`
	WorkspaceUUID    uuid.UUID `json:"workspace_uuid"`
	AdminUUID        uuid.UUID `json:"admin_uuid"`
	Email            string    `json:"email"`
	Slug             string    `json:"slug"`
}
