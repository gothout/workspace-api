package modulo

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	modelmodulo "workspace-api/internal/licensing/model/modulo"
)

// Repository é UM POR AGREGADO: declara, em linguagem de negócio, o que o
// subdomínio precisa perguntar/gravar. Métodos recebem ctx.
//
// EXCEÇÃO de escopo documentada (AGENTS.md do domínio licensing): a tabela
// do catálogo é GLOBAL da plataforma — SEM organization_uuid, logo SEM
// orgctx.Scope*. O fail-closed aqui é a PERMISSÃO rota a rota
// (licensing:modulo:*, escrita exclusiva do super_admin), não o tenancy.
type Repository interface {
	Criar(ctx context.Context, m *modelmodulo.Modulo) error
	BuscarPorUUID(ctx context.Context, id uuid.UUID) (*modelmodulo.Modulo, error)
	BuscarPorSlug(ctx context.Context, slug modelmodulo.Slug) (*modelmodulo.Modulo, error)
	Listar(ctx context.Context, f modelmodulo.ListFilter) ([]modelmodulo.Modulo, int64, error)
	Atualizar(ctx context.Context, m *modelmodulo.Modulo) error
	Remover(ctx context.Context, id uuid.UUID) error
}

type repositoryImpl struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) Repository { return &repositoryImpl{db: db} }

func (r *repositoryImpl) Criar(ctx context.Context, m *modelmodulo.Modulo) error {
	return traduzirErroDriver(r.db.WithContext(ctx).Create(m).Error)
}

func (r *repositoryImpl) BuscarPorUUID(ctx context.Context, id uuid.UUID) (*modelmodulo.Modulo, error) {
	var m modelmodulo.Modulo
	err := r.db.WithContext(ctx).Where("uuid = ?", id).First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// BuscarPorSlug alimenta a resolução de acesso e as telas de licenciamento —
// consulta global por natureza (catálogo da plataforma inteira).
func (r *repositoryImpl) BuscarPorSlug(ctx context.Context, slug modelmodulo.Slug) (*modelmodulo.Modulo, error) {
	var m modelmodulo.Modulo
	err := r.db.WithContext(ctx).Where("slug = ?", slug).First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *repositoryImpl) Listar(ctx context.Context, f modelmodulo.ListFilter) ([]modelmodulo.Modulo, int64, error) {
	var items []modelmodulo.Modulo
	var total int64
	q := r.db.WithContext(ctx).Model(&modelmodulo.Modulo{})
	if f.Nome != "" {
		q = q.Where("nome ILIKE ?", "%"+f.Nome+"%")
	}
	if f.Ativo != nil {
		q = q.Where("ativo = ?", *f.Ativo)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("nome ASC").
		Offset(f.Pagination.Offset()).Limit(f.Pagination.Limit()).
		Find(&items).Error
	return items, total, err
}

func (r *repositoryImpl) Atualizar(ctx context.Context, m *modelmodulo.Modulo) error {
	return traduzirErroDriver(r.db.WithContext(ctx).Save(m).Error)
}

// Remover confere RowsAffected: 0 linhas = registro inexistente → ErrNotFound.
func (r *repositoryImpl) Remover(ctx context.Context, id uuid.UUID) error {
	res := r.db.WithContext(ctx).Where("uuid = ?", id).Delete(&modelmodulo.Modulo{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// traduzirErroDriver: conflito de unicidade do Postgres (SQLSTATE 23505) vira
// a sentinela 409 — só existe um índice único nesta tabela (slug, TOTAL).
func traduzirErroDriver(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrSlugEmUso
	}
	return err
}
