package user

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	modeluser "workspace-api/internal/identidade/model/user"
	"workspace-api/internal/pkg/orgctx"
)

// Nomes canônicos dos papéis de suporte (tabela identidade_user_papel.nome,
// seed da F1) — os caminhos de suporte perguntam por eles.
const (
	papelSuperAdmin        = "super_admin"
	papelAdminOrganization = "admin_organization"
)

// Credenciais é a fronteira da CRIPTOGRAFIA dentro do subdomínio: gerar hash
// na escrita e comparar na autenticação. A implementação real usa bcrypt
// (singleton); testes injetam dublês — a senha crua nunca atravessa para
// fora deste pacote.
type Credenciais interface {
	Gerar(senha string) (string, error)
	Comparar(hash, senha string) bool
}

// Service é o domain service do agregado: TODA a regra que não cabe num
// método da entidade vive aqui — política e geração de credencial, login
// INDISTINGUÍVEL, revogação de sessões, atribuição de papéis por workspace
// com validação do vínculo, resolução de permissões para o middleware e
// auditoria de toda escrita.
type Service interface {
	Create(ctx context.Context, in EntradaCriacao) (*modeluser.User, error)
	Read(ctx context.Context, id uuid.UUID) (*modeluser.User, error)
	List(ctx context.Context, f modeluser.ListFilter) ([]modeluser.User, int64, error)
	Update(ctx context.Context, id uuid.UUID, in modeluser.UpdateInput) (*modeluser.User, error)
	Delete(ctx context.Context, id uuid.UUID) error

	// Autenticar busca o usuário POR E-MAIL dentro da organization escopada
	// no ctx e compara a senha. NÃO distingue "não existe" de "senha errada":
	// mesmo sentinela e MESMO TRABALHO — quando o usuário não existe a
	// comparação roda contra um hash de mentira (não vaza existência nem por
	// timing).
	Autenticar(ctx context.Context, email string, senha string) (*modeluser.User, error)

	// Sessões persistidas (revogação por jti — doc 03). O ctx chega escopado
	// na organization dona da sessão (aplicação auth via adaptador).
	RegistrarSessao(ctx context.Context, usuarioUUID uuid.UUID, jti string, expiraEm time.Time) error
	SessaoAtiva(ctx context.Context, usuarioUUID uuid.UUID, jti string) (bool, error)
	EncerrarSessao(ctx context.Context, usuarioUUID uuid.UUID, jti string) error

	// Contrato ResolvedorPermissoes do middleware: vínculo direto OU suporte
	// auditado (super_admin em qualquer organization; admin_organization na
	// própria) e união das permissões efetivas. Organization vem do ctx — o
	// adaptador do bootstrap injeta antes de chamar.
	TemVinculo(ctx context.Context, usuarioUUID, workspaceUUID uuid.UUID) (bool, error)
	PermissoesEfetivas(ctx context.Context, usuarioUUID, workspaceUUID uuid.UUID) ([]string, error)

	// Atribuição de papéis POR workspace — permissão separada de editar:
	// dar poder a alguém não é "editar um campo".
	AtribuirPapel(ctx context.Context, usuarioUUID, workspaceUUID, papelUUID uuid.UUID) (*modeluser.AtribuicaoComPapel, error)
	Atribuicoes(ctx context.Context, usuarioUUID uuid.UUID) ([]modeluser.AtribuicaoComPapel, error)
	RemoverAtribuicao(ctx context.Context, usuarioUUID, atribuicaoUUID uuid.UUID) error
}

type serviceImpl struct {
	repo          Repository
	atribuicoes   RepositorioAtribuicoes
	validador     ValidadorWorkspaces // nil = sem validação de workspace alheio? NÃO: obrigatório no singleton; nil só em teste unitário explícito
	credenciais   Credenciais
	hashDeMentira sync.Once
	falsoHash     string
}

func NewService(repo Repository, atribuicoes RepositorioAtribuicoes, validador ValidadorWorkspaces, credenciais Credenciais) Service {
	return &serviceImpl{repo: repo, atribuicoes: atribuicoes, validador: validador, credenciais: credenciais}
}

// --- CRUD -----------------------------------------------------------------------

// Criar: input cru → escopo do ctx → política de senha → hash → entidade
// VÁLIDA pelo construtor → unicidade amigável → persistência → auditoria.
func (s *serviceImpl) Create(ctx context.Context, in EntradaCriacao) (*modeluser.User, error) {
	if err := modeluser.ValidarSenha(in.Senha); err != nil {
		return nil, err
	}
	hash, err := s.credenciais.Gerar(in.Senha)
	if err != nil {
		return nil, err
	}
	in.Dados.OrganizationUUID = orgctx.OrganizationUUID(ctx) // escopo vem do ctx, NUNCA do corpo
	in.Dados.SenhaHash = hash
	u, err := modeluser.NewUser(in.Dados)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Criar(ctx, u); err != nil {
		return nil, err
	}
	s.auditar(ctx, "criar", u.UUID, true, "email", u.Email.String())
	return u, nil
}

func (s *serviceImpl) Read(ctx context.Context, id uuid.UUID) (*modeluser.User, error) {
	return s.repo.BuscarPorUUID(ctx, id)
}

func (s *serviceImpl) List(ctx context.Context, f modeluser.ListFilter) ([]modeluser.User, int64, error) {
	return s.repo.Listar(ctx, f)
}

// Update traduz o input em chamadas aos métodos de comportamento. Inativar
// encerra as sessões abertas (revogação persistida) — sessão não sobrevive
// à conta.
func (s *serviceImpl) Update(ctx context.Context, id uuid.UUID, in modeluser.UpdateInput) (*modeluser.User, error) {
	u, err := s.repo.BuscarPorUUID(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.Nome != nil {
		if err := u.Renomear(*in.Nome); err != nil {
			return nil, err
		}
	}
	sessoesEncerradas := int64(0)
	if in.Status != nil && *in.Status == modeluser.StatusInativo {
		if err := u.Inativar(); err != nil {
			return nil, err
		}
		sessoesEncerradas, err = s.repo.RevogarTokensAtivosDoUsuario(ctx, u.UUID)
		if err != nil {
			return nil, err
		}
	}
	if in.Status != nil && *in.Status == modeluser.StatusAtivo {
		if err := u.Reativar(); err != nil {
			return nil, err
		}
	}
	if err := s.repo.Atualizar(ctx, u); err != nil {
		return nil, err
	}
	s.auditar(ctx, "editar", u.UUID, true,
		"status", string(u.Status), "sessoes_encerradas", sessoesEncerradas)
	return u, nil
}

// Remover é remoção LÓGICA e também encerra as sessões abertas.
func (s *serviceImpl) Delete(ctx context.Context, id uuid.UUID) error {
	u, err := s.repo.BuscarPorUUID(ctx, id)
	if err != nil {
		return err
	}
	sessoes, err := s.repo.RevogarTokensAtivosDoUsuario(ctx, u.UUID)
	if err != nil {
		return err
	}
	if err := s.repo.Remover(ctx, id); err != nil {
		return err
	}
	s.auditar(ctx, "remover", u.UUID, true, "email", u.Email.String(), "sessoes_encerradas", sessoes)
	return nil
}

// --- Autenticação -----------------------------------------------------------------

// Autenticar é a garantia de INDISTINGUIBILIDADE: usuário inexistente roda a
// MESMA comparação contra um hash de mentira (gerado uma única vez por
// processo) e devolve o MESMO sentinela de senha errada. Usuário inativo
// também vira credencial inválida — estado da conta não se revela.
func (s *serviceImpl) Autenticar(ctx context.Context, emailTexto, senha string) (*modeluser.User, error) {
	u, err := s.repo.BuscarPorEmail(ctx, modeluser.Email(emailTexto))
	if errors.Is(err, ErrNotFound) {
		s.compararComHashDeMentira(senha)
		return nil, ErrCredenciaisInvalidas
	}
	if err != nil {
		return nil, err
	}
	if !u.Autenticavel() || !s.credenciais.Comparar(u.SenhaHash, senha) {
		return nil, ErrCredenciaisInvalidas
	}
	return u, nil
}

// compararComHashDeMentira mantém o custo temporal das duas falhas igual:
// bcrypt roda nas duas rotas, só muda contra qual hash.
func (s *serviceImpl) compararComHashDeMentira(senha string) {
	s.hashDeMentira.Do(func() {
		hash, err := s.credenciais.Gerar("hash-de-mentira-para-timing-uniforme")
		if err != nil {
			// Sem hash falso a comparação vira no-op: aceitável só como
			// degradação extrema — loga e segue com o mesmo erro ao cliente.
			slog.Error("user: falha ao preparar hash de mentira", "causa", err.Error())
			return
		}
		s.falsoHash = hash
	})
	if s.falsoHash != "" {
		s.credenciais.Comparar(s.falsoHash, senha)
	}
}

// --- Sessões persistidas ------------------------------------------------------------

func (s *serviceImpl) RegistrarSessao(ctx context.Context, usuarioUUID uuid.UUID, jti string, expiraEm time.Time) error {
	token, err := modeluser.NewRefreshToken(modeluser.CreateRefreshTokenInput{
		OrganizationUUID: orgctx.OrganizationUUID(ctx),
		UserUUID:         usuarioUUID,
		JTI:              jti,
		ExpiraEm:         expiraEm,
	})
	if err != nil {
		return err
	}
	return s.repo.RegistrarRefreshToken(ctx, token)
}

// SessaoAtiva confere linha existente, do próprio usuário, não revogada e
// dentro da validade — segunda linha de defesa além da assinatura do JWT.
func (s *serviceImpl) SessaoAtiva(ctx context.Context, usuarioUUID uuid.UUID, jti string) (bool, error) {
	token, err := s.repo.BuscarRefreshToken(ctx, usuarioUUID, jti)
	if err != nil {
		return false, err
	}
	return token.Ativo(agoraUTC()), nil
}

// EncerrarSessão revoga marcando revogado_em. Token já revogado é sucesso
// (logout idempotente); linha ausente é recusa — não há sessão para fechar.
func (s *serviceImpl) EncerrarSessao(ctx context.Context, usuarioUUID uuid.UUID, jti string) error {
	token, err := s.repo.BuscarRefreshToken(ctx, usuarioUUID, jti)
	if err != nil {
		return err
	}
	if token.RevogadoEm != nil {
		return nil
	}
	token.Revogar(agoraUTC())
	if err := s.repo.RevogarRefreshToken(ctx, token); err != nil {
		return err
	}
	s.auditar(ctx, "encerrar_sessao", usuarioUUID, true, "jti", jti)
	return nil
}

// --- Autorização (contrato ResolvedorPermissoes) --------------------------------------

// TemVinculo responde atribuição direta OU concessão de suporte auditada:
// super_admin em qualquer organization, admin_organization na própria. As
// consultas de suporte rodam só nos caminhos de falha do vínculo direto.
func (s *serviceImpl) TemVinculo(ctx context.Context, usuarioUUID, workspaceUUID uuid.UUID) (bool, error) {
	direto, err := s.atribuicoes.TemAtribuicaoDireta(ctx, usuarioUUID, workspaceUUID)
	if err != nil {
		return false, err
	}
	if direto {
		return true, nil
	}
	superAdmin, err := s.atribuicoes.TemPapelEmQualquerOrganization(ctx, usuarioUUID, papelSuperAdmin)
	if err != nil {
		return false, err
	}
	if superAdmin {
		s.registrarSuporte(ctx, usuarioUUID, workspaceUUID, papelSuperAdmin)
		return true, nil
	}
	adminOrg, err := s.atribuicoes.TemPapelNaOrganization(ctx, usuarioUUID, papelAdminOrganization)
	if err != nil {
		return false, err
	}
	if adminOrg {
		s.registrarSuporte(ctx, usuarioUUID, workspaceUUID, papelAdminOrganization)
		return true, nil
	}
	return false, nil
}

func (s *serviceImpl) PermissoesEfetivas(ctx context.Context, usuarioUUID, workspaceUUID uuid.UUID) ([]string, error) {
	return s.atribuicoes.PermissoesEfetivas(ctx, usuarioUUID, workspaceUUID)
}

// registrarSuporte audita a concessão (quem entrou, onde, com qual papel) —
// payload montado à mão com vocabulário fechado (agents/03/04).
func (s *serviceImpl) registrarSuporte(ctx context.Context, usuarioUUID, workspaceUUID uuid.UUID, papel string) {
	slog.InfoContext(ctx, "[SUPORTE] acesso concedido sem atribuição direta",
		"dominio", modeluser.Dominio, "subdominio", modeluser.Subdominio, "acao", "suporte_concedido",
		"user_uuid", usuarioUUID.String(),
		"organization_uuid", orgctx.OrganizationUUID(ctx).String(),
		"workspace_uuid", workspaceUUID.String(),
		"papel", papel,
		"ray_trace", orgctx.RayTrace(ctx))
}

// --- Atribuição de papéis ---------------------------------------------------------

// AtribuirPapel valida o tripé antes de gravar: papel existe (global),
// workspace pertence à organization do ctx e está ativo (contrato ligado no
// bootstrap), e o usuário está no mesmo escopo.
func (s *serviceImpl) AtribuirPapel(ctx context.Context, usuarioUUID, workspaceUUID, papelUUID uuid.UUID) (*modeluser.AtribuicaoComPapel, error) {
	if _, err := s.repo.BuscarPorUUID(ctx, usuarioUUID); err != nil {
		return nil, err
	}
	papel, err := s.atribuicoes.PapelPorUUID(ctx, papelUUID)
	if err != nil {
		return nil, err
	}
	if s.validador == nil {
		return nil, ErrWorkspaceInvalido
	}
	pertence, err := s.validador.Pertence(ctx, orgctx.OrganizationUUID(ctx), workspaceUUID)
	if err != nil {
		return nil, err
	}
	if !pertence {
		return nil, ErrWorkspaceInvalido
	}
	a, err := modeluser.NovaAtribuicao(modeluser.CreateAtribuicaoInput{
		OrganizationUUID: orgctx.OrganizationUUID(ctx),
		WorkspaceUUID:    workspaceUUID,
		UserUUID:         usuarioUUID,
		PapelUUID:        papelUUID,
	})
	if err != nil {
		return nil, err
	}
	if err := s.atribuicoes.Criar(ctx, a); err != nil {
		return nil, err
	}
	s.auditar(ctx, "atribuir_papel", usuarioUUID, true,
		"atribuicao_uuid", a.UUID.String(),
		"workspace_uuid", workspaceUUID.String(),
		"papel_uuid", papelUUID.String())
	return &modeluser.AtribuicaoComPapel{Atribuicao: *a, PapelNome: papel.Nome}, nil
}

func (s *serviceImpl) Atribuicoes(ctx context.Context, usuarioUUID uuid.UUID) ([]modeluser.AtribuicaoComPapel, error) {
	if _, err := s.repo.BuscarPorUUID(ctx, usuarioUUID); err != nil {
		return nil, err
	}
	return s.atribuicoes.ListarPorUsuario(ctx, usuarioUUID)
}

func (s *serviceImpl) RemoverAtribuicao(ctx context.Context, usuarioUUID, atribuicaoUUID uuid.UUID) error {
	if _, err := s.repo.BuscarPorUUID(ctx, usuarioUUID); err != nil {
		return err
	}
	if err := s.atribuicoes.Remover(ctx, usuarioUUID, atribuicaoUUID); err != nil {
		return err
	}
	s.auditar(ctx, "remover_atribuicao", usuarioUUID, true, "atribuicao_uuid", atribuicaoUUID.String())
	return nil
}

// --- Internos -------------------------------------------------------------------

// NovasCredenciaisBcrypt devolve a implementação REAL da fronteira de
// criptografia (bcrypt, custo padrão) — usada pelo singleton do subdomínio e
// pelos testes de integração; a senha crua nunca atravessa para outra camada.
func NovasCredenciaisBcrypt() Credenciais { return credenciaisBcrypt{} }

type credenciaisBcrypt struct{}

func (credenciaisBcrypt) Gerar(senha string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(senha), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func (credenciaisBcrypt) Comparar(hash, senha string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(senha)) == nil
}

// auditar registra toda ESCRITA com payload montado à mão (doc 04):
// identificadores e vocabulário fechado, nunca texto livre.
func (s *serviceImpl) auditar(ctx context.Context, acao string, usuarioUUID uuid.UUID, success bool, extras ...any) {
	args := []any{
		"dominio", modeluser.Dominio, "subdominio", modeluser.Subdominio, "acao", acao,
		"user_uuid", usuarioUUID.String(),
		"organization_uuid", orgctx.OrganizationUUID(ctx).String(),
		"ray_trace", orgctx.RayTrace(ctx),
		"success", success,
	}
	args = append(args, extras...)
	slog.InfoContext(ctx, "user."+acao, args...)
}
