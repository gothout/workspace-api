package ativacao

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	modelativacao "workspace-api/internal/licensing/model/ativacao"
)

// Repository é UM POR AGREGADO: declara, em linguagem de negócio, o que o
// subdomínio precisa perguntar/gravar. Métodos recebem ctx.
//
// EXCEÇÕES de escopo documentadas (AGENTS.md do domínio licensing): a
// administração por workspace alvo (painel da organization) leva o par
// (organization, workspace) como parâmetro EXPLÍCITO validado pelo service;
// ListarSlugsLiberados é a consulta de RESOLUÇÃO do acesso — roda por
// identificador explícito e nunca expõe dados além dos slugs liberados.
type Repository interface {
	Criar(ctx context.Context, a *modelativacao.Ativacao) error
	BuscarPorUUID(ctx context.Context, organizationUUID, workspaceUUID, id uuid.UUID) (*modelativacao.AtivacaoComModulo, error)
	Listar(ctx context.Context, organizationUUID, workspaceUUID uuid.UUID) ([]modelativacao.AtivacaoComModulo, error)
	ListarSlugsLiberados(ctx context.Context, organizationUUID, workspaceUUID uuid.UUID) ([]AplicacaoDisponivelDto, error)
	Remover(ctx context.Context, organizationUUID, workspaceUUID, id uuid.UUID) error
}

type repositoryImpl struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) Repository { return &repositoryImpl{db: db} }

// Consultas cruas via .Table(): o filtro de soft delete NÃO é automático fora
// de Model(&Entidade{}) — por isso explícito em cada JOIN/WHERE.
const projecaoAtivacaoModulo = "licensing_ativacao_ativacao a " +
	"JOIN licensing_modulo_modulo m ON m.uuid = a.modulo_uuid AND m.deleted_at IS NULL " +
	"AND a.deleted_at IS NULL"

const colunasAtivacaoModulo = "a.*, m.slug AS modulo_slug, m.nome AS modulo_nome"

func (r *repositoryImpl) Criar(ctx context.Context, a *modelativacao.Ativacao) error {
	return traduzirErroDriver(r.db.WithContext(ctx).Create(a).Error)
}

func (r *repositoryImpl) BuscarPorUUID(ctx context.Context, organizationUUID, workspaceUUID, id uuid.UUID) (*modelativacao.AtivacaoComModulo, error) {
	var a modelativacao.AtivacaoComModulo
	err := r.db.WithContext(ctx).
		Table(projecaoAtivacaoModulo).
		Select(colunasAtivacaoModulo).
		Where("a.uuid = ? AND a.organization_uuid = ? AND a.workspace_uuid = ?", id, organizationUUID, workspaceUUID).
		Scan(&a).Error
	if err != nil {
		return nil, err
	}
	if a.UUID == uuid.Nil {
		return nil, ErrNotFound
	}
	return &a, nil
}

func (r *repositoryImpl) Listar(ctx context.Context, organizationUUID, workspaceUUID uuid.UUID) ([]modelativacao.AtivacaoComModulo, error) {
	var items []modelativacao.AtivacaoComModulo
	err := r.db.WithContext(ctx).
		Table(projecaoAtivacaoModulo).
		Select(colunasAtivacaoModulo).
		Where("a.organization_uuid = ? AND a.workspace_uuid = ?", organizationUUID, workspaceUUID).
		Order("m.nome ASC").
		Scan(&items).Error
	return items, err
}

// ListarSlugsLiberados é O caminho quente da resolução `Application`:
// licença viva na organization ∩ ativação viva no workspace ∩ módulo ativo.
// Devolve slug+nome prontos para o painel do front; cache entra NA FRENTE
// desta consulta (F8), Postgres é a fonte da verdade.
func (r *repositoryImpl) ListarSlugsLiberados(ctx context.Context, organizationUUID, workspaceUUID uuid.UUID) ([]AplicacaoDisponivelDto, error) {
	var items []AplicacaoDisponivelDto
	err := r.db.WithContext(ctx).
		Table(projecaoAtivacaoModulo +
			" JOIN licensing_licenca_licenca lic ON lic.organization_uuid = a.organization_uuid " +
			"AND lic.modulo_uuid = a.modulo_uuid AND lic.deleted_at IS NULL").
		Select("DISTINCT m.slug, m.nome").
		Where("a.organization_uuid = ? AND a.workspace_uuid = ? AND m.ativo = ?", organizationUUID, workspaceUUID, true).
		Order("m.nome ASC").
		Scan(&items).Error
	return items, err
}

// Remover confere RowsAffected: 0 linhas = inexistente ou fora do par alvo —
// ambos viram ErrNotFound (não vaza existência).
func (r *repositoryImpl) Remover(ctx context.Context, organizationUUID, workspaceUUID, id uuid.UUID) error {
	res := r.db.WithContext(ctx).
		Where("uuid = ? AND organization_uuid = ? AND workspace_uuid = ?", id, organizationUUID, workspaceUUID).
		Delete(&modelativacao.Ativacao{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// traduzirErroDriver: conflito de unicidade do Postgres (SQLSTATE 23505) vira
// a sentinela 409 — índice único parcial (workspace, módulo) vivos.
func traduzirErroDriver(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrJaAtivada
	}
	return err
}
