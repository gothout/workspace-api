package auth

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	modeluser "workspace-api/internal/identidade/model/user"
	"workspace-api/internal/infra/jwt"
	"workspace-api/internal/pkg/log/audit_log"
	"workspace-api/internal/pkg/orgctx"
	"workspace-api/internal/pkg/pii"
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
	// Refresh troca um refresh token válido por um par novo COM ROTAÇÃO
	// (R5): o jti anterior é revogado antes da emissão — reuso de refresh
	// renovado falha fechado.
	Refresh(ctx context.Context, refreshToken string) (*SessaoResponseDto, error)
	// Logout revoga o refresh token no Postgres (revogação persistida).
	// IDEMPOTENTE: token já revogado é sucesso — repetir o logout nunca
	// falha; só assinatura/tipo/claims inválidos recusam.
	Logout(ctx context.Context, refreshToken string) error
}

type LoginEntrada struct {
	Email string
	Senha string
	IP    string // origem da tentativa (c.ClientIP() no controller) — chave do lockout
}

type Dependencias struct {
	Usuarios     Usuarios
	Emissor      EmissorToken
	Organizacoes ResolvedorOrganization
	Vitalidade   VitalidadeOrganization
	Limite       LimitadorLogin    // opcional: nil = sem lockout (Redis ausente é operação normal)
	Trilha       audit_log.Destino // opcional (#9): nil = slog legado nos auditar()
}

type serviceImpl struct {
	deps Dependencias
}

func NewService(deps Dependencias) Service { return &serviceImpl{deps: deps} }

func (s *serviceImpl) Login(ctx context.Context, host string, in LoginEntrada) (*SessaoResponseDto, error) {
	// Lockout distribuído (issue #8): par e-mail+IP preso = 429 ANTES de
	// qualquer verificação. Falha do limitador NUNCA impede login — segue
	// sem lockout, com log (degradação é a recusa certa aqui: cache fora do
	// ar não vira indisponibilidade de autenticação).
	if s.deps.Limite != nil {
		bloqueado, espera, err := s.deps.Limite.Autorizado(ctx, in.Email, in.IP)
		if err != nil {
			slog.WarnContext(ctx, "auth.login_lockout_indisponivel", "erro", err.Error())
		} else if bloqueado {
			s.auditar(ctx, "login", false,
				"email", pii.MascaraEmail(in.Email), "limite_excedido", true,
				"espera_seg", int(espera.Seconds()))
			return nil, ErrLoginBloqueado
		}
	}
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
		// E-mail é PII e, aqui, input NÃO validado do cliente: vai mascarado
		// (R7) — auditoria mantém o "quem" aproximado sem ecoar o valor.
		s.auditar(ctx, "login", false, "email", pii.MascaraEmail(in.Email))
		s.contarFalhaLogin(ctx, in)         // falha alimenta o lockout quando ligado
		return nil, ErrCredenciaisInvalidas // o motivo exato fica no subdomínio/log
	}
	sessao, err := s.abrirSessao(ctxOrg, u)
	if err != nil {
		return nil, err
	}
	s.limparFalhasLogin(ctx, in) // login bom zera o histórico do par
	s.auditar(ctx, "login", true,
		"user_uuid", u.UUID.String(), "organization_uuid", u.OrganizationUUID.String())
	return sessao, nil
}

func (s *serviceImpl) Refresh(ctx context.Context, refreshToken string) (*SessaoResponseDto, error) {
	u, ctxOrg, jtiAnterior, err := s.sessaoDoToken(ctx, refreshToken)
	if err != nil {
		return nil, err
	}
	// Rotação (R5) na direção fail-closed: revoga o jti anterior ANTES de
	// emitir o par novo — se a emissão falhar, a sessão morre (o dono
	// re-loga); nunca dois refresh válidos coexistem. EncerrarSessao é
	// idempotente no subdomínio e audita a revogação.
	if err := s.deps.Usuarios.EncerrarSessao(ctxOrg, u.UUID, jtiAnterior); err != nil {
		return nil, ErrSessaoInvalida
	}
	sessao, err := s.abrirSessao(ctxOrg, u)
	if err != nil {
		return nil, err
	}
	s.auditar(ctx, "refresh", true,
		"user_uuid", u.UUID.String(),
		"organization_uuid", u.OrganizationUUID.String(),
		"jti_anterior", jtiAnterior)
	return sessao, nil
}

func (s *serviceImpl) Logout(ctx context.Context, refreshToken string) error {
	usuarioUUID, organizationUUID, jti, err := s.claimsParaEncerrar(refreshToken)
	if err != nil {
		return err
	}
	ctxOrg := orgctx.WithOrganization(ctx, organizationUUID)
	// Logout NÃO exige jti ativo, conta autenticável nem dona viva: é
	// operação de DESTRUIÇÃO — nunca concede acesso, então nada nela precisa
	// de fail-closed além da assinatura. O token já revogado (logout
	// repetido, rotação do refresh ou cascata da organization) encontra a
	// linha marcada e EncerrarSessao devolve nil: idempotente de verdade
	// (R5) — antes deste ajuste esse caminho era inalcançável porque o
	// logout passava pela mesma porta do refresh.
	if err := s.deps.Usuarios.EncerrarSessao(ctxOrg, usuarioUUID, jti); err != nil {
		// Linha ausente/anomalia de infra: recusa honesta — nada foi revogado
		// e repetir não muda nada até a causa sumir.
		return ErrSessaoInvalida
	}
	s.auditar(ctx, "logout", true,
		"user_uuid", usuarioUUID.String(),
		"organization_uuid", organizationUUID.String(),
		"jti", jti)
	return nil
}

// --- Internos -------------------------------------------------------------------

// abrirSessao emite o PAR de tokens com os dados ATUAIS do usuário e persiste
// a linha do refresh (jti único). Chamado pelo login e pelo refresh — que
// revoga o jti anterior antes (rotação, R5); sozinho ele NUNCA mantém duas
// sessões vivas do mesmo usuário.
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

// claimsParaEncerrar é a porta do LOGOUT (R5): confere assinatura, tipo e
// parses das claims — e SÓ isso. Diferente de sessaoDoToken, não exige jti
// ativo nem consulta estado do usuário/dona: revogar o que já está revogado
// tem que continuar sendo sucesso. Token malformado/estranho segue recusa
// genérica (ErrSessaoInvalida).
func (s *serviceImpl) claimsParaEncerrar(refreshToken string) (usuarioUUID, organizationUUID uuid.UUID, jti string, err error) {
	// ValidarSemRevogacao: o token JÁ REVOGADO tem que passar aqui — é
	// exatamente o caso em que o logout repete. Expirado/assinatura ruim/
	// tipo errado seguem recusa genérica.
	claims, err := s.deps.Emissor.ValidarSemRevogacao(refreshToken)
	if err != nil || claims.Tipo != jwt.ClaimTipoRefresh || claims.JTI == "" {
		return uuid.Nil, uuid.Nil, "", ErrSessaoInvalida
	}
	usuarioUUID, err = uuid.Parse(claims.UserUUID)
	if err != nil || usuarioUUID == uuid.Nil {
		return uuid.Nil, uuid.Nil, "", ErrSessaoInvalida
	}
	organizationUUID, err = uuid.Parse(claims.OrganizationUUID)
	if err != nil || organizationUUID == uuid.Nil {
		return uuid.Nil, uuid.Nil, "", ErrSessaoInvalida
	}
	return usuarioUUID, organizationUUID, claims.JTI, nil
}

// contarFalhaLogin alimenta o lockout com a credencial recusada. Erro do
// limitador é só log: cache fora do ar não pode falhar a resposta de login
// (a recusa de credencial já aconteceu e é ela que o cliente recebe).
func (s *serviceImpl) contarFalhaLogin(ctx context.Context, in LoginEntrada) {
	if s.deps.Limite == nil {
		return
	}
	if err := s.deps.Limite.RegistrarFalha(ctx, in.Email, in.IP); err != nil {
		slog.WarnContext(ctx, "auth.login_lockout_indisponivel", "erro", err.Error())
	}
}

// limparFalhasLogin zera o histórico do par após login bem-sucedido — mesmo
// raciocínio do contarFalhaLogin para falhas do próprio limitador.
func (s *serviceImpl) limparFalhasLogin(ctx context.Context, in LoginEntrada) {
	if s.deps.Limite == nil {
		return
	}
	if err := s.deps.Limite.RegistrarSucesso(ctx, in.Email, in.IP); err != nil {
		slog.WarnContext(ctx, "auth.login_lockout_indisponivel", "erro", err.Error())
	}
}

// auditar registra login/logout/refresh (doc 03) na trilha assíncrona (#9):
// sucesso E falha, payload montado à mão com vocabulário fechado — nunca
// texto livre. Sem trilha ligada (montagem direta em teste), cai para o slog
// legado — mesmo payload, caminho síncrono.
func (s *serviceImpl) auditar(ctx context.Context, acao string, success bool, extras ...any) {
	evento := audit_log.Evento{
		Instante:   time.Now().UTC(),
		Dominio:    Dominio,
		Subdominio: Subdominio,
		Acao:       acao,
		Sucesso:    success,
		RayTrace:   orgctx.RayTrace(ctx),
		Detalhes:   audit_log.Detalhes(extras...),
	}
	if s.deps.Trilha != nil {
		s.deps.Trilha.Registrar(evento)
		return
	}
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
