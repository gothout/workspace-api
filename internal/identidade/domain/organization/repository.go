package organization

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	orgmodel "workspace-api/internal/identidade/model/organization"
	"workspace-api/internal/pkg/orgctx"
	"workspace-api/internal/pkg/pagination"
)

// Repository é UM POR AGREGADO: a raiz Organization e o registro-filho ApiKey.
// Métodos em linguagem de negócio; toda query recebe ctx.
//
// EXCEÇÕES de escopo documentadas (agents/03 e AGENTS.md do pacote):
//   - identidade_organization_organization é a RAIZ da hierarquia — não tem
//     organization_uuid para filtrar (ela define o escopo de todo o resto);
//     o acesso é controlado pela permissão própria de cada rota + conferência
//     de pertencimento no service (exigirOrganizacaoDoContexto).
//   - BuscarApiKeyPorHash é GLOBAL: o resolvedor do middleware valida
//     X-Api-Key antes de existir escopo (mesma natureza do FindBySlug do
//     workspace); o resultado nunca vaza para rota de administração.
type Repository interface {
	Criar(ctx context.Context, o *orgmodel.Organization) error
	BuscarPorUUID(ctx context.Context, id uuid.UUID) (*orgmodel.Organization, error)
	Listar(ctx context.Context, f orgmodel.ListFilter) ([]orgmodel.Organization, int64, error)
	Atualizar(ctx context.Context, o *orgmodel.Organization) error
	Remover(ctx context.Context, id uuid.UUID) error
	ListarDominiosAtivos(ctx context.Context) ([]LinhaDominioAtivo, error)
	// ListarOpcoes devolve uuid+nome das organizations para os Selects do
	// painel de logs (issue #30): ponteiro nil = TODAS (caminho da
	// plataforma — a tabela raiz não tem escopo acima, exceção documentada);
	// preenchido = só a pedida. Leitura de referência, sem paginação.
	ListarOpcoes(ctx context.Context, organizacaoUUID *uuid.UUID) ([]orgmodel.Organization, error)
}

// RepositorioApiKeys opera o registro-filho do agregado — sempre amarrado à
// organization dona, que o service já conferiu contra o ctx.
type RepositorioApiKeys interface {
	CriarApiKey(ctx context.Context, k *orgmodel.ApiKey) error
	ListarApiKeys(ctx context.Context, organizationUUID uuid.UUID, p pagination.Pagination) ([]orgmodel.ApiKey, int64, error)
	BuscarApiKeyPorUUID(ctx context.Context, organizationUUID, chaveUUID uuid.UUID) (*orgmodel.ApiKey, error)
	BuscarApiKeyPorHash(ctx context.Context, hash string) (*orgmodel.ApiKey, error)
	RemoverApiKey(ctx context.Context, organizationUUID, chaveUUID uuid.UUID) error
	// RevogarApiKeysDaOrganization encerra TODAS as chaves ativas da
	// organization (remoção lógica em cascata) e devolve quantas foram
	// revogadas — lado da própria raiz na inativação/remoção (R4).
	// Idempotente: sem chave ativa devolve 0 sem erro.
	RevogarApiKeysDaOrganization(ctx context.Context, organizationUUID uuid.UUID) (int64, error)
}

// LinhaDominioAtivo é a projeção mínima dos domínios custom ativos — o que o
// provedor do middleware/CORS consome; nada do modelo interno atravessa aqui.
type LinhaDominioAtivo struct {
	Valor            string
	OrganizationUUID uuid.UUID
}

type repositoryImpl struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) Repository { return &repositoryImpl{db: db} }

type repositorioApiKeysImpl struct{ db *gorm.DB }

func NewRepositorioApiKeys(db *gorm.DB) RepositorioApiKeys { return &repositorioApiKeysImpl{db: db} }

func (r *repositoryImpl) Criar(ctx context.Context, o *orgmodel.Organization) error {
	return traduzirErroDriver(r.db.WithContext(ctx).Create(o).Error, ErrDominioEmUso)
}

func (r *repositoryImpl) BuscarPorUUID(ctx context.Context, id uuid.UUID) (*orgmodel.Organization, error) {
	var o orgmodel.Organization
	err := r.db.WithContext(ctx).Where("uuid = ?", id).First(&o).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

func (r *repositoryImpl) Listar(ctx context.Context, f orgmodel.ListFilter) ([]orgmodel.Organization, int64, error) {
	var items []orgmodel.Organization
	var total int64
	q := r.db.WithContext(ctx).Model(&orgmodel.Organization{})
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

func (r *repositoryImpl) Atualizar(ctx context.Context, o *orgmodel.Organization) error {
	return traduzirErroDriver(r.db.WithContext(ctx).Save(o).Error, ErrDominioEmUso)
}

func (r *repositoryImpl) Remover(ctx context.Context, id uuid.UUID) error {
	res := r.db.WithContext(ctx).
		Where("uuid = ?", id).Delete(&orgmodel.Organization{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *repositoryImpl) ListarDominiosAtivos(ctx context.Context) ([]LinhaDominioAtivo, error) {
	var linhas []struct {
		Dominio          string
		OrganizationUUID uuid.UUID
	}
	err := r.db.WithContext(ctx).
		Table("identidade_organization_organization").
		// A raiz é dona dela mesma: o organization_uuid projetado é o uuid da linha.
		// Consulta por Table() cru NÃO ganha filtro automático de soft delete —
		// organization removida sai da resolução na hora (invariante do doc 03).
		Select("dominio, uuid AS organization_uuid").
		Where("dominio IS NOT NULL AND dominio <> '' AND status = 'ativo' AND deleted_at IS NULL").
		Order("dominio").
		Scan(&linhas).Error
	if err != nil {
		return nil, err
	}
	lista := make([]LinhaDominioAtivo, 0, len(linhas))
	for _, l := range linhas {
		lista = append(lista, LinhaDominioAtivo{Valor: l.Dominio, OrganizationUUID: l.OrganizationUUID})
	}
	return lista, nil
}

func (r *repositorioApiKeysImpl) CriarApiKey(ctx context.Context, k *orgmodel.ApiKey) error {
	return traduzirErroDriver(
		orgctx.ScopeOrganization(r.db.WithContext(ctx), ctx).
			Create(k).Error, ErrChaveEmUso)
}

func (r *repositorioApiKeysImpl) ListarApiKeys(ctx context.Context, organizationUUID uuid.UUID, p pagination.Pagination) ([]orgmodel.ApiKey, int64, error) {
	var items []orgmodel.ApiKey
	var total int64
	q := orgctx.ScopeOrganization(r.db.WithContext(ctx), ctx).Model(&orgmodel.ApiKey{}).
		Where("organization_uuid = ?", organizationUUID)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("created_at DESC").
		Offset(p.Offset()).Limit(p.Limit()).
		Find(&items).Error
	return items, total, err
}

func (r *repositorioApiKeysImpl) BuscarApiKeyPorUUID(ctx context.Context, organizationUUID, chaveUUID uuid.UUID) (*orgmodel.ApiKey, error) {
	var k orgmodel.ApiKey
	err := orgctx.ScopeOrganization(r.db.WithContext(ctx), ctx).
		Where("uuid = ? AND organization_uuid = ?", chaveUUID, organizationUUID).
		First(&k).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrApiKeyNaoEncontrada
	}
	if err != nil {
		return nil, err
	}
	return &k, nil
}

// BuscarApiKeyPorHash é a EXCEÇÃO global documentada acima (resolvedor do
// middleware): filtra hash + vitalidade DA CHAVE E DA DONA — R4: chave de
// organization inativa/removida falha fechada no JOIN, defesa em
// profundidade além da revogação em cascata. Nunca exposto em rota de
// administração.
func (r *repositorioApiKeysImpl) BuscarApiKeyPorHash(ctx context.Context, hash string) (*orgmodel.ApiKey, error) {
	var k orgmodel.ApiKey
	err := r.db.WithContext(ctx).
		Model(&orgmodel.ApiKey{}).
		Joins("JOIN identidade_organization_organization dona ON dona.uuid = identidade_organization_apikey.organization_uuid").
		Where("identidade_organization_apikey.key_hash = ? AND identidade_organization_apikey.status = ?", hash, orgmodel.StatusAtivo).
		Where("dona.status = ? AND dona.deleted_at IS NULL", orgmodel.StatusAtivo).
		First(&k).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrApiKeyNaoEncontrada
	}
	if err != nil {
		return nil, err
	}
	return &k, nil
}

// RevogarApiKeysDaOrganization executa o lado da RAIZ na cascata de
// inativação/remoção (R4): remoção lógica de todas as chaves da organization
// escopada no ctx — BuscarApiKeyPorHash para de vê-las na hora.
func (r *repositorioApiKeysImpl) RevogarApiKeysDaOrganization(ctx context.Context, organizationUUID uuid.UUID) (int64, error) {
	res := orgctx.ScopeOrganization(r.db.WithContext(ctx), ctx).
		Where("organization_uuid = ?", organizationUUID).
		Delete(&orgmodel.ApiKey{})
	return res.RowsAffected, res.Error
}

func (r *repositorioApiKeysImpl) RemoverApiKey(ctx context.Context, organizationUUID, chaveUUID uuid.UUID) error {
	res := orgctx.ScopeOrganization(r.db.WithContext(ctx), ctx).
		Where("uuid = ? AND organization_uuid = ?", chaveUUID, organizationUUID).
		Delete(&orgmodel.ApiKey{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrApiKeyNaoEncontrada
	}
	return nil
}

// traduzirErroDriver: conflito de unicidade do Postgres (SQLSTATE 23505) vira
// a sentinela 409 do índice violado — dominio (raiz) ou key_hash (apikey).
func traduzirErroDriver(err error, conflito error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return conflito
	}
	return err
}

// ListarOpcoes devolve uuid+nome das organizations (issue #30). Ponteiro nil
// = TODAS (caminho da plataforma); preenchido = só a pedida. Find com Model()
// herda o soft delete do gorm — removida não aparece como opção.
func (r *repositoryImpl) ListarOpcoes(ctx context.Context, organizacaoUUID *uuid.UUID) ([]orgmodel.Organization, error) {
	q := r.db.WithContext(ctx).
		Model(&orgmodel.Organization{}).
		Select("uuid", "nome")
	if organizacaoUUID != nil {
		q = q.Where("uuid = ?", *organizacaoUUID)
	}
	var itens []orgmodel.Organization
	err := q.Order("nome ASC").Find(&itens).Error
	return itens, err
}
