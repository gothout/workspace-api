package auth

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	modeluser "workspace-api/internal/identidade/model/user"
	"workspace-api/internal/infra/jwt"
	"workspace-api/internal/pkg/orgctx"
)

// --- Dublês dos TRÊS contratos (aplicação testável sem banco/singleton) ------

type usuariosFake struct {
	mu                 sync.Mutex
	porEmail           map[string]*modeluser.User
	tokens             map[string]*modeluser.RefreshToken
	chamadasAut        int // quantas vezes Autenticar rodou — prova o caminho idêntico quando o host não resolve
	orgUltimaTentativa uuid.UUID
	senhaCerta         string
}

func novoUsuariosFake() *usuariosFake {
	return &usuariosFake{
		porEmail:   map[string]*modeluser.User{},
		tokens:     map[string]*modeluser.RefreshToken{},
		senhaCerta: "senha-segura-123",
	}
}

func (u *usuariosFake) semear(org uuid.UUID, email string) *modeluser.User {
	u.mu.Lock()
	defer u.mu.Unlock()
	usuario, err := modeluser.NewUser(modeluser.CreateInput{
		OrganizationUUID: org, Nome: "Ana Lima", Email: email,
		SenhaHash: "$fake$",
	})
	if err != nil {
		panic(err)
	}
	u.porEmail[email] = usuario
	return usuario
}

func (u *usuariosFake) Autenticar(ctx context.Context, email, senha string) (*modeluser.User, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.chamadasAut++
	u.orgUltimaTentativa = orgctx.OrganizationUUID(ctx)
	usuario, ok := u.porEmail[email]
	// Emula o fail-closed do escopo: organization divergente NÃO encontra o
	// usuário — é exatamente por aqui que o host não resolvido falha.
	if !ok || usuario.OrganizationUUID != u.orgUltimaTentativa || senha != u.senhaCerta {
		return nil, errCredenciaisFake
	}
	return usuario, nil
}

func (u *usuariosFake) PorUUID(_ context.Context, id uuid.UUID) (*modeluser.User, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	for _, usuario := range u.porEmail {
		if usuario.UUID == id {
			copia := *usuario
			return &copia, nil
		}
	}
	return nil, errNaoEncontradoFake
}

func (u *usuariosFake) RegistrarRefreshToken(_ context.Context, usuarioUUID uuid.UUID, jti string, expiraEm time.Time) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.tokens[jti] = &modeluser.RefreshToken{UUID: uuid.New(), UserUUID: usuarioUUID, JTI: jti, ExpiraEm: expiraEm}
	return nil
}

func (u *usuariosFake) RefreshTokenAtivo(_ context.Context, usuarioUUID uuid.UUID, jti string) (bool, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	t, ok := u.tokens[jti]
	return ok && t.UserUUID == usuarioUUID && t.RevogadoEm == nil && time.Now().UTC().Before(t.ExpiraEm), nil
}

func (u *usuariosFake) EncerrarSessao(_ context.Context, _ uuid.UUID, jti string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	t, ok := u.tokens[jti]
	if !ok {
		return errSessaoInvalidaFake
	}
	momento := time.Now().UTC()
	t.RevogadoEm = &momento
	return nil
}

// revogarTudo emula a cascata da organization (R4): o EncerradorSessoesUsuarios
// revoga TODOS os tokens ativos da dona no momento da inativação — fora do
// caminho do auth, exatamente como no mundo real.
func (u *usuariosFake) revogarTudo() {
	u.mu.Lock()
	defer u.mu.Unlock()
	for _, t := range u.tokens {
		if t.RevogadoEm == nil {
			momento := time.Now().UTC()
			t.RevogadoEm = &momento
		}
	}
}

var (
	errCredenciaisFake    = assert.AnError // qualquer erro vira ErrCredenciaisInvalidas no service
	errSessaoInvalidaFake = assert.AnError
	errNaoEncontradoFake  = assert.AnError
)

type emissorFake struct {
	mu     sync.Mutex
	tokens map[string]jwt.Claims
	seq    int
}

func novoEmissorFake() *emissorFake { return &emissorFake{tokens: map[string]jwt.Claims{}} }

func (e *emissorFake) EmitirPar(in jwt.EntradaToken) (string, string, string, time.Time, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.seq++
	jti := "jti-" + string(rune('a'+e.seq))
	expira := time.Now().UTC().Add(time.Hour)
	refresh := "refresh-" + jti
	acesso := "access-" + jti
	e.tokens[refresh] = jwt.Claims{
		UserUUID: in.UserUUID.String(), OrganizationUUID: in.OrganizationUUID.String(),
		Tipo: jwt.ClaimTipoRefresh, JTI: jti,
	}
	e.tokens[acesso] = jwt.Claims{
		UserUUID: in.UserUUID.String(), OrganizationUUID: in.OrganizationUUID.String(),
		Tipo: jwt.ClaimTipoAccess, JTI: jti,
	}
	return acesso, refresh, jti, expira, nil
}

func (e *emissorFake) Validar(tokenTexto string) (*jwt.Claims, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if claims, ok := e.tokens[tokenTexto]; ok {
		return &claims, nil
	}
	return nil, errSessaoInvalidaFake
}

// ValidarSemRevogacao espelha a porta do logout: o dublê nunca consultou
// denylist em Validar, então as duas portas coincidem — mas o método existe
// para o contrato continuar honrado pelo dublê.
func (e *emissorFake) ValidarSemRevogacao(tokenTexto string) (*jwt.Claims, error) {
	return e.Validar(tokenTexto)
}

type organizacoesFake struct {
	org       uuid.UUID
	resolvido bool
	falha     error
}

func (o *organizacoesFake) Resolver(context.Context, string) (uuid.UUID, bool, error) {
	return o.org, o.resolvido, o.falha
}

// vitalidadeFake é o dublê do contrato R4: a dona da sessão vive ou não.
type vitalidadeFake struct {
	mu    sync.Mutex
	ativa map[uuid.UUID]bool
	falha error
}

func novaVitalidadeFake() *vitalidadeFake {
	return &vitalidadeFake{ativa: map[uuid.UUID]bool{}}
}

func (v *vitalidadeFake) Ativa(_ context.Context, organizationUUID uuid.UUID) (bool, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.falha != nil {
		return false, v.falha
	}
	viva, registrada := v.ativa[organizationUUID]
	return viva || !registrada, nil // não registrada = org viva (comportamento padrão)
}

func (v *vitalidadeFake) inativar(org uuid.UUID) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.ativa[org] = false
}

// --- Suíte -------------------------------------------------------------------

func montarApp(t *testing.T) (Service, *usuariosFake, *emissorFake, *organizacoesFake, *vitalidadeFake) {
	t.Helper()
	usuarios := novoUsuariosFake()
	emissor := novoEmissorFake()
	orgs := &organizacoesFake{org: uuid.New(), resolvido: true}
	vitalidade := novaVitalidadeFake()
	svc := NewService(Dependencias{
		Usuarios:   usuarios,
		Emissor:    emissor,
		Organizacoes: orgs,
		Vitalidade: vitalidade,
	})
	return svc, usuarios, emissor, orgs, vitalidade
}

const hostDeTeste = "filial-sul.exemplo.com"

func TestLoginBomEmiteOParEPersisteOJti(t *testing.T) {
	svc, usuarios, _, orgs, _ := montarApp(t)
	ana := usuarios.semear(orgs.org, "ana@exemplo.com")

	sessao, err := svc.Login(context.Background(), hostDeTeste, LoginEntrada{Email: "ana@exemplo.com", Senha: "senha-segura-123"})
	require.NoError(t, err)
	assert.Equal(t, "Bearer", sessao.TokenType)
	assert.NotEmpty(t, sessao.AccessToken)
	assert.NotEmpty(t, sessao.RefreshToken)
	assert.Equal(t, ana.UUID, sessao.Usuario.UUID)
	assert.Equal(t, "ana@exemplo.com", sessao.Usuario.Email)
	require.Len(t, usuarios.tokens, 1, "o jti do refresh é persistido")
}

// O coração do contrato: as três falhas são indistinguíveis — mesmo corpo
// (sentinela) e o MESMO caminho de comparação de hash.
func TestFalhasDeLoginSaoIndistingriveis(t *testing.T) {
	svc, usuarios, _, orgs, _ := montarApp(t)
	usuarios.semear(orgs.org, "ana@exemplo.com")

	// 1. senha errada; 2. usuário inexistente; 3. host sem organization.
	_, err1 := svc.Login(context.Background(), hostDeTeste, LoginEntrada{Email: "ana@exemplo.com", Senha: "errada-de-propósito"})
	_, err2 := svc.Login(context.Background(), hostDeTeste, LoginEntrada{Email: "fantasma@exemplo.com", Senha: "senha-segura-123"})
	orgs.resolvido = false
	_, err3 := svc.Login(context.Background(), "desconhecido.exemplo.com", LoginEntrada{Email: "ana@exemplo.com", Senha: "senha-segura-123"})

	assert.ErrorIs(t, err1, ErrCredenciaisInvalidas)
	assert.ErrorIs(t, err2, ErrCredenciaisInvalidas)
	assert.ErrorIs(t, err3, ErrCredenciaisInvalidas)
	assert.Equal(t, err1, err3, "corpos iguais: mesmo sentinela devolvido ao cliente")

	// Mesmo caminho: com host não resolvido a comparação TAMBÉM rodou — a
	// aplicação passou uma organization aleatória e o user queimou bcrypt.
	usuarios.mu.Lock()
	chamadas := usuarios.chamadasAut
	usuarios.mu.Unlock()
	assert.Equal(t, 3, chamadas, "host sem organization roda o MESMO caminho de autenticação")
}

func TestRefreshTrocaOParEExigeLinhaAtiva(t *testing.T) {
	svc, usuarios, emissor, orgs, _ := montarApp(t)
	ana := usuarios.semear(orgs.org, "ana@exemplo.com")

	primeira, err := svc.Login(context.Background(), hostDeTeste, LoginEntrada{Email: "ana@exemplo.com", Senha: "senha-segura-123"})
	require.NoError(t, err)

	segunda, err := svc.Refresh(context.Background(), primeira.RefreshToken)
	require.NoError(t, err)
	assert.Equal(t, ana.UUID, segunda.Usuario.UUID)
	require.Len(t, usuarios.tokens, 2, "novo jti é persistido a cada renovação")

	// Token revogado (logout anterior): recusa.
	require.NoError(t, svc.Logout(context.Background(), segunda.RefreshToken))
	_, err = svc.Refresh(context.Background(), segunda.RefreshToken)
	assert.ErrorIs(t, err, ErrSessaoInvalida, "jti revogado não renova sessão")

	// Access token não é refresh.
	var access string
	for token, claims := range emissor.tokens {
		if claims.Tipo == jwt.ClaimTipoAccess {
			access = token
			break
		}
	}
	_, err = svc.Refresh(context.Background(), access)
	assert.ErrorIs(t, err, ErrSessaoInvalida, "typ=access não passa no refresh")

	// Lixo: recusa genérica.
	_, err = svc.Refresh(context.Background(), "nao-e-token")
	assert.ErrorIs(t, err, ErrSessaoInvalida)
}

// R5 (issue #23): refresh com ROTAÇÃO — o jti anterior é revogado na
// renovação e o reuso dele falha fechado; o logout do token já rodado pela
// rotação também é sucesso (idempotente).
func TestRefreshRotacionaERevogaOJtiAnterior(t *testing.T) {
	svc, usuarios, emissor, orgs, _ := montarApp(t)
	usuarios.semear(orgs.org, "ana@exemplo.com")

	primeira, err := svc.Login(context.Background(), hostDeTeste, LoginEntrada{Email: "ana@exemplo.com", Senha: "senha-segura-123"})
	require.NoError(t, err)
	jtiAntigo, err := emissor.Validar(primeira.RefreshToken)
	require.NoError(t, err)

	segunda, err := svc.Refresh(context.Background(), primeira.RefreshToken)
	require.NoError(t, err)
	jtiNovo, err := emissor.Validar(segunda.RefreshToken)
	require.NoError(t, err)
	assert.NotEqual(t, jtiAntigo.JTI, jtiNovo.JTI, "rotação emite jti novo")

	usuarios.mu.Lock()
	revogado := usuarios.tokens[jtiAntigo.JTI].RevogadoEm != nil
	novoAtivo := usuarios.tokens[jtiNovo.JTI].RevogadoEm == nil
	usuarios.mu.Unlock()
	assert.True(t, revogado, "o jti anterior é revogado NO ARMAZÉM, não só rejeitado na porta")
	assert.True(t, novoAtivo, "o par novo nasce com a linha ativa")

	// Reuso do refresh antigo: falha fechado.
	_, err = svc.Refresh(context.Background(), primeira.RefreshToken)
	assert.ErrorIs(t, err, ErrSessaoInvalida, "refresh renovado não serve de novo")

	// Logout do token já rodado pela rotação: sucesso (idempotente).
	assert.NoError(t, svc.Logout(context.Background(), primeira.RefreshToken))

	// A sessão atual segue viva depois disso.
	terceira, err := svc.Refresh(context.Background(), segunda.RefreshToken)
	require.NoError(t, err)
	assert.NotEmpty(t, terceira.AccessToken)
}

// R5 (issue #23): logout IDEMPOTENTE de verdade — repetir com o mesmo token
// (já revogado pelo próprio logout) é sucesso; só entrada inválida recusa.
func TestLogoutRepetidoNaoFalha(t *testing.T) {
	svc, usuarios, _, orgs, _ := montarApp(t)
	usuarios.semear(orgs.org, "ana@exemplo.com")

	sessao, err := svc.Login(context.Background(), hostDeTeste, LoginEntrada{Email: "ana@exemplo.com", Senha: "senha-segura-123"})
	require.NoError(t, err)

	require.NoError(t, svc.Logout(context.Background(), sessao.RefreshToken))
	assert.NoError(t, svc.Logout(context.Background(), sessao.RefreshToken),
		"segundo logout do mesmo token: caminho idempotente alcançável")
	assert.NoError(t, svc.Logout(context.Background(), sessao.RefreshToken), "terceiro tanto faz")

	_, err = svc.Refresh(context.Background(), sessao.RefreshToken)
	assert.ErrorIs(t, err, ErrSessaoInvalida)

	// Lixo/assimétrico continua recusando — idempotente não é porta aberta.
	assert.ErrorIs(t, svc.Logout(context.Background(), "nao-e-token"), ErrSessaoInvalida)
}

func TestLogoutRevogaPersistindoNoPostgres(t *testing.T) {
	svc, usuarios, _, orgs, _ := montarApp(t)
	usuarios.semear(orgs.org, "ana@exemplo.com")

	sessao, err := svc.Login(context.Background(), hostDeTeste, LoginEntrada{Email: "ana@exemplo.com", Senha: "senha-segura-123"})
	require.NoError(t, err)

	require.NoError(t, svc.Logout(context.Background(), sessao.RefreshToken))

	usuarios.mu.Lock()
	for _, token := range usuarios.tokens {
		require.NotNil(t, token.RevogadoEm, "revogação marcada com revogado_em")
	}
	usuarios.mu.Unlock()

	_, err = svc.Refresh(context.Background(), sessao.RefreshToken)
	assert.ErrorIs(t, err, ErrSessaoInvalida, "sessão encerrada não volta")
}

func TestContaInativaEncerraASessaoSemVazarEstado(t *testing.T) {
	svc, usuarios, _, orgs, _ := montarApp(t)
	ana := usuarios.semear(orgs.org, "ana@exemplo.com")

	sessao, err := svc.Login(context.Background(), hostDeTeste, LoginEntrada{Email: "ana@exemplo.com", Senha: "senha-segura-123"})
	require.NoError(t, err)

	usuarios.mu.Lock()
	inativo := modeluser.StatusInativo
	usuarios.porEmail["ana@exemplo.com"].Status = inativo
	_ = ana
	usuarios.mu.Unlock()

	_, err = svc.Refresh(context.Background(), sessao.RefreshToken)
	assert.ErrorIs(t, err, ErrSessaoInvalida, "mesma recusa genérica — estado da conta não se revela")
}

// R4 (issue #22) + R5 (issue #23): a sessão não sobrevive à organization
// inativa — o refresh falha FECHADO com a dona morta; o LOGOUT, operação de
// destruição, não é bloqueado por ela: confirma a revogação e responde
// sucesso (idempotente).
func TestSessaoNaoSobreviveAOrganizationInativa(t *testing.T) {
	svc, usuarios, _, orgs, vitalidade := montarApp(t)
	usuarios.semear(orgs.org, "ana@exemplo.com")

	sessao, err := svc.Login(context.Background(), hostDeTeste, LoginEntrada{Email: "ana@exemplo.com", Senha: "senha-segura-123"})
	require.NoError(t, err)

	vitalidade.inativar(orgs.org)
	_, err = svc.Refresh(context.Background(), sessao.RefreshToken)
	assert.ErrorIs(t, err, ErrSessaoInvalida, "refresh com dona inativa falha fechado")

	// No mundo real a cascata (EncerradorSessoesUsuarios) revogou as linhas
	// NO MOMENTO da inativação — emulada aqui no dublê.
	usuarios.revogarTudo()

	// R5: logout do token já revogado pela cascata NÃO falha — destruição
	// best-effort; dona morta não tem nada a proteger aqui.
	assert.NoError(t, svc.Logout(context.Background(), sessao.RefreshToken))

	// Reativação da dona NÃO ressuscita o jti: revogação da cascata é
	// permanente; a vitalidade segue como defesa em profundidade no refresh.
	vitalidade.ativa[orgs.org] = true
	_, err = svc.Refresh(context.Background(), sessao.RefreshToken)
	assert.ErrorIs(t, err, ErrSessaoInvalida)
}

// Falha de infraestrutura na vitalidade também recusa a sessão — nunca
// renova com a saúde da dona desconhecida.
func TestFalhaNaVitalidadeRecusaASessao(t *testing.T) {
	svc, usuarios, _, orgs, vitalidade := montarApp(t)
	usuarios.semear(orgs.org, "ana@exemplo.com")
	sessao, err := svc.Login(context.Background(), hostDeTeste, LoginEntrada{Email: "ana@exemplo.com", Senha: "senha-segura-123"})
	require.NoError(t, err)

	vitalidade.falha = assert.AnError
	_, err = svc.Refresh(context.Background(), sessao.RefreshToken)
	assert.ErrorIs(t, err, ErrSessaoInvalida)
}

// Dependência ausente na montagem = fail-closed (mesma regra da cadeia de
// middleware) — sem saber se a dona vive, ninguém renova sessão.
func TestSemContratoDeVitalidadeNinguemRenovaSessao(t *testing.T) {
	usuarios := novoUsuariosFake()
	orgs := &organizacoesFake{org: uuid.New(), resolvido: true}
	svc := NewService(Dependencias{
		Usuarios:     usuarios,
		Emissor:      novoEmissorFake(),
		Organizacoes: orgs,
	})
	usuarios.semear(orgs.org, "ana@exemplo.com")
	sessao, err := svc.Login(context.Background(), hostDeTeste, LoginEntrada{Email: "ana@exemplo.com", Senha: "senha-segura-123"})
	require.NoError(t, err)

	_, err = svc.Refresh(context.Background(), sessao.RefreshToken)
	assert.ErrorIs(t, err, ErrSessaoInvalida)
}

// --- Lockout de login (evolução Redis, issue #8) -------------------------------

// limitadorFake conta as interações do service com o contrato LimitadorLogin.
type limitadorFake struct {
	mu        sync.Mutex
	bloqueado bool
	espera    time.Duration
	falhas    int
	sucessos  int
	consultas int
	errAutorizado error
}

func (l *limitadorFake) Autorizado(_ context.Context, _, _ string) (bool, time.Duration, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.consultas++
	if l.errAutorizado != nil {
		return false, 0, l.errAutorizado
	}
	return l.bloqueado, l.espera, nil
}

func (l *limitadorFake) RegistrarFalha(context.Context, string, string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.falhas++
	return nil
}

func (l *limitadorFake) RegistrarSucesso(context.Context, string, string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sucessos++
	return nil
}

func montarAppComLimite(t *testing.T, limite LimitadorLogin) (Service, *usuariosFake, *organizacoesFake) {
	t.Helper()
	usuarios := novoUsuariosFake()
	orgs := &organizacoesFake{org: uuid.New(), resolvido: true}
	svc := NewService(Dependencias{
		Usuarios:   usuarios,
		Emissor:    novoEmissorFake(),
		Organizacoes: orgs,
		Vitalidade: novaVitalidadeFake(),
		Limite:     limite,
	})
	return svc, usuarios, orgs
}

// Par bloqueado = 429 ANTES de qualquer verificação — nem o caminho de
// autenticação roda (não há comparação de bcrypt para um par preso).
func TestLoginBloqueadoNaoChegaAutenticar(t *testing.T) {
	limite := &limitadorFake{bloqueado: true, espera: 90 * time.Second}
	svc, usuarios, orgs := montarAppComLimite(t, limite)
	usuarios.semear(orgs.org, "ana@exemplo.com")

	_, err := svc.Login(context.Background(), hostDeTeste, LoginEntrada{
		Email: "ana@exemplo.com", Senha: "senha-segura-123", IP: "10.0.0.1",
	})
	require.ErrorIs(t, err, ErrLoginBloqueado)
	require.Equal(t, 1, limite.consultas)
	usuarios.mu.Lock()
	defer usuarios.mu.Unlock()
	require.Zero(t, usuarios.chamadasAut, "par preso NÃO passa pelo bcrypt")
	require.Zero(t, limite.falhas, "bloqueio não é falha de credencial: não alimenta o próprio contador")
}

// Falha de credencial alimenta o lockout; login bom limpa o histórico.
func TestLoginRegistraFalhaESucessoNoLimitador(t *testing.T) {
	limite := &limitadorFake{}
	svc, usuarios, orgs := montarAppComLimite(t, limite)
	usuarios.semear(orgs.org, "ana@exemplo.com")

	_, err := svc.Login(context.Background(), hostDeTeste, LoginEntrada{
		Email: "ana@exemplo.com", Senha: "errada-de-propósito", IP: "10.0.0.2",
	})
	require.ErrorIs(t, err, ErrCredenciaisInvalidas)
	require.Equal(t, 1, limite.falhas)

	_, err = svc.Login(context.Background(), hostDeTeste, LoginEntrada{
		Email: "ana@exemplo.com", Senha: "senha-segura-123", IP: "10.0.0.2",
	})
	require.NoError(t, err)
	require.Equal(t, 1, limite.sucessos, "login bom zera o histórico do par")
	require.Equal(t, 1, limite.falhas)
}

// Falha do PRÓPRIO limitador nunca impede login: degradação é seguir sem
// lockout (cache fora do ar não vira indisponibilidade de autenticação).
func TestLoginSegueSemLockoutQuandoLimitadorFalha(t *testing.T) {
	limite := &limitadorFake{errAutorizado: errors.New("redis fora")}
	svc, usuarios, orgs := montarAppComLimite(t, limite)
	usuarios.semear(orgs.org, "ana@exemplo.com")

	sessao, err := svc.Login(context.Background(), hostDeTeste, LoginEntrada{
		Email: "ana@exemplo.com", Senha: "senha-segura-123", IP: "10.0.0.3",
	})
	require.NoError(t, err, "falha do limitador não pode recusar login")
	require.NotEmpty(t, sessao.AccessToken)
}

// Sem limitador nas dependências (Redis ausente): fluxo idêntico ao
// pré-evolução — nil é operação normal.
func TestLoginSemLimitadorFuncionaComoAntes(t *testing.T) {
	svc, usuarios, _, orgs, _ := montarApp(t)
	usuarios.semear(orgs.org, "ana@exemplo.com")

	sessao, err := svc.Login(context.Background(), hostDeTeste, LoginEntrada{
		Email: "ana@exemplo.com", Senha: "senha-segura-123",
	})
	require.NoError(t, err)
	require.NotEmpty(t, sessao.RefreshToken)
}
