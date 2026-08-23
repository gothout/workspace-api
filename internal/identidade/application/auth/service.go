package auth

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	modeluser "workspace-api/internal/identidade/model/user"
	"workspace-api/internal/infra/jwt"
	"workspace-api/internal/pkg/orgctx"
)

// Service orquestra user × resolução da organization pelo Host — caso de uso
// que cruza dois subdomínios, por isso aplicação e não regra do user. NÃO
// persiste nada próprio: tokens são tabela do subdomínio user, acessados
// pelo contrato Usuarios.
type Service interface {
	// Login autentica e devolve o PAR de tokens. As três falhas — host sem
	// organization, usuário inexistente, senha errada (e conta inativa) —
	// são INDISTINGUÍVEIS: mesmo corpo e, quando o host não resolve, o MESMO
	// caminho de comparação de hash (organization aleatória não encontra
	// ninguém e o service do user queima o bcrypt contra hash de mentira).
	Login(ctx context.Context, host string, in LoginEntrada) (*SessaoResponseDto, error)
	// Refresh troca um refresh token válido por um par novo.
	Refresh(ctx context.Context, refreshToken string) (*SessaoResponseDto, error)
	// Logout revoga o refresh token no Postgres (revogação persistida).
	Logout(ctx context.Context, refreshToken string) error
}

type LoginEntrada struct {
	Email string
	Senha string
}

type Dependencias struct {
	Usuarios     Usuarios
	Emissor      EmissorToken
	Organizacoes ResolvedorOrganization
	Vitalidade   VitalidadeOrganization
}

type serviceImpl struct {
	deps Dependencias
}

func NewService(deps Dependencias) Service { return &serviceImpl{deps: deps} }

func (s *serviceImpl) Login(ctx context.Context, host string, in LoginEntrada) (*SessaoResponseDto, error) {
	org, resolvido, err := s.deps.Organizacoes.Resolver(ctx, host)
	if err != nil {
		return nil, err // falha de infraestrutura sobe → 500; nunca vira 401 de mentira
	}
	if !resolvido {
		// Organization aleatória: o caminho é IDÊNTICO ao de credencial errada —
		// a consulta escopada não encontra ninguém e o subdomínio user roda a
		// comparação contra hash de mentira (corpo e tempo indistinguíveis).
		org = uuid.New()
	}
	ctxOrg := orgctx.WithOrganization(ctx, org)
	u, err := s.deps.Usuarios.Autenticar(ctxOrg, in.Email, in.Senha)
	if err != nil {
		s.auditar(ctx, "login", false, "email", in.Email)
		return nil, ErrCredenciaisInvalidas // o motivo exato fica no subdomínio/log
	}
	sessao, err := s.abrirSessao(ctxOrg, u)
	if err != nil {
		return nil, err
	}
	s.auditar(ctx, "login", true,
		"user_uuid", u.UUID.String(), "organization_uuid", u.OrganizationUUID.String())
	return sessao, nil
}

func (s *serviceImpl) Refresh(ctx context.Context, refreshToken string) (*SessaoResponseDto, error) {
	u, ctxOrg, jti, err := s.sessaoDoToken(ctx, refreshToken)
	if err != nil {
		return nil, err
	}
	sessao, err := s.abrirSessao(ctxOrg, u)
	if err != nil {
		return nil, err
	}
	s.auditar(ctx, "refresh", true,
		"user_uuid", u.UUID.String(),
		"organization_uuid", u.OrganizationUUID.String(),
		"jti_anterior", jti)
	return sessao, nil
}

func (s *serviceImpl) Logout(ctx context.Context, refreshToken string) error {
	u, ctxOrg, jti, err := s.sessaoDoToken(ctx, refreshToken)
	if err != nil {
		return err
	}
	if err := s.deps.Usuarios.EncerrarSessao(ctxOrg, u.UUID, jti); err != nil {
		return ErrSessaoInvalida
	}
	s.auditar(ctx, "logout", true,
		"user_uuid", u.UUID.String(),
		"organization_uuid", u.OrganizationUUID.String(),
		"jti", jti)
	return nil
}

// --- Internos -------------------------------------------------------------------

// abrirSessao emite o PAR de tokens com os dados ATUAIS do usuário e persiste
// a linha do refresh (jti único). O refresh anterior continua válido até
// expirar ou logout — rotação de refresh é evolução futura documentada.
func (s *serviceImpl) abrirSessao(ctx context.Context, u *modeluser.User) (*SessaoResponseDto, error) {
	acesso, refresh, jti, expiraRefresh, err := s.deps.Emissor.EmitirPar(jwt.EntradaToken{
		UserUUID:         u.UUID,
		OrganizationUUID: u.OrganizationUUID,
		Nome:             u.Nome,
		Email:            u.Email.String(),
	})
	if err != nil {
		return nil, err
	}
	if err := s.deps.Usuarios.RegistrarRefreshToken(ctx, u.UUID, jti, expiraRefresh); err != nil {
		return nil, err
	}
	return &SessaoResponseDto{
		AccessToken:  acesso,
		RefreshToken: refresh,
		TokenType:    "Bearer",
		Usuario: UsuarioResumo{
			UUID:  u.UUID,
			Nome:  u.Nome,
			Email: u.Email.String(),
		},
	}, nil
}

// sessaoDoToken valida assinatura/tipo, recarrega o usuário com escopo da
// organization ASSINADA no token e confirma que a linha do jti segue ativa.
// Qualquer falha vira ErrSessaoInvalida — detalhe de por que o token falhou
// é informação para atacante.
func (s *serviceImpl) sessaoDoToken(ctx context.Context, refreshToken string) (*modeluser.User, context.Context, string, error) {
	claims, err := s.deps.Emissor.Validar(refreshToken)
	if err != nil || claims.Tipo != jwt.ClaimTipoRefresh {
		return nil, ctx, "", ErrSessaoInvalida
	}
	usuarioUUID, err := uuid.Parse(claims.UserUUID)
	if err != nil || usuarioUUID == uuid.Nil {
		return nil, ctx, "", ErrSessaoInvalida
	}
	organizationUUID, err := uuid.Parse(claims.OrganizationUUID)
	if err != nil || organizationUUID == uuid.Nil {
		return nil, ctx, "", ErrSessaoInvalida
	}
	ctxOrg := orgctx.WithOrganization(ctx, organizationUUID)

	ativa, err := s.deps.Usuarios.RefreshTokenAtivo(ctxOrg, usuarioUUID, claims.JTI)
	if err != nil || !ativa {
		return nil, ctx, "", ErrSessaoInvalida
	}
	u, err := s.deps.Usuarios.PorUUID(ctxOrg, usuarioUUID)
	if err != nil || !u.Autenticavel() {
		// Conta inativa/removida encerra a sessão — estado da conta não é
		// revelado além da recusa genérica.
		return nil, ctx, "", ErrSessaoInvalida
	}
	if s.deps.Vitalidade == nil {
		// Peça faltando é boot quebrado: fail-closed (mesma regra da cadeia
		// de middleware) — nunca sessão aberta sem saber se a dona vive.
		return nil, ctx, "", ErrSessaoInvalida
	}
	viva, err := s.deps.Vitalidade.Ativa(ctxOrg, organizationUUID)
	if err != nil || !viva {
		// R4: organization inativa/removida encerra a sessão INDEPENDENTE do
		// jti estar ativo — mesma recusa genérica, estado da dona não vaza.
		return nil, ctx, "", ErrSessaoInvalida
	}
	return u, ctxOrg, claims.JTI, nil
}

// auditar registra login/logout/refresh (doc 03): sucesso E falha, payload
// montado à mão com vocabulário fechado — nunca texto livre.
func (s *serviceImpl) auditar(ctx context.Context, acao string, success bool, extras ...any) {
	args := []any{
		"dominio", Dominio, "subdominio", Subdominio, "acao", acao,
		"ray_trace", orgctx.RayTrace(ctx),
		"success", success,
	}
	args = append(args, extras...)
	if !success && acao == "login" {
		// Falha de login é evento de segurança: nível WARN para destacar.
		slog.WarnContext(ctx, "auth."+acao, args...)
		return
	}
	slog.InfoContext(ctx, "auth."+acao, args...)
}
