// Package user implementa o MODELO do subdomínio user do domínio identidade:
// a pessoa da hierarquia (filha da organization, com papéis atribuídos por
// workspace), o refresh token persistido, a atribuição e os papéis globais
// da plataforma — entidades raiz/filhas do agregado, VO Email e invariantes.
//
// A CREDENCIAL não recebe comportamento de leitura aqui: a comparação de
// senha é método do service do subdomínio (internal/identidade/domain/user),
// nunca função deste pacote.
//
// FOLHA do domínio: não importa domain, application, infra nem middleware —
// qualquer camada pode importá-lo (regra 9 do doc 01).
package user

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"workspace-api/internal/pkg/pagination"
)

const (
	// Dominio e Subdominio identificam este modelo nos catálogos (erros,
	// permissões) e logs.
	Dominio    = "identidade"
	Subdominio = "user"
)

// Sentinelas de invariante do MODELO — o pacote é folha e não importa o
// errors.go do subdomínio; o catálogo de lá (code estável + status) registra
// estas sentinelas.
var (
	ErrEmailInvalido      = errors.New("e-mail fora do formato esperado")
	ErrNomeInvalido       = errors.New("nome fora do formato esperado")
	ErrSenhaInvalida      = errors.New("senha fora da política de credenciais")
	ErrHashAusente        = errors.New("hash de senha ausente")
	ErrJaInativo          = errors.New("usuário já está inativo")
	ErrJaAtivo            = errors.New("usuário já está ativo")
	ErrAtribuicaoInvalida = errors.New("atribuição incompleta: informe usuário, organization, workspace e papel")
	ErrRefreshInvalido    = errors.New("refresh token fora do formato esperado")
)

// Limites da política de senha: mínimo de 8 e máximo de 72 bytes — o teto é
// o limite interno do bcrypt, que trunca silenciosamente acima dele.
const (
	TamanhoMinimoSenha = 8
	TamanhoMaximoSenha = 72
)

// ValidarSenha confere a POLÍTICA da senha crua — checagem de invariante,
// NÃO comparação de credencial (essa é método do service do subdomínio).
func ValidarSenha(senha string) error {
	if len(senha) < TamanhoMinimoSenha || len(senha) > TamanhoMaximoSenha {
		return ErrSenhaInvalida
	}
	return nil
}

// StatusUsuario — ciclo de vida do usuário. Inativar encerra sessões: os
// refresh tokens ativos são revogados pelo service na transição.
type StatusUsuario string

const (
	StatusAtivo   StatusUsuario = "ativo"
	StatusInativo StatusUsuario = "inativo"
)

// Valido confere se o valor está no conjunto fechado — todo tipo nomeado tem um.
func (s StatusUsuario) Valido() bool {
	switch s {
	case StatusAtivo, StatusInativo:
		return true
	}
	return false
}

// --- VO Email -----------------------------------------------------------------

// emailRegex aceita a forma local@dominio com domínio DNS simples — a
// primeira linha de defesa fica na tag `email` do binding; aqui é a garantia
// de domínio. Entrada normalizada para minúsculas.
var emailRegex = regexp.MustCompile(`^[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}$`)

// Email é um VALUE OBJECT: imutável, válido desde o nascimento. É único POR
// organization — a checagem de unicidade é do service/repository, não daqui.
type Email string

// ParseEmail valida o formato e devolve o VO; inválido = ErrEmailInvalido.
func ParseEmail(valor string) (Email, error) {
	e := strings.ToLower(strings.TrimSpace(valor))
	if len(e) < 6 || len(e) > 254 || !emailRegex.MatchString(e) {
		return "", ErrEmailInvalido
	}
	return Email(e), nil
}

func (e Email) String() string { return string(e) }

// --- Entidade User (raiz do agregado) ------------------------------------------

// User é a ENTIDADE raiz do agregado do subdomínio: identidade (uuid), pertence
// a UMA organization e tem ciclo de vida. A credencial NUNCA sai daqui:
// SenhaHash é `json:"-"` e nenhum DTO a expõe — a comparação é método do
// service do subdomínio. Campos exportados são concessão ao GORM — MUTAÇÃO
// DIRETA fora dos métodos de comportamento é proibida.
type User struct {
	UUID             uuid.UUID      `gorm:"column:uuid;type:uuid;primaryKey" json:"uuid"`
	OrganizationUUID uuid.UUID      `gorm:"column:organization_uuid;type:uuid;not null" json:"organization_uuid"`
	Nome             string         `gorm:"column:nome;not null" json:"nome"`
	Email            Email          `gorm:"column:email;type:text;not null" json:"email"`
	SenhaHash        string         `gorm:"column:senha_hash;not null" json:"-"`
	Status           StatusUsuario  `gorm:"column:status;not null;default:'ativo'" json:"status"`
	CreatedAt        time.Time      `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt        time.Time      `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"column:deleted_at;index" json:"-"`
}

func (User) TableName() string { return "identidade_user_user" }

// CreateInput carrega só o que a regra permite escrever. O hash entra PRONTO
// (gerado pelo service do subdomínio — a política bruta é ValidarSenha);
// OrganizationUUID é preenchido pelo SERVICE a partir do ctx, nunca do corpo.
type CreateInput struct {
	OrganizationUUID uuid.UUID
	Nome             string
	Email            string
	SenhaHash        string
}

// NewUser é o CONSTRUTOR do agregado: valida as invariantes antes de devolver
// a entidade. Controller e service nunca montam entidade campo a campo.
func NewUser(in CreateInput) (*User, error) {
	nome := strings.TrimSpace(in.Nome)
	if len(nome) < 2 || len(nome) > 120 {
		return nil, ErrNomeInvalido
	}
	email, err := ParseEmail(in.Email)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.SenhaHash) == "" || in.OrganizationUUID == uuid.Nil {
		return nil, ErrHashAusente
	}
	return &User{
		UUID:             uuid.New(),
		OrganizationUUID: in.OrganizationUUID,
		Nome:             nome,
		Email:            email,
		SenhaHash:        in.SenhaHash,
		Status:           StatusAtivo,
	}, nil
}

// Renomear — comportamento com invariante de tamanho.
func (u *User) Renomear(nome string) error {
	nome = strings.TrimSpace(nome)
	if len(nome) < 2 || len(nome) > 120 {
		return ErrNomeInvalido
	}
	u.Nome = nome
	return nil
}

// Inativar — transição com invariante: usuário inativo não inativa de novo.
// A revogação das sessões abertas é orquestrada pelo SERVICE (persistência
// não acontece aqui).
func (u *User) Inativar() error {
	if u.Status == StatusInativo {
		return ErrJaInativo
	}
	u.Status = StatusInativo
	return nil
}

// Reativar devolve o usuário ao ar — usuário inativo não autentica.
func (u *User) Reativar() error {
	if u.Status == StatusAtivo {
		return ErrJaAtivo
	}
	u.Status = StatusAtivo
	return nil
}

// Autenticavel informa se o usuário pode entrar — inativo/removido não.
func (u *User) Autenticavel() bool { return u.Status == StatusAtivo }

// UpdateInput — ponteiros distinguem "ausente" de "vazio"; o service traduz
// em chamadas aos métodos de comportamento.
type UpdateInput struct {
	Nome   *string
	Status *StatusUsuario
}

// ListFilter — filtros de listagem; o escopo vem do ctx, nunca do filtro.
// identidade_user_user NÃO tem workspace_uuid: a listagem POR workspace é via
// atribuição (WorkspaceUUID vira EXISTS no repository).
type ListFilter struct {
	Nome          string
	Email         string
	Status        *StatusUsuario
	WorkspaceUUID *uuid.UUID
	pagination.Pagination
}

// --- Entidade RefreshToken -------------------------------------------------------

// RefreshToken é o registro-filho da raiz User: UMA linha por jti com a
// revogação persistida (doc 03). A linha nunca é removida — logout marca
// RevogadoEm; a denylist Redis da evolução é só cache desta verdade.
type RefreshToken struct {
	UUID             uuid.UUID  `gorm:"column:uuid;type:uuid;primaryKey" json:"uuid"`
	OrganizationUUID uuid.UUID  `gorm:"column:organization_uuid;type:uuid;not null" json:"organization_uuid"`
	UserUUID         uuid.UUID  `gorm:"column:user_uuid;type:uuid;not null" json:"user_uuid"`
	JTI              string     `gorm:"column:jti;not null" json:"jti"`
	ExpiraEm         time.Time  `gorm:"column:expira_em;not null" json:"expira_em"`
	RevogadoEm       *time.Time `gorm:"column:revogado_em" json:"revogado_em,omitempty"`
	CreatedAt        time.Time  `gorm:"column:created_at;autoCreateTime" json:"created_at"`
}

func (RefreshToken) TableName() string { return "identidade_user_refresh_token" }

// CreateRefreshTokenInput carrega o que a regra permite escrever. JTI e
// expiração vêm do emissor (infra/jwt); OrganizationUUID é preenchido pelo
// SERVICE a partir do ctx.
type CreateRefreshTokenInput struct {
	OrganizationUUID uuid.UUID
	UserUUID         uuid.UUID
	JTI              string
	ExpiraEm         time.Time
}

// NewRefreshToken valida as invariantes do registro-filho antes de devolvê-lo.
func NewRefreshToken(in CreateRefreshTokenInput) (*RefreshToken, error) {
	if in.OrganizationUUID == uuid.Nil || in.UserUUID == uuid.Nil ||
		strings.TrimSpace(in.JTI) == "" || in.ExpiraEm.IsZero() {
		return nil, ErrRefreshInvalido
	}
	return &RefreshToken{
		UUID:             uuid.New(),
		OrganizationUUID: in.OrganizationUUID,
		UserUUID:         in.UserUUID,
		JTI:              strings.TrimSpace(in.JTI),
		ExpiraEm:         in.ExpiraEm.UTC(),
	}, nil
}

// Ativo informa se a linha representa uma sessão ainda válida.
func (t *RefreshToken) Ativo(agora time.Time) bool {
	return t.RevogadoEm == nil && agora.Before(t.ExpiraEm)
}

// Revogar carimba a revogação — idempotente por construção: token já
// revogado mantém o primeiro carimbo.
func (t *RefreshToken) Revogar(agora time.Time) {
	if t.RevogadoEm == nil {
		momento := agora.UTC()
		t.RevogadoEm = &momento
	}
}

// --- Entidade Atribuicao ---------------------------------------------------------

// Atribuicao liga user × workspace × papel — os papéis são POR workspace: o
// mesmo usuário é admin num workspace e leitor em outro. Registro-filho da
// raiz User (só é tocado pela raiz), tabela criada na F1.
type Atribuicao struct {
	UUID             uuid.UUID      `gorm:"column:uuid;type:uuid;primaryKey" json:"uuid"`
	OrganizationUUID uuid.UUID      `gorm:"column:organization_uuid;type:uuid;not null" json:"organization_uuid"`
	WorkspaceUUID    uuid.UUID      `gorm:"column:workspace_uuid;type:uuid;not null" json:"workspace_uuid"`
	UserUUID         uuid.UUID      `gorm:"column:user_uuid;type:uuid;not null" json:"user_uuid"`
	PapelUUID        uuid.UUID      `gorm:"column:papel_uuid;type:uuid;not null" json:"papel_uuid"`
	CreatedAt        time.Time      `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt        time.Time      `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"column:deleted_at;index" json:"-"`
}

func (Atribuicao) TableName() string { return "identidade_user_atribuicao" }

// CreateAtribuicaoInput — OrganizationUUID é preenchido pelo SERVICE a partir
// do ctx; workspace e papel entram por uuid (referência entre agregados é
// sempre por uuid, nunca join de escrita).
type CreateAtribuicaoInput struct {
	OrganizationUUID uuid.UUID
	WorkspaceUUID    uuid.UUID
	UserUUID         uuid.UUID
	PapelUUID        uuid.UUID
}

// NovaAtribuicao valida as invariantes do vínculo antes de devolvê-lo.
func NovaAtribuicao(in CreateAtribuicaoInput) (*Atribuicao, error) {
	if in.OrganizationUUID == uuid.Nil || in.WorkspaceUUID == uuid.Nil ||
		in.UserUUID == uuid.Nil || in.PapelUUID == uuid.Nil {
		return nil, ErrAtribuicaoInvalida
	}
	return &Atribuicao{
		UUID:             uuid.New(),
		OrganizationUUID: in.OrganizationUUID,
		WorkspaceUUID:    in.WorkspaceUUID,
		UserUUID:         in.UserUUID,
		PapelUUID:        in.PapelUUID,
	}, nil
}

// --- Papéis globais (exceção documentada: sem escopo, F1) ------------------------

// Papel é o papel seed GLOBAL da plataforma (super_admin, admin_organization,
// admin_workspace, usuario_workspace, somente_leitura): sem coluna de escopo
// — exceção documentada em agents/03 e no AGENTS.md do domínio.
type Papel struct {
	UUID      uuid.UUID `gorm:"column:uuid;type:uuid;primaryKey" json:"uuid"`
	Nome      string    `gorm:"column:nome;not null" json:"nome"`
	Descricao string    `gorm:"column:descricao;not null" json:"descricao"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

func (Papel) TableName() string { return "identidade_user_papel" }

// PapelPermissao é a permissão granular concedida a um papel (string
// dominio:subdominio:acao, curingas só para admin) — global, sem escopo.
type PapelPermissao struct {
	PapelUUID uuid.UUID `gorm:"column:papel_uuid;type:uuid;primaryKey" json:"papel_uuid"`
	Permissao string    `gorm:"column:permissao;primaryKey" json:"permissao"`
}

func (PapelPermissao) TableName() string { return "identidade_user_papel_permissao" }

// AtribuicaoComPapel é a PROJEÇÃO de leitura da atribuição com o nome do
// papel — evita N+1 nas listagens sem expor a entidade alheia inteira.
type AtribuicaoComPapel struct {
	Atribuicao
	PapelNome string `gorm:"column:papel_nome" json:"papel"`
}
