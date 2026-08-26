// Package licenca implementa o MODELO do subdomínio licença do domínio
// licensing: a concessão de um módulo a uma organization, feita pelo
// super_admin. BINÁRIA por decisão de produto — linha viva = licenciada;
// remoção lógica = revogada (histórico preservado pelo soft delete).
//
// FOLHA do domínio: não importa domain, application, infra nem middleware —
// qualquer camada pode importá-lo (regra 9 do doc 01).
package licenca

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	// Dominio e Subdominio identificam este modelo nos catálogos (erros,
	// permissões) e logs.
	Dominio    = "licensing"
	Subdominio = "licenca"
)

// Licenca é a ENTIDADE raiz do agregado: pertence a UMA organization e aponta
// para UM módulo do catálogo. Campos exportados são concessão ao GORM — não
// há transição de estado além de existir/revogar (binária), então o agregado
// não carrega métodos de comportamento.
type Licenca struct {
	UUID             uuid.UUID      `gorm:"column:uuid;type:uuid;primaryKey" json:"uuid"`
	OrganizationUUID uuid.UUID      `gorm:"column:organization_uuid;type:uuid;not null" json:"organization_uuid"`
	ModuloUUID       uuid.UUID      `gorm:"column:modulo_uuid;type:uuid;not null" json:"modulo_uuid"`
	ConcedidaPor     *uuid.UUID     `gorm:"column:concedida_por;type:uuid" json:"concedida_por"`
	CreatedAt        time.Time      `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt        time.Time      `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"column:deleted_at;index" json:"-"`
}

func (Licenca) TableName() string { return "licensing_licenca_licenca" }

// NewLicenca é o CONSTRUTOR do agregado: quem concede entra preenchido pelo
// SERVICE a partir do ctx — nunca do corpo da requisição.
func NewLicenca(in CreateInput) *Licenca {
	var concedidaPor *uuid.UUID
	if in.ConcedidaPor != uuid.Nil {
		v := in.ConcedidaPor
		concedidaPor = &v
	}
	return &Licenca{
		UUID:             uuid.New(),
		OrganizationUUID: in.OrganizationUUID,
		ModuloUUID:       in.ModuloUUID,
		ConcedidaPor:     concedidaPor,
	}
}

// CreateInput carrega só o que a regra permite escrever.
type CreateInput struct {
	OrganizationUUID uuid.UUID
	ModuloUUID       uuid.UUID
	ConcedidaPor     uuid.UUID // uuid.Nil = concessão sem usuário (seed/CLI)
}

// LicencaComModulo é a PROJEÇÃO de leitura com o resumo do módulo (join) —
// o painel de licenças exibe tudo numa linha só, sem N chamadas.
type LicencaComModulo struct {
	Licenca
	ModuloSlug string `gorm:"column:modulo_slug" json:"modulo_slug"`
	ModuloNome string `gorm:"column:modulo_nome" json:"modulo_nome"`
}
