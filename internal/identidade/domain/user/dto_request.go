package user

import (
	modeluser "workspace-api/internal/identidade/model/user"
)

// CreateUserRequestDto — entrada de POST /api/domain/identidade/users.
// organization_uuid NUNCA entra por aqui: o service toma do ctx (escopo).
// A senha cru atravessa UMA vez até o service, que aplica a política e gera
// o hash — nunca sai do subdomínio (nem volta em resposta).
type CreateUserRequestDto struct {
	Nome  string `json:"nome" binding:"required,min=2,max=120"`
	Email string `json:"email" binding:"required,email,max=254"`
	Senha string `json:"senha" binding:"required,min=8,max=72"`
}

// EntradaCriacao carrega os dados do modelo MAIS a senha crua — tipo interno
// da fronteira controller→service deste pacote: o hash nasce no service e a
// senha não atravessa para o modelo nem para qualquer outra camada.
type EntradaCriacao struct {
	Dados modeluser.CreateInput // OrganizationUUID é preenchido pelo service
	Senha string
}

// ParaEntrada converte o DTO na entrada do service — controller nunca monta entidade.
func (d CreateUserRequestDto) ParaEntrada() EntradaCriacao {
	return EntradaCriacao{Dados: modeluser.CreateInput{Nome: d.Nome, Email: d.Email}, Senha: d.Senha}
}

// UpdateUserRequestDto — entrada de PATCH; ponteiros distinguem "ausente" de
// "vazio". Ambas as transições passam pelos métodos de comportamento; inativar
// revoga as sessões abertas do usuário.
type UpdateUserRequestDto struct {
	Nome   *string `json:"nome" binding:"omitempty,min=2,max=120"`
	Status *string `json:"status" binding:"omitempty,oneof=ativo inativo"`
}

// ParaEntrada valida e converte para UpdateInput; status fora do conjunto = ErrInvalidInput.
func (d UpdateUserRequestDto) ParaEntrada() (modeluser.UpdateInput, error) {
	in := modeluser.UpdateInput{Nome: d.Nome}
	if d.Status != nil {
		s := modeluser.StatusUsuario(*d.Status)
		if !s.Valido() {
			return modeluser.UpdateInput{}, ErrInvalidInput
		}
		in.Status = &s
	}
	return in, nil
}

// AtribuirPapelRequestDto — entrada de POST .../users/{uuid}/atribuicoes:
// liga o usuário a um workspace com um papel. Referências são por uuid.
type AtribuirPapelRequestDto struct {
	WorkspaceUUID string `json:"workspace_uuid" binding:"required,uuid4"`
	PapelUUID     string `json:"papel_uuid" binding:"required,uuid4"`
}
