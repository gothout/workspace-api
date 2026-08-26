// Package workspace implementa o MODELO do subdomínio workspace do domínio
// identidade: filho da organization e dono do endereço público
// {slug}.{base_domain} — entidade raiz do agregado, VO Slug e invariantes.
//
// FOLHA do domínio: não importa domain, application, infra nem middleware —
// qualquer camada pode importá-lo (regra 9 do doc 01).
package workspace

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"workspace-api/internal/pkg/pagination"
	"workspace-api/internal/pkg/validator"
)

const (
	// Dominio e Subdominio identificam este modelo nos catálogos (erros,
	// permissões) e logs.
	Dominio    = "identidade"
	Subdominio = "workspace"
)

// Sentinelas de invariante do MODELO — o pacote é folha e não importa o
// errors.go do subdomínio; o catálogo de lá (code estável + status) registra
// estas sentinelas.
var (
	ErrSlugInvalido = errors.New("slug fora do formato DNS")
	ErrNomeInvalido = errors.New("nome fora do formato esperado")
	ErrJaInativo    = errors.New("workspace já está inativo")
	ErrJaAtivo      = errors.New("workspace já está ativo")
)

// StatusWorkspace — ciclo de vida do workspace. Inativar suspende a
// resolução pelo Host na hora; reativação é ação de negócio própria.
type StatusWorkspace string

const (
	StatusAtivo   StatusWorkspace = "ativo"
	StatusInativo StatusWorkspace = "inativo"
)

// Valido confere se o valor está no conjunto fechado — todo tipo nomeado tem um.
func (s StatusWorkspace) Valido() bool {
	switch s {
	case StatusAtivo, StatusInativo:
		return true
	}
	return false
}

// Slug é um VALUE OBJECT: imutável, definido pelo valor, válido desde o
// nascimento. É o rótulo do endereço {slug}.{base_domain} da plataforma —
// por isso DNS-safe e único GLOBAL (a unicidade em si é garantia do índice
// único TOTAL no banco; a lista de reservados é regra do service do
// subdomínio, não daqui).
type Slug string

// ParseSlug valida o formato DNS e devolve o VO; inválido = ErrSlugInvalido.
// A expressão canônica mora em pkg/validator (única fonte: pkg é folha,
// alcançável por todos; R7 deduplicou a cópia que vivia aqui) — a tag
// `slugdns` do DTO executa a MESMA regex no binding; aqui é a garantia de
// domínio. (A tag é primeira linha de defesa.)
func ParseSlug(valor string) (Slug, error) {
	if !validator.SlugValido(valor) {
		return "", ErrSlugInvalido
	}
	return Slug(valor), nil
}

func (s Slug) String() string { return string(s) }

// Workspace é a ENTIDADE raiz do agregado do subdomínio: tem identidade
// (uuid), pertence a UMA organization (não existe workspace órfão) e ciclo
// de vida. Campos exportados são concessão ao GORM — MUTAÇÃO DIRETA fora dos
// métodos de comportamento é proibida.
type Workspace struct {
	UUID             uuid.UUID       `gorm:"column:uuid;type:uuid;primaryKey" json:"uuid"`
	OrganizationUUID uuid.UUID       `gorm:"column:organization_uuid;type:uuid;not null" json:"organization_uuid"`
	Nome             string          `gorm:"column:nome;not null" json:"nome"`
	Slug             Slug            `gorm:"column:slug;type:text;not null" json:"slug"`
	Status           StatusWorkspace `gorm:"column:status;not null;default:'ativo'" json:"status"`
	CreatedAt        time.Time       `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt        time.Time       `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
	DeletedAt        gorm.DeletedAt  `gorm:"column:deleted_at;index" json:"-"`
}

func (Workspace) TableName() string { return "identidade_workspace_workspace" }

// NewWorkspace é o CONSTRUTOR do agregado: valida as invariantes antes de
// devolver a entidade. Controller e service nunca montam entidade campo a
// campo. OrganizationUUID entra preenchido pelo SERVICE a partir do ctx —
// nunca do corpo da requisição.
func NewWorkspace(in CreateInput) (*Workspace, error) {
	slug, err := ParseSlug(in.Slug)
	if err != nil {
		return nil, err
	}
	nome := strings.TrimSpace(in.Nome)
	if len(nome) < 2 || len(nome) > 120 {
		return nil, ErrNomeInvalido
	}
	return &Workspace{
		UUID:             uuid.New(),
		OrganizationUUID: in.OrganizationUUID,
		Nome:             nome,
		Slug:             slug,
		Status:           StatusAtivo,
	}, nil
}

// Renomear — comportamento com invariante de tamanho; o service chama este
// método em vez de atribuir w.Nome direto.
func (w *Workspace) Renomear(nome string) error {
	nome = strings.TrimSpace(nome)
	if len(nome) < 2 || len(nome) > 120 {
		return ErrNomeInvalido
	}
	w.Nome = nome
	return nil
}

// Inativar — transição de estado com invariante: workspace inativo não
// inativa de novo. Workspace inativo sai da resolução pelo Host (404).
func (w *Workspace) Inativar() error {
	if w.Status == StatusInativo {
		return ErrJaInativo
	}
	w.Status = StatusInativo
	return nil
}

// Reativar devolve o workspace ao ar — ação de negócio própria
// (POST .../acoes/reativar), nunca PATCH de campo.
func (w *Workspace) Reativar() error {
	if w.Status == StatusAtivo {
		return ErrJaAtivo
	}
	w.Status = StatusAtivo
	return nil
}

// CreateInput e UpdateInput carregam só o que a regra permite escrever.
// OrganizationUUID é preenchido pelo SERVICE a partir do ctx; a EXCEÇÃO é a
// gestão cross-tenant da plataforma (UX4): OrganizationPedida chega do corpo,
// e o service só a honra para chamador com posse exata de `*:*` (super_admin)
// — para os demais ela vira recusa fora_do_escopo ou é equivalente ao escopo.
type CreateInput struct {
	OrganizationUUID   uuid.UUID
	OrganizationPedida *uuid.UUID
	Nome               string
	Slug               string
}

type UpdateInput struct {
	Nome   *string
	Status *StatusWorkspace
}

// ListFilter — filtros de listagem; o escopo vem do ctx, nunca do filtro.
// EXCEÇÃO (UX4): OrganizationUUID é o filtro de PLATAFORMA (posse exata de
// `*:*`) para listar os workspaces de qualquer organization — o service
// valida o chamador antes de aplicá-lo; não-plataforma apontando alheia é
// recusado. form:"-" porque entra parseado pelo controller (query string),
// nunca pelo binding automático.
type ListFilter struct {
	Nome             string
	Status           *StatusWorkspace
	OrganizationUUID *uuid.UUID `form:"-"`
	pagination.Pagination
}
