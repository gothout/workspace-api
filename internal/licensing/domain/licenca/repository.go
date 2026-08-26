package licenca

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	modellicenca "workspace-api/internal/licensing/model/licenca"
	"workspace-api/internal/pkg/orgctx"
)

// Repository é UM POR AGREGADO: declara, em linguagem de negócio, o que o
// subdomínio precisa perguntar/gravar. Métodos recebem ctx.
//
// EXCEÇÕES de escopo documentadas (AGENTS.md do domínio licensing): a
// licença é ESCRITA pelo super_admin CRUZANDO organizations — as consultas
// de administração levam a organization ALVO como parâmetro EXPLÍCITO,
// validado no service (mesma org do ctx OU permissão ler_plataforma). A
// leitura do próprio tenant usa orgctx.ScopeOrganization; ExisteParaModulo
// é GLOBAL por natureza (guarda de remoção é regra da plataforma).
type Repository interface {
	Criar(ctx context.Context, l *modellicenca.Licenca) error
	BuscarPorUUID(ctx context.Context, id uuid.UUID) (*modellicenca.LicencaComModulo, error)
	BuscarPorUUIDNaOrganization(ctx context.Context, organizationUUID, id uuid.UUID) (*modellicenca.LicencaComModulo, error)
	Listar(ctx context.Context, organizationUUID uuid.UUID) ([]modellicenca.LicencaComModulo, error)
	ExisteParaModulo(ctx context.Context, moduloUUID uuid.UUID) (bool, error)
	// ExisteNaOrganization é a pergunta da ATIVAÇÃO: esta organization tem
	// licença viva deste módulo? Consulta por par explícito (o chamador já
	// validou o alvo).
	ExisteNaOrganization(ctx context.Context, organizationUUID, moduloUUID uuid.UUID) (bool, error)
	Revogar(ctx context.Context, organizationUUID, id uuid.UUID) error
}

type repositoryImpl struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) Repository { return &repositoryImpl{db: db} }

// Consulta crua via .Table(): o filtro de soft delete NÃO é automático aqui
// (gorm só o aplica em Model(&Entidade{})) — por isso explícito no JOIN.
const projecaoLicencaModulo = "licensing_licenca_licenca l " +
	"JOIN licensing_modulo_modulo m ON m.uuid = l.modulo_uuid AND m.deleted_at IS NULL " +
	"AND l.deleted_at IS NULL"

const colunasLicencaModulo = "l.*, m.slug AS modulo_slug, m.nome AS modulo_nome"

func (r *repositoryImpl) Criar(ctx context.Context, l *modellicenca.Licenca) error {
	return traduzirErroDriver(r.db.WithContext(ctx).Create(l).Error)
}

// BuscarPorUUID é a leitura ESCOPADA da própria organization.
func (r *repositoryImpl) BuscarPorUUID(ctx context.Context, id uuid.UUID) (*modellicenca.LicencaComModulo, error) {
	var l modellicenca.LicencaComModulo
	err := orgctx.ScopeOrganization(r.db.WithContext(ctx), ctx).
		Table(projecaoLicencaModulo).
		Select(colunasLicencaModulo).
		Where("l.uuid = ?", id).
		Scan(&l).Error
	if err != nil {
		return nil, err
	}
	if l.UUID == uuid.Nil {
		return nil, ErrNotFound
	}
	return &l, nil
}

// BuscarPorUUIDNaOrganization atende o super_admin cruzando organizations:
// o par (organization alvo, uuid) é validado antes pela autorização rota a
// rota + service.
func (r *repositoryImpl) BuscarPorUUIDNaOrganization(ctx context.Context, organizationUUID, id uuid.UUID) (*modellicenca.LicencaComModulo, error) {
	var l modellicenca.LicencaComModulo
	err := r.db.WithContext(ctx).
		Table(projecaoLicencaModulo).
		Select(colunasLicencaModulo).
		Where("l.uuid = ? AND l.organization_uuid = ?", id, organizationUUID).
		Scan(&l).Error
	if err != nil {
		return nil, err
	}
	if l.UUID == uuid.Nil {
		return nil, ErrNotFound
	}
	return &l, nil
}

func (r *repositoryImpl) Listar(ctx context.Context, organizationUUID uuid.UUID) ([]modellicenca.LicencaComModulo, error) {
	var items []modellicenca.LicencaComModulo
	err := r.db.WithContext(ctx).
		Table(projecaoLicencaModulo).
		Select(colunasLicencaModulo).
		Where("l.organization_uuid = ?", organizationUUID).
		Order("m.nome ASC").
		Scan(&items).Error
	return items, err
}

// ExisteParaModulo é GLOBAL por natureza: a remoção de um módulo do catálogo
// só é permitida sem NENHUMA concessão viva em qualquer organization.
func (r *repositoryImpl) ExisteParaModulo(ctx context.Context, moduloUUID uuid.UUID) (bool, error) {
	var total int64
	err := r.db.WithContext(ctx).
		Table("licensing_licenca_licenca").
		Where("modulo_uuid = ? AND deleted_at IS NULL", moduloUUID).
		Count(&total).Error
	return total > 0, err
}

// ExisteNaOrganization responde ao contrato VerificadorLicencas da ativação:
// par (organization, módulo) explícito, filtro de soft delete manual.
func (r *repositoryImpl) ExisteNaOrganization(ctx context.Context, organizationUUID, moduloUUID uuid.UUID) (bool, error) {
	var total int64
	err := r.db.WithContext(ctx).
		Table("licensing_licenca_licenca").
		Where("organization_uuid = ? AND modulo_uuid = ? AND deleted_at IS NULL", organizationUUID, moduloUUID).
		Count(&total).Error
	return total > 0, err
}

// Revogar confere RowsAffected: 0 linhas = inexistente ou fora da organization
// alvo — ambos viram ErrNotFound (não vaza existência).
func (r *repositoryImpl) Revogar(ctx context.Context, organizationUUID, id uuid.UUID) error {
	res := r.db.WithContext(ctx).
		Where("uuid = ? AND organization_uuid = ?", id, organizationUUID).
		Delete(&modellicenca.Licenca{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// traduzirErroDriver: conflito de unicidade do Postgres (SQLSTATE 23505) vira
// a sentinela 409 — índice único parcial (organization, módulo) vivos.
func traduzirErroDriver(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrJaConcedida
	}
	return err
}
