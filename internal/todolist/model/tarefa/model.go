// Package tarefa implementa o MODELO do subdomínio tarefa do domínio
// todolist — o PRIMEIRO MÓDULO do monolito, prova ponta a ponta do licensing
// (F10): uma lista de tarefas comum vivendo DENTRO do workspace, protegida
// pelo RequireAplicacao("todolist") na cadeia de acesso.
//
// FOLHA do domínio: não importa domain, application, infra nem middleware —
// qualquer camada pode importá-lo (regra 9 do doc 01).
package tarefa

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"workspace-api/internal/pkg/pagination"
)

const (
	// Dominio e Subdominio identificam este modelo nos catálogos (erros,
	// permissões) e logs.
	Dominio    = "todolist"
	Subdominio = "tarefa"
)

// Sentinelas de invariante do MODELO — o catálogo de erros do subdomínio
// registra estas sentinelas (folha não importa o errors.go de lá).
var (
	ErrTituloInvalido = errors.New("título fora do formato esperado")
	ErrJaConcluida    = errors.New("tarefa já está concluída")
	ErrJaPendente     = errors.New("tarefa já está pendente")
)

// Tarefa é a ENTIDADE raiz do agregado: identidade (uuid), tenancy completo
// (organization + workspace) e ciclo de vida simples. Campos exportados são
// concessão ao GORM — MUTAÇÃO DIRETA fora dos métodos de comportamento é
// proibida.
type Tarefa struct {
	UUID             uuid.UUID      `gorm:"column:uuid;type:uuid;primaryKey" json:"uuid"`
	OrganizationUUID uuid.UUID      `gorm:"column:organization_uuid;type:uuid;not null" json:"organization_uuid"`
	WorkspaceUUID    uuid.UUID      `gorm:"column:workspace_uuid;type:uuid;not null" json:"workspace_uuid"`
	Titulo           string         `gorm:"column:titulo;not null" json:"titulo"`
	Descricao        *string        `gorm:"column:descricao" json:"descricao"`
	Concluida        bool           `gorm:"column:concluida;not null;default:false" json:"concluida"`
	CreatedAt        time.Time      `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt        time.Time      `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"column:deleted_at;index" json:"-"`
}

func (Tarefa) TableName() string { return "todolist_tarefa_tarefa" }

// NewTarefa é o CONSTRUTOR do agregado: valida as invariantes antes de
// devolver a entidade. OrganizationUUID/WorkspaceUUID entram preenchidos
// pelo SERVICE a partir do ctx — nunca do corpo.
func NewTarefa(in CreateInput) (*Tarefa, error) {
	titulo := strings.TrimSpace(in.Titulo)
	if len(titulo) < 2 || len(titulo) > 200 {
		return nil, ErrTituloInvalido
	}
	var descricao *string
	if texto := strings.TrimSpace(in.Descricao); texto != "" {
		descricao = &texto
	}
	return &Tarefa{
		UUID:             uuid.New(),
		OrganizationUUID: in.OrganizationUUID,
		WorkspaceUUID:    in.WorkspaceUUID,
		Titulo:           titulo,
		Descricao:        descricao,
		Concluida:        false,
	}, nil
}

// Renomear — comportamento com invariante de tamanho.
func (t *Tarefa) Renomear(titulo string) error {
	titulo = strings.TrimSpace(titulo)
	if len(titulo) < 2 || len(titulo) > 200 {
		return ErrTituloInvalido
	}
	t.Titulo = titulo
	return nil
}

// EditarDescricao substitui a descrição; vazio remove o texto (NULL).
func (t *Tarefa) EditarDescricao(descricao string) {
	descricao = strings.TrimSpace(descricao)
	if descricao == "" {
		t.Descricao = nil
		return
	}
	t.Descricao = &descricao
}

// Concluir — transição de estado com invariante.
func (t *Tarefa) Concluir() error {
	if t.Concluida {
		return ErrJaConcluida
	}
	t.Concluida = true
	return nil
}

// Reabrir devolve a tarefa ao estado pendente.
func (t *Tarefa) Reabrir() error {
	if !t.Concluida {
		return ErrJaPendente
	}
	t.Concluida = false
	return nil
}

// CreateInput e UpdateInput carregam só o que a regra permite escrever.
// OrganizationUUID/WorkspaceUUID são preenchidos pelo SERVICE a partir do ctx.
type CreateInput struct {
	OrganizationUUID uuid.UUID
	WorkspaceUUID    uuid.UUID
	Titulo           string
	Descricao        string
}

type UpdateInput struct {
	Titulo    *string
	Descricao *string
	Concluida *bool
}

// ListFilter — filtros de listagem; o escopo vem do ctx, nunca do filtro.
type ListFilter struct {
	Concluida *bool
	pagination.Pagination
}
