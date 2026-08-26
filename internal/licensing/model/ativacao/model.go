// Package ativacao implementa o MODELO do subdomínio ativação do domínio
// licensing: a ponte licença → uso — a organization aplica um módulo que ela
// tem licenciado a um workspace dela. É esta tabela, cruzada com a licença
// viva, que a resolução de acesso consulta para responder ao header
// `Application`.
//
// FOLHA do domínio: não importa domain, application, infra nem middleware —
// qualquer camada pode importá-lo (regra 9 do doc 01).
package ativacao

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	// Dominio e Subdominio identificam este modelo nos catálogos (erros,
	// permissões) e logs.
	Dominio    = "licensing"
	Subdominio = "ativacao"
)

// Ativacao é a ENTIDADE raiz do agregado: liga UM workspace a UM módulo,
// sempre dentro da organization dona dos dois. BINÁRIA como a licença —
// linha viva = ativa; remoção lógica = desativada.
type Ativacao struct {
	UUID             uuid.UUID      `gorm:"column:uuid;type:uuid;primaryKey" json:"uuid"`
	OrganizationUUID uuid.UUID      `gorm:"column:organization_uuid;type:uuid;not null" json:"organization_uuid"`
	WorkspaceUUID    uuid.UUID      `gorm:"column:workspace_uuid;type:uuid;not null" json:"workspace_uuid"`
	ModuloUUID       uuid.UUID      `gorm:"column:modulo_uuid;type:uuid;not null" json:"modulo_uuid"`
	AtivadoPor       *uuid.UUID     `gorm:"column:ativado_por;type:uuid" json:"ativado_por"`
	CreatedAt        time.Time      `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt        time.Time      `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"column:deleted_at;index" json:"-"`
}

func (Ativacao) TableName() string { return "licensing_ativacao_ativacao" }

// NewAtivacao é o CONSTRUTOR do agregado: os três UUIDs entram validados pelo
// service (workspace pertence à org; org tem licença do módulo) — quem
// ativou sai do ctx, nunca do corpo.
func NewAtivacao(in CreateInput) *Ativacao {
	var ativadoPor *uuid.UUID
	if in.AtivadoPor != uuid.Nil {
		v := in.AtivadoPor
		ativadoPor = &v
	}
	return &Ativacao{
		UUID:             uuid.New(),
		OrganizationUUID: in.OrganizationUUID,
		WorkspaceUUID:    in.WorkspaceUUID,
		ModuloUUID:       in.ModuloUUID,
		AtivadoPor:       ativadoPor,
	}
}

// CreateInput carrega só o que a regra permite escrever.
type CreateInput struct {
	OrganizationUUID uuid.UUID
	WorkspaceUUID    uuid.UUID
	ModuloUUID       uuid.UUID
	AtivadoPor       uuid.UUID // uuid.Nil = ativação sem usuário (seed/CLI)
}

// AtivacaoComModulo é a PROJEÇÃO de leitura com o resumo do módulo (join) —
// o painel de módulos do workspace exibe tudo numa linha só.
type AtivacaoComModulo struct {
	Ativacao
	ModuloSlug string `gorm:"column:modulo_slug" json:"modulo_slug"`
	ModuloNome string `gorm:"column:modulo_nome" json:"modulo_nome"`
}

// AplicacaoDisponivelDto é o item da lista "quais aplicações posso usar
// aqui" — folha: consumida pelo seletor do front (aplicação aplicacoes) sem
// importar o domain.
type AplicacaoDisponivelDto struct {
	Slug string `json:"slug"`
	Nome string `json:"nome"`
}
