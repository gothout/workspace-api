package tarefa

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"

	modeltarefa "workspace-api/internal/todolist/model/tarefa"
	"workspace-api/internal/pkg/orgctx"
)

// Repository é UM POR AGREGADO: declara, em linguagem de negócio, o que o
// subdomínio precisa perguntar/gravar. Tabela vive DENTRO do workspace —
// escopo completo padrão (orgctx.Scope, fail-closed), sem exceções.
type Repository interface {
	Criar(ctx context.Context, t *modeltarefa.Tarefa) error
	BuscarPorUUID(ctx context.Context, id uuid.UUID) (*modeltarefa.Tarefa, error)
	Listar(ctx context.Context, f modeltarefa.ListFilter) ([]modeltarefa.Tarefa, int64, error)
	Atualizar(ctx context.Context, t *modeltarefa.Tarefa) error
	Remover(ctx context.Context, id uuid.UUID) error
}

type repositoryImpl struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) Repository { return &repositoryImpl{db: db} }

func (r *repositoryImpl) Criar(ctx context.Context, t *modeltarefa.Tarefa) error {
	return orgctx.Scope(r.db.WithContext(ctx), ctx).Create(t).Error
}

func (r *repositoryImpl) BuscarPorUUID(ctx context.Context, id uuid.UUID) (*modeltarefa.Tarefa, error) {
	var t modeltarefa.Tarefa
	err := orgctx.Scope(r.db.WithContext(ctx), ctx).
		Where("uuid = ?", id).First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *repositoryImpl) Listar(ctx context.Context, f modeltarefa.ListFilter) ([]modeltarefa.Tarefa, int64, error) {
	var items []modeltarefa.Tarefa
	var total int64
	q := orgctx.Scope(r.db.WithContext(ctx), ctx).Model(&modeltarefa.Tarefa{})
	if f.Concluida != nil {
		q = q.Where("concluida = ?", *f.Concluida)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("created_at DESC").
		Offset(f.Pagination.Offset()).Limit(f.Pagination.Limit()).
		Find(&items).Error
	return items, total, err
}

func (r *repositoryImpl) Atualizar(ctx context.Context, t *modeltarefa.Tarefa) error {
	return orgctx.Scope(r.db.WithContext(ctx), ctx).Save(t).Error
}

// Remover confere RowsAffected: 0 linhas = inexistente ou fora do escopo —
// ambos viram ErrNotFound (não vaza existência).
func (r *repositoryImpl) Remover(ctx context.Context, id uuid.UUID) error {
	res := orgctx.Scope(r.db.WithContext(ctx), ctx).
		Where("uuid = ?", id).Delete(&modeltarefa.Tarefa{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
