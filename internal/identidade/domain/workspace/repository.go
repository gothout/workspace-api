package workspace

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	modelworkspace "workspace-api/internal/identidade/model/workspace"
	"workspace-api/internal/pkg/orgctx"
)

// Repository é UM POR AGREGADO: declara, em linguagem de negócio, o que o
// subdomínio precisa perguntar/gravar — nunca um "CRUD genérico". Métodos
// recebem ctx; o escopo sai dele.
//
// EXCEÇÕES de escopo documentadas (agents/03 e AGENTS.md do pacote):
//   - BuscarPorSlug é GLOBAL — a resolução pelo Host acontece ANTES de
//     existir escopo (o Host não diz de qual organization o slug é); o
//     resultado nunca é exposto em rota de administração.
//   - BuscarPorUUIDGlobal atende o fallback X-Workspace-Id (acesso direto e
//     dev local), mesma natureza: resolução antes do escopo, resultado nunca
//     exposto em administração — a leitura de administração é BuscarPorUUID,
//     escopada por orgctx.ScopeOrganization.
type Repository interface {
	Criar(ctx context.Context, w *modelworkspace.Workspace) error
	BuscarPorUUID(ctx context.Context, id uuid.UUID) (*modelworkspace.Workspace, error)
	BuscarPorSlug(ctx context.Context, slug modelworkspace.Slug) (*modelworkspace.Workspace, error)
	BuscarPorUUIDGlobal(ctx context.Context, id uuid.UUID) (*modelworkspace.Workspace, error)
	Listar(ctx context.Context, f modelworkspace.ListFilter) ([]modelworkspace.Workspace, int64, error)
	Atualizar(ctx context.Context, w *modelworkspace.Workspace) error
	Remover(ctx context.Context, id uuid.UUID) error
	SuspenderPorOrganization(ctx context.Context, organizationUUID uuid.UUID) (int64, error)
}

type repositoryImpl struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) Repository { return &repositoryImpl{db: db} }

func (r *repositoryImpl) Criar(ctx context.Context, w *modelworkspace.Workspace) error {
	return traduzirErroDriver(orgctx.ScopeOrganization(r.db.WithContext(ctx), ctx).Create(w).Error)
}

func (r *repositoryImpl) BuscarPorUUID(ctx context.Context, id uuid.UUID) (*modelworkspace.Workspace, error) {
	var w modelworkspace.Workspace
	err := orgctx.ScopeOrganization(r.db.WithContext(ctx), ctx).
		Where("uuid = ?", id).First(&w).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

// BuscarPorSlug é a EXCEÇÃO de escopo documentada acima: query GLOBAL usada
// na resolução pelo Host (antes de existir escopo). Soft delete do gorm
// mantém workspace removido fora; inativo volta como StatusInativo e quem
// consome decide o 404 — aqui não se distingue para não vazar existência.
func (r *repositoryImpl) BuscarPorSlug(ctx context.Context, slug modelworkspace.Slug) (*modelworkspace.Workspace, error) {
	var w modelworkspace.Workspace
	err := r.db.WithContext(ctx).
		Where("slug = ?", slug).First(&w).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

// BuscarPorUUIDGlobal é a segunda EXCEÇÃO documentada acima (fallback
// X-Workspace-Id): query GLOBAL por identificador, mesmo tratamento de
// vitalidade da BuscarPorSlug.
func (r *repositoryImpl) BuscarPorUUIDGlobal(ctx context.Context, id uuid.UUID) (*modelworkspace.Workspace, error) {
	var w modelworkspace.Workspace
	err := r.db.WithContext(ctx).
		Where("uuid = ?", id).First(&w).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

func (r *repositoryImpl) Listar(ctx context.Context, f modelworkspace.ListFilter) ([]modelworkspace.Workspace, int64, error) {
	var items []modelworkspace.Workspace
	var total int64
	q := orgctx.ScopeOrganization(r.db.WithContext(ctx), ctx).Model(&modelworkspace.Workspace{})
	if f.Nome != "" {
		q = q.Where("nome ILIKE ?", "%"+f.Nome+"%")
	}
	if f.Status != nil {
		q = q.Where("status = ?", *f.Status)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("created_at DESC").
		Offset(f.Pagination.Offset()).Limit(f.Pagination.Limit()).
		Find(&items).Error
	return items, total, err
}

func (r *repositoryImpl) Atualizar(ctx context.Context, w *modelworkspace.Workspace) error {
	return traduzirErroDriver(orgctx.ScopeOrganization(r.db.WithContext(ctx), ctx).Save(w).Error)
}

// Remover confere RowsAffected: 0 linhas = registro inexistente ou fora do
// escopo — ambos viram ErrNotFound (não vaza existência).
func (r *repositoryImpl) Remover(ctx context.Context, id uuid.UUID) error {
	res := orgctx.ScopeOrganization(r.db.WithContext(ctx), ctx).
		Where("uuid = ?", id).Delete(&modelworkspace.Workspace{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// SuspenderPorOrganization executa o lado workspace da cascata de
// inativação da organization (contrato SuspendedorWorkspaces declarado no
// irmão, ligado no cmd/bootstrap): inativa TODOS os workspaces ativos dela —
// filho nunca fica mais vivo que o pai. Idempotente: rodar de novo devolve 0.
// O ctx chega já escopado na organization alvo (garantido pelo service da
// organization); o ScopeOrganization mantém a query fail-closed.
func (r *repositoryImpl) SuspenderPorOrganization(ctx context.Context, organizationUUID uuid.UUID) (int64, error) {
	res := orgctx.ScopeOrganization(r.db.WithContext(ctx), ctx).Model(&modelworkspace.Workspace{}).
		Where("organization_uuid = ? AND status = ?", organizationUUID, modelworkspace.StatusAtivo).
		Update("status", modelworkspace.StatusInativo)
	return res.RowsAffected, res.Error
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
