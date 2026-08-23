package user

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	modeluser "workspace-api/internal/identidade/model/user"
	"workspace-api/internal/pkg/orgctx"
)

// Repository é UM POR AGREGADO (raiz User): declara, em linguagem de negócio,
// o que o subdomínio precisa perguntar/gravar — nunca um "CRUD genérico".
// Toda query de administração passa por orgctx.ScopeOrganization — esta
// tabela está ACIMA do workspace; sem organization no ctx, o escopo FALHA
// (fail-closed).
//
// EXCEÇÕES de escopo documentadas (agents/03 e AGENTS.md do pacote):
//   - PapelPorUUID e os papéis em geral são GLOBAIS da plataforma (tabelas
//     sem coluna de escopo, seed da F1) — consulta por chave primária para
//     validar atribuições.
//   - RefreshTokenRevogado é GLOBAL pelo jti (único TOTAL): a validação do
//     JWT no middleware acontece antes de qualquer escopo; a consulta é por
//     chave única imutável e o resultado só fecha a porta (nunca abre dado).
type Repository interface {
	Criar(ctx context.Context, u *modeluser.User) error
	BuscarPorUUID(ctx context.Context, id uuid.UUID) (*modeluser.User, error)
	BuscarPorEmail(ctx context.Context, email modeluser.Email) (*modeluser.User, error)
	Listar(ctx context.Context, f modeluser.ListFilter) ([]modeluser.User, int64, error)
	Atualizar(ctx context.Context, u *modeluser.User) error
	Remover(ctx context.Context, id uuid.UUID) error

	RegistrarRefreshToken(ctx context.Context, t *modeluser.RefreshToken) error
	BuscarRefreshToken(ctx context.Context, usuarioUUID uuid.UUID, jti string) (*modeluser.RefreshToken, error)
	RevogarRefreshToken(ctx context.Context, t *modeluser.RefreshToken) error
	RevogarTokensAtivosDoUsuario(ctx context.Context, usuarioUUID uuid.UUID) (int64, error)
	RevogarTokensAtivosDaOrganization(ctx context.Context) (int64, error)
	RefreshTokenRevogado(jti string) (bool, error)
}

type repositoryImpl struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) Repository { return &repositoryImpl{db: db} }

func (r *repositoryImpl) Criar(ctx context.Context, u *modeluser.User) error {
	return traduzirErroUsuario(orgctx.ScopeOrganization(r.db.WithContext(ctx), ctx).Create(u).Error)
}

func (r *repositoryImpl) BuscarPorUUID(ctx context.Context, id uuid.UUID) (*modeluser.User, error) {
	var u modeluser.User
	err := orgctx.ScopeOrganization(r.db.WithContext(ctx), ctx).
		Where("uuid = ?", id).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// BuscarPorEmail é a porta do login: escopada pela organization resolvida
// (injetada no ctx pela aplicação auth via adaptador) — usuário de outra
// organization simplesmente não existe aqui.
func (r *repositoryImpl) BuscarPorEmail(ctx context.Context, email modeluser.Email) (*modeluser.User, error) {
	var u modeluser.User
	err := orgctx.ScopeOrganization(r.db.WithContext(ctx), ctx).
		Where("email = ?", email).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *repositoryImpl) Listar(ctx context.Context, f modeluser.ListFilter) ([]modeluser.User, int64, error) {
	var items []modeluser.User
	var total int64
	q := orgctx.ScopeOrganization(r.db.WithContext(ctx), ctx).Model(&modeluser.User{})
	if f.Nome != "" {
		q = q.Where("nome ILIKE ?", "%"+f.Nome+"%")
	}
	if f.Email != "" {
		q = q.Where("email ILIKE ?", "%"+f.Email+"%")
	}
	if f.Status != nil {
		q = q.Where("status = ?", *f.Status)
	}
	if f.WorkspaceUUID != nil {
		// Listagem POR workspace via atribuição — identidade_user_user não tem
		// workspace_uuid. O filtro manual `deleted_at IS NULL` é obrigatório:
		// subquery crua não herda o soft delete do gorm.
		q = q.Where(`EXISTS (
			SELECT 1 FROM identidade_user_atribuicao a
			WHERE a.user_uuid = identidade_user_user.uuid
			  AND a.workspace_uuid = ?
			  AND a.deleted_at IS NULL)`, *f.WorkspaceUUID)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("created_at DESC").
		Offset(f.Pagination.Offset()).Limit(f.Pagination.Limit()).
		Find(&items).Error
	return items, total, err
}

func (r *repositoryImpl) Atualizar(ctx context.Context, u *modeluser.User) error {
	return traduzirErroUsuario(orgctx.ScopeOrganization(r.db.WithContext(ctx), ctx).Save(u).Error)
}

// Remover confere RowsAffected: 0 linhas = registro inexistente ou fora do
// escopo — ambos viram ErrNotFound (não vaza existência).
func (r *repositoryImpl) Remover(ctx context.Context, id uuid.UUID) error {
	res := orgctx.ScopeOrganization(r.db.WithContext(ctx), ctx).
		Where("uuid = ?", id).Delete(&modeluser.User{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Refresh tokens (revogação persistida) --------------------------------------

func (r *repositoryImpl) RegistrarRefreshToken(ctx context.Context, t *modeluser.RefreshToken) error {
	return traduzirErroUsuario(orgctx.ScopeOrganization(r.db.WithContext(ctx), ctx).Create(t).Error)
}

// BuscarRefreshToken é escopada: linha de outra organization não existe aqui.
func (r *repositoryImpl) BuscarRefreshToken(ctx context.Context, usuarioUUID uuid.UUID, jti string) (*modeluser.RefreshToken, error) {
	var t modeluser.RefreshToken
	err := orgctx.ScopeOrganization(r.db.WithContext(ctx), ctx).
		Where("user_uuid = ? AND jti = ?", usuarioUUID, jti).First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrRefreshTokenInvalido
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// RevogarRefreshToken carimba revogado_em na linha JÁ carregada (o service
// decide o significado de ausente/revogada antes de chegar aqui).
func (r *repositoryImpl) RevogarRefreshToken(ctx context.Context, t *modeluser.RefreshToken) error {
	return orgctx.ScopeOrganization(r.db.WithContext(ctx), ctx).Save(t).Error
}

// RevogarTokensAtivosDoUsuario encerra TODAS as sessões abertas — chamado ao
// inativar/remover o usuário (sessão não sobrevive à conta).
func (r *repositoryImpl) RevogarTokensAtivosDoUsuario(ctx context.Context, usuarioUUID uuid.UUID) (int64, error) {
	res := orgctx.ScopeOrganization(r.db.WithContext(ctx), ctx).Model(&modeluser.RefreshToken{}).
		Where("user_uuid = ? AND revogado_em IS NULL", usuarioUUID).
		Update("revogado_em", agoraUTC())
	return res.RowsAffected, res.Error
}

// RevogarTokensAtivosDaOrganization encerra TODAS as sessões abertas dos
// usuários da organization ESCOPADA NO CTX — lado user da cascata de
// inativação/remoção da dona do contrato (R4; contrato
// EncerradorSessoesUsuarios ligado no cmd/bootstrap). Idempotente: sem token
// ativo devolve 0 sem erro.
func (r *repositoryImpl) RevogarTokensAtivosDaOrganization(ctx context.Context) (int64, error) {
	res := orgctx.ScopeOrganization(r.db.WithContext(ctx), ctx).Model(&modeluser.RefreshToken{}).
		Where("revogado_em IS NULL").
		Update("revogado_em", agoraUTC())
	return res.RowsAffected, res.Error
}

// RefreshTokenRevogado é a EXCEÇÃO GLOBAL documentada: o validador do JWT
// (middleware/infra) confere o jti ANTES de existir escopo; jti é único TOTAL
// e imutável, e o resultado apenas recusa tokens revogados — nenhum dado
// atravessa esta consulta.
func (r *repositoryImpl) RefreshTokenRevogado(jti string) (bool, error) {
	var t modeluser.RefreshToken
	err := r.db.Select("revogado_em").
		Where("jti = ?", jti).First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil // linha ausente = nunca emitido; a decisão é das demais validações
	}
	if err != nil {
		return false, err
	}
	return t.RevogadoEm != nil, nil
}

// traduzirErroUsuario: conflito de unicidade do Postgres (SQLSTATE 23505)
// vira a sentinela 409 — o único índice único parcial da tabela raiz é o
// e-mail por organization.
func traduzirErroUsuario(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrEmailEmUso
	}
	return err
}

func agoraUTC() time.Time { return time.Now().UTC() }

// --- RepositorioAtribuicoes (registro-filho operado pela raiz) -------------------

// RepositorioAtribuicoes atende as consultas do vínculo user × workspace ×
// papel e da autorização (contrato ResolvedorPermissoes do middleware).
// As tabelas de papéis são GLOBAIS da plataforma (exceção documentada no
// AGENTS.md do pacote e em agents/03); as de atribuição escopam por org.
type RepositorioAtribuicoes interface {
	Criar(ctx context.Context, a *modeluser.Atribuicao) error
	ListarPorUsuario(ctx context.Context, usuarioUUID uuid.UUID) ([]modeluser.AtribuicaoComPapel, error)
	Remover(ctx context.Context, usuarioUUID, atribuicaoUUID uuid.UUID) error

	TemAtribuicaoDireta(ctx context.Context, usuarioUUID, workspaceUUID uuid.UUID) (bool, error)
	TemPapelNaOrganization(ctx context.Context, usuarioUUID uuid.UUID, papelNome string) (bool, error)
	TemPapelEmQualquerOrganization(ctx context.Context, usuarioUUID uuid.UUID, papelNome string) (bool, error)
	PapelPorUUID(ctx context.Context, papelUUID uuid.UUID) (*modeluser.Papel, error)
	ListarPapeis(ctx context.Context) ([]modeluser.Papel, error)
	PermissoesEfetivas(ctx context.Context, usuarioUUID, workspaceUUID uuid.UUID) ([]string, error)
}

type repositorioAtribuicoesImpl struct{ db *gorm.DB }

func NewRepositorioAtribuicoes(db *gorm.DB) RepositorioAtribuicoes {
	return &repositorioAtribuicoesImpl{db: db}
}

// Criar traduz a violação do índice único parcial (mesmo papel para o mesmo
// par usuário×workspace) na sentinela 409.
func (r *repositorioAtribuicoesImpl) Criar(ctx context.Context, a *modeluser.Atribuicao) error {
	err := orgctx.ScopeOrganization(r.db.WithContext(ctx), ctx).Create(a).Error
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrAtribuicaoDuplicada
	}
	return err
}

// ListarPorUsuario projeta o vínculo com o NOME canônico do papel — join de
// leitura com tabela global, sem expor a entidade alheia inteira.
func (r *repositorioAtribuicoesImpl) ListarPorUsuario(ctx context.Context, usuarioUUID uuid.UUID) ([]modeluser.AtribuicaoComPapel, error) {
	var itens []modeluser.AtribuicaoComPapel
	err := orgctx.ScopeOrganization(r.db.WithContext(ctx), ctx).
		Table("identidade_user_atribuicao").
		Select("identidade_user_atribuicao.*, p.nome AS papel_nome").
		Joins("JOIN identidade_user_papel p ON p.uuid = identidade_user_atribuicao.papel_uuid").
		Where("identidade_user_atribuicao.user_uuid = ? AND identidade_user_atribuicao.deleted_at IS NULL", usuarioUUID).
		Order("identidade_user_atribuicao.created_at ASC").
		Scan(&itens).Error
	return itens, err
}

// Remover confere RowsAffected: 0 linhas = inexistente ou fora do escopo —
// ambos viram ErrAtribuicaoNaoEncontrada (não vaza existência).
func (r *repositorioAtribuicoesImpl) Remover(ctx context.Context, usuarioUUID, atribuicaoUUID uuid.UUID) error {
	res := orgctx.ScopeOrganization(r.db.WithContext(ctx), ctx).
		Where("uuid = ? AND user_uuid = ?", atribuicaoUUID, usuarioUUID).
		Delete(&modeluser.Atribuicao{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrAtribuicaoNaoEncontrada
	}
	return nil
}

func (r *repositorioAtribuicoesImpl) TemAtribuicaoDireta(ctx context.Context, usuarioUUID, workspaceUUID uuid.UUID) (bool, error) {
	var total int64
	err := orgctx.ScopeOrganization(r.db.WithContext(ctx), ctx).
		Model(&modeluser.Atribuicao{}).
		Where("user_uuid = ? AND workspace_uuid = ?", usuarioUUID, workspaceUUID).
		Limit(1).Count(&total).Error
	return total > 0, err
}

// TemPapelNaOrganization é o caminho de suporte do admin_organization: papel
// exercido em QUALQUER workspace da própria organization (escopo do ctx).
func (r *repositorioAtribuicoesImpl) TemPapelNaOrganization(ctx context.Context, usuarioUUID uuid.UUID, papelNome string) (bool, error) {
	var total int64
	err := orgctx.ScopeOrganization(r.db.WithContext(ctx), ctx).
		Table("identidade_user_atribuicao a").
		Joins("JOIN identidade_user_papel p ON p.uuid = a.papel_uuid").
		Where("a.user_uuid = ? AND p.nome = ? AND a.deleted_at IS NULL", usuarioUUID, papelNome).
		Limit(1).Count(&total).Error
	return total > 0, err
}

// TemPapelEmQualquerOrganization é o caminho de suporte do super_admin:
// consulta GLOBAL por natureza (o super_admin atravessa organizations) —
// roda só nos caminhos de falha do vínculo direto, nunca no quente.
func (r *repositorioAtribuicoesImpl) TemPapelEmQualquerOrganization(ctx context.Context, usuarioUUID uuid.UUID, papelNome string) (bool, error) {
	var total int64
	err := r.db.WithContext(ctx).
		Table("identidade_user_atribuicao a").
		Joins("JOIN identidade_user_papel p ON p.uuid = a.papel_uuid").
		Where("a.user_uuid = ? AND p.nome = ? AND a.deleted_at IS NULL", usuarioUUID, papelNome).
		Limit(1).Count(&total).Error
	return total > 0, err
}

// PapelPorUUID consulta a tabela GLOBAL de papéis por chave primária —
// exceção documentada: papéis seed da plataforma, sem coluna de escopo.
func (r *repositorioAtribuicoesImpl) PapelPorUUID(ctx context.Context, papelUUID uuid.UUID) (*modeluser.Papel, error) {
	var p modeluser.Papel
	err := r.db.WithContext(ctx).Where("uuid = ?", papelUUID).First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrPapelNaoEncontrado
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ListarPapeis devolve TODOS os papéis globais da plataforma (seed da F1) —
// MESMA EXCEÇÃO de escopo do PapelPorUUID: as tabelas de papéis não têm
// coluna de escopo (agents/03). Lista de referência para o painel montar o
// Select de atribuição; ordenada por nome para saída determinística.
func (r *repositorioAtribuicoesImpl) ListarPapeis(ctx context.Context) ([]modeluser.Papel, error) {
	var papeis []modeluser.Papel
	err := r.db.WithContext(ctx).
		Order("nome ASC").
		Find(&papeis).Error
	if err != nil {
		return nil, err
	}
	if papeis == nil {
		papeis = []modeluser.Papel{}
	}
	return papeis, nil
}

// PermissoesEfetivas devolve a UNIÃO das permissões dos papéis do usuário no
// workspace (atribuição direta) mais as dos papéis de suporte que ele exerce
// — curingas inclusos. Sem cache no núcleo (Redis é evolução futura).
func (r *repositorioAtribuicoesImpl) PermissoesEfetivas(ctx context.Context, usuarioUUID, workspaceUUID uuid.UUID) ([]string, error) {
	org := orgctx.OrganizationUUID(ctx)
	var permissoes []string
	err := r.db.WithContext(ctx).Raw(`
SELECT DISTINCT pp.permissao
FROM identidade_user_atribuicao a
JOIN identidade_user_papel_permissao pp ON pp.papel_uuid = a.papel_uuid
WHERE a.user_uuid = ? AND a.workspace_uuid = ? AND a.deleted_at IS NULL

UNION

SELECT DISTINCT pp.permissao
FROM identidade_user_papel p
JOIN identidade_user_papel_permissao pp ON pp.papel_uuid = p.uuid
WHERE p.nome = ?
  AND EXISTS (
    SELECT 1 FROM identidade_user_atribuicao sa
    WHERE sa.user_uuid = ? AND sa.papel_uuid = p.uuid AND sa.deleted_at IS NULL)

UNION

SELECT DISTINCT pp.permissao
FROM identidade_user_papel p
JOIN identidade_user_papel_permissao pp ON pp.papel_uuid = p.uuid
WHERE p.nome = ?
  AND EXISTS (
    SELECT 1 FROM identidade_user_atribuicao sa
    WHERE sa.user_uuid = ? AND sa.organization_uuid = ? AND sa.papel_uuid = p.uuid
      AND sa.deleted_at IS NULL)
`, usuarioUUID, workspaceUUID, papelSuperAdmin, usuarioUUID,
		papelAdminOrganization, usuarioUUID, org).Scan(&permissoes).Error
	if err != nil {
		return nil, err
	}
	if permissoes == nil {
		permissoes = []string{}
	}
	return permissoes, nil
}
