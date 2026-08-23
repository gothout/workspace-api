package user

import (
	"time"

	"github.com/google/uuid"

	modeluser "workspace-api/internal/identidade/model/user"
	"workspace-api/internal/pkg/pagination"
)

// UserResponseDto — saída única do subdomínio. A credencial NUNCA aparece:
// nem hash, nem política — o campo nem existe aqui (o modelo guarda com
// `json:"-"`; este DTO simplesmente não tem o campo).
type UserResponseDto struct {
	UUID             uuid.UUID `json:"uuid"`
	OrganizationUUID uuid.UUID `json:"organization_uuid"`
	Nome             string    `json:"nome"`
	Email            string    `json:"email"`
	Status           string    `json:"status"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func NovoUserResponseDto(u *modeluser.User) UserResponseDto {
	return UserResponseDto{
		UUID:             u.UUID,
		OrganizationUUID: u.OrganizationUUID,
		Nome:             u.Nome,
		Email:            u.Email.String(),
		Status:           string(u.Status),
		CreatedAt:        u.CreatedAt,
		UpdatedAt:        u.UpdatedAt,
	}
}

func NovoUserListaResponseDto(items []modeluser.User, total int64, p pagination.Pagination) pagination.Response[UserResponseDto] {
	dtos := make([]UserResponseDto, 0, len(items))
	for i := range items {
		dtos = append(dtos, NovoUserResponseDto(&items[i]))
	}
	return pagination.NovaResponse(dtos, total, p)
}

// AtribuicaoResponseDto — saída das atribuições do usuário: o vínculo
// user × workspace × papel com o nome canônico do papel.
type AtribuicaoResponseDto struct {
	UUID          uuid.UUID `json:"uuid"`
	UserUUID      uuid.UUID `json:"user_uuid"`
	WorkspaceUUID uuid.UUID `json:"workspace_uuid"`
	PapelUUID     uuid.UUID `json:"papel_uuid"`
	Papel         string    `json:"papel"`
	CreatedAt     time.Time `json:"created_at"`
}

func NovoAtribuicaoResponseDto(a *modeluser.AtribuicaoComPapel) AtribuicaoResponseDto {
	return AtribuicaoResponseDto{
		UUID:          a.UUID,
		UserUUID:      a.UserUUID,
		WorkspaceUUID: a.WorkspaceUUID,
		PapelUUID:     a.PapelUUID,
		Papel:         a.PapelNome,
		CreatedAt:     a.CreatedAt,
	}
}

func NovasAtribuicoesResponseDto(itens []modeluser.AtribuicaoComPapel) []AtribuicaoResponseDto {
	dtos := make([]AtribuicaoResponseDto, 0, len(itens))
	for i := range itens {
		dtos = append(dtos, NovoAtribuicaoResponseDto(&itens[i]))
	}
	return dtos
}
