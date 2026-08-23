package auth

import (
	"context"
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

// R4 (issue #22): a sessão não sobrevive à organization inativa — refresh e
// logout falham FECHADO mesmo com jti ativo e conta ativa; a recusa é a
// genérica (estado da dona não vaza).
func TestSessaoNaoSobreviveAOrganizationInativa(t *testing.T) {
	svc, usuarios, _, orgs, vitalidade := montarApp(t)
	usuarios.semear(orgs.org, "ana@exemplo.com")

	sessao, err := svc.Login(context.Background(), hostDeTeste, LoginEntrada{Email: "ana@exemplo.com", Senha: "senha-segura-123"})
	require.NoError(t, err)

	vitalidade.inativar(orgs.org)
	_, err = svc.Refresh(context.Background(), sessao.RefreshToken)
	assert.ErrorIs(t, err, ErrSessaoInvalida, "refresh com dona inativa falha fechado")
	assert.ErrorIs(t, svc.Logout(context.Background(), sessao.RefreshToken), ErrSessaoInvalida,
		"logout pela mesma porta: dona morta não abre sessão")

	// No mundo real a cascata (EncerradorSessoesUsuarios) revogou as linhas
	// NO MOMENTO da inativação — o logout bloqueado pela vitalidade não é quem
	// revoga. Emulada aqui no dublê.
	usuarios.revogarTudo()

	// Reativação da dona NÃO ressuscita o jti: revogação da cascata é
	// permanente; a vitalidade segue como defesa em profundidade.
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
