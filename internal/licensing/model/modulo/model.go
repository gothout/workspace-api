// Package modulo implementa o MODELO do subdomínio módulo do domínio
// licensing: catálogo GLOBAL das aplicações que o monolito oferece — cada
// linha é um app licenciável (ex.: todolist), identificado pelo slug que o
// cliente carrega no header `Application`.
//
// FOLHA do domínio: não importa domain, application, infra nem middleware —
// qualquer camada pode importá-lo (regra 9 do doc 01).
package modulo

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
	Dominio    = "licensing"
	Subdominio = "modulo"
)

// Sentinelas de invariante do MODELO — o pacote é folha e não importa o
// errors.go do subdomínio; o catálogo de lá (code estável + status) registra
// estas sentinelas.
var (
	ErrSlugInvalido = errors.New("slug fora do formato DNS")
	ErrNomeInvalido = errors.New("nome fora do formato esperado")
	ErrJaAtivo      = errors.New("módulo já está ativo")
	ErrJaInativo    = errors.New("módulo já está inativo")
)

// Slug é um VALUE OBJECT: imutável, definido pelo valor, válido desde o
// nascimento. É o valor aceito no header `Application` e o rótulo amarrado
// ao código do monolito (RequireAplicacao) — por isso DNS-safe e único
// GLOBAL (a unicidade em si é garantia do índice único TOTAL no banco).
type Slug string

// ParseSlug valida o formato DNS e devolve o VO; inválido = ErrSlugInvalido.
// A expressão canônica mora em pkg/validator (única fonte); a tag `slugdns`
// do DTO executa a MESMA regex no binding — aqui é a garantia de domínio.
func ParseSlug(valor string) (Slug, error) {
	if !validator.SlugValido(valor) {
		return "", ErrSlugInvalido
	}
	return Slug(valor), nil
}

func (s Slug) String() string { return string(s) }

// Modulo é a ENTIDADE raiz do agregado do subdomínio: tem identidade (uuid),
// vive na plataforma (SEM organization — tabela global) e ciclo de vida.
// Campos exportados são concessão ao GORM — MUTAÇÃO DIRETA fora dos métodos
// de comportamento é proibida.
type Modulo struct {
	UUID      uuid.UUID      `gorm:"column:uuid;type:uuid;primaryKey" json:"uuid"`
	Slug      Slug           `gorm:"column:slug;type:text;not null" json:"slug"`
	Nome      string         `gorm:"column:nome;not null" json:"nome"`
	Descricao *string        `gorm:"column:descricao" json:"descricao"`
	Ativo     bool           `gorm:"column:ativo;not null;default:true" json:"ativo"`
	CreatedAt time.Time      `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at;index" json:"-"`
}

func (Modulo) TableName() string { return "licensing_modulo_modulo" }

// NewModulo é o CONSTRUTOR do agregado: valida as invariantes antes de
// devolver a entidade. Controller e service nunca montam entidade campo a
// campo. Módulo nasce ATIVO — desativar é decisão de negócio posterior.
func NewModulo(in CreateInput) (*Modulo, error) {
	slug, err := ParseSlug(in.Slug)
	if err != nil {
		return nil, err
	}
	nome := strings.TrimSpace(in.Nome)
	if len(nome) < 2 || len(nome) > 120 {
		return nil, ErrNomeInvalido
	}
	var descricao *string
	if texto := strings.TrimSpace(in.Descricao); texto != "" {
		descricao = &texto
	}
	return &Modulo{
		UUID:      uuid.New(),
		Slug:      slug,
		Nome:      nome,
		Descricao: descricao,
		Ativo:     true,
	}, nil
}

// Renomear — comportamento com invariante de tamanho; o service chama este
// método em vez de atribuir m.Nome direto.
func (m *Modulo) Renomear(nome string) error {
	nome = strings.TrimSpace(nome)
	if len(nome) < 2 || len(nome) > 120 {
		return ErrNomeInvalido
	}
	m.Nome = nome
	return nil
}

// EditarDescricao substitui a descrição; vazio remove o texto (NULL).
func (m *Modulo) EditarDescricao(descricao string) {
	descricao = strings.TrimSpace(descricao)
	if descricao == "" {
		m.Descricao = nil
		return
	}
	m.Descricao = &descricao
}

// Desativar — transição de estado com invariante: módulo inativo não
// desativa de novo. Módulo inativo sai da resolução de acesso imediatamente.
func (m *Modulo) Desativar() error {
	if !m.Ativo {
		return ErrJaInativo
	}
	m.Ativo = false
	return nil
}

// Ativar devolve o módulo à resolução de acesso.
func (m *Modulo) Ativar() error {
	if m.Ativo {
		return ErrJaAtivo
	}
	m.Ativo = true
	return nil
}

// CreateInput e UpdateInput carregam só o que a regra permite escrever.
// Tabela global: NÃO há organization_uuid — o escopo aqui é a permissão
// licensing:modulo:* (super_admin), não o tenancy.
type CreateInput struct {
	Nome      string
	Slug      string
	Descricao string
}

type UpdateInput struct {
	Nome      *string
	Descricao *string
	Ativo     *bool
}

// ListFilter — filtros de listagem do catálogo.
type ListFilter struct {
	Nome   string
	Ativo  *bool
	pagination.Pagination
}
