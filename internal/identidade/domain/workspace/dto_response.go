package workspace

import (
	"time"

	"github.com/google/uuid"

	modelworkspace "workspace-api/internal/identidade/model/workspace"
	"workspace-api/internal/pkg/pagination"
)

// WorkspaceResponseDto — saída única do subdomínio; nunca expõe campos internos.
type WorkspaceResponseDto struct {
	UUID             uuid.UUID `json:"uuid"`
	OrganizationUUID uuid.UUID `json:"organization_uuid"`
	Nome             string    `json:"nome"`
	Slug             string    `json:"slug"`
	Status           string    `json:"status"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func NovoWorkspaceResponseDto(w *modelworkspace.Workspace) WorkspaceResponseDto {
	return WorkspaceResponseDto{
		UUID:             w.UUID,
		OrganizationUUID: w.OrganizationUUID,
		Nome:             w.Nome,
		Slug:             w.Slug.String(),
		Status:           string(w.Status),
		CreatedAt:        w.CreatedAt,
		UpdatedAt:        w.UpdatedAt,
	}
}

func NovoWorkspaceListaResponseDto(items []modelworkspace.Workspace, total int64, p pagination.Pagination) pagination.Response[WorkspaceResponseDto] {
	dtos := make([]WorkspaceResponseDto, 0, len(items))
	for i := range items {
		dtos = append(dtos, NovoWorkspaceResponseDto(&items[i]))
	}
	return pagination.NovaResponse(dtos, total, p)
}
