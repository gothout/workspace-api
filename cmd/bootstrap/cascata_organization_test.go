// Cenário R4 (issue #22) ponta a ponta sobre o esquema migrado: inativar a
// organization encerra TODOS os acessos dela — sessões dos usuários
// (refresh tokens) e chaves de API — e o refresh falha FECHADO com a dona
// morta mesmo antes de qualquer nova tentativa de acesso; o LOGOUT segue
// idempotente (R5): operação de destruição não é bloqueada pela dona.
// Montagem por construtores PUROS sobre o banco efêmero (o singleton é POR
// PROCESSO: este pacote tem outro teste que boota os contêineres).
package bootstrap

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	aplicacaoauth "workspace-api/internal/identidade/application/auth"
	dominioOrganizacao "workspace-api/internal/identidade/domain/organization"
	dominioUsuario "workspace-api/internal/identidade/domain/user"
	orgmodel "workspace-api/internal/identidade/model/organization"
	modeluser "workspace-api/internal/identidade/model/user"
	"workspace-api/internal/infra/jwt"
	"workspace-api/internal/pkg/orgctx"
)

// --- Dublês de montagem (espelhos puros dos adaptadores do bootstrap) -------

// suspensorNulo representa o lado workspace da cascata nos testes sem o
// singleton dele — o caminho workspace REAL já tem cobertura própria.
type suspensorNulo struct{}

func (suspensorNulo) SuspenderPorOrganization(context.Context, uuid.UUID) (int, error) {
	return 0, nil
}

// encerradorLocal liga o contrato EncerradorSessoesUsuarios ao service PURO
// do user — mesma tradução do adaptador encerradorSessoesUsuario do boot.
type encerradorLocal struct{ svc dominioUsuario.Service }

func (e encerradorLocal) RevogarTokensDaOrganization(ctx context.Context) (int64, error) {
	return e.svc.RevogarSessoesDaOrganization(ctx)
}

// usuariosAuthLocal expõe ao auth a face mínima do service do user, com a
// MESMA tradução ACL do adaptador usuariosAuth do boot.
type usuariosAuthLocal struct{ svc dominioUsuario.Service }

func (a usuariosAuthLocal) Autenticar(ctx context.Context, email, senha string) (*modeluser.User, error) {
	u, err := a.svc.Autenticar(ctx, email, senha)
	if err != nil {
		return nil, aplicacaoauth.ErrCredenciaisInvalidas
	}
	return u, nil
}

func (a usuariosAuthLocal) PorUUID(ctx context.Context, id uuid.UUID) (*modeluser.User, error) {
	u, err := a.svc.Read(ctx, id)
	if err != nil {
		return nil, aplicacaoauth.ErrSessaoInvalida
	}
	return u, nil
}

func (a usuariosAuthLocal) RegistrarRefreshToken(ctx context.Context, usuarioUUID uuid.UUID, jti string, expiraEm time.Time) error {
	return a.svc.RegistrarSessao(ctx, usuarioUUID, jti, expiraEm)
}

func (a usuariosAuthLocal) RefreshTokenAtivo(ctx context.Context, usuarioUUID uuid.UUID, jti string) (bool, error) {
	return a.svc.SessaoAtiva(ctx, usuarioUUID, jti)
}

func (a usuariosAuthLocal) EncerrarSessao(ctx context.Context, usuarioUUID uuid.UUID, jti string) error {
	return a.svc.EncerrarSessao(ctx, usuarioUUID, jti)
}

// emissorLocal usa o Manager REAL do JWT (assinatura HMAC verdadeira) sem
// tocar no singleton do processo.
type emissorLocal struct{ m *jwt.Manager }

func (e emissorLocal) EmitirPar(in jwt.EntradaToken) (string, string, string, time.Time, error) {
	acesso, err := e.m.EmitirAcesso(in)
	if err != nil {
		return "", "", "", time.Time{}, err
	}
	refresh, jti, expira, err := e.m.EmitirRefresh(in)
	if err != nil {
		return "", "", "", time.Time{}, err
	}
	return acesso, refresh, jti, expira, nil
}

func (e emissorLocal) Validar(tokenTexto string) (*jwt.Claims, error) {
	return e.m.Validar(tokenTexto)
}

func (e emissorLocal) ValidarSemRevogacao(tokenTexto string) (*jwt.Claims, error) {
	return e.m.ValidarAssinatura(tokenTexto)
}

// resolvedorFixo devolve sempre a mesma organization — a resolução REAL por
// Host tem cobertura própria (middleware + ponta a ponta da organization).
type resolvedorFixo struct{ org uuid.UUID }

func (r resolvedorFixo) Resolver(context.Context, string) (uuid.UUID, bool, error) {
	return r.org, true, nil
}

// vitalidadeLocal espelha vitalidadeOrganizacao do boot sobre o service puro.
type vitalidadeLocal struct{ svc dominioOrganizacao.Service }

func (v vitalidadeLocal) Ativa(ctx context.Context, organizationUUID uuid.UUID) (bool, error) {
	o, err := v.svc.Read(orgctx.WithOrganization(ctx, organizationUUID), organizationUUID)
	if err != nil {
		if errors.Is(err, dominioOrganizacao.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return o.Status == orgmodel.StatusAtivo, nil
}

// --- Teste --------------------------------------------------------------------

// A cascata inteira num único fio: login real (JWT assinado + bcrypt +
// linha de refresh persistida) → inativação da organization → sessões
// revogadas NO BANCO, chaves de API fora do ar, refresh falha fechado.
func TestCascataDeInativacaoEncerraSessoesEApiKeys(t *testing.T) {
	if !dockerDisponivel(t) {
		t.Skip("docker indisponível — teste de integração pulado")
	}
	amb := subirAmbiente(t)

	svcUser := dominioUsuario.NewService(
		dominioUsuario.NewRepository(amb.db),
		dominioUsuario.NewRepositorioAtribuicoes(amb.db),
		validadorSemprePertence{}, dominioUsuario.NovasCredenciaisBcrypt())
	chaves := dominioOrganizacao.NewRepositorioApiKeys(amb.db)
	svcOrg := dominioOrganizacao.NewService(
		dominioOrganizacao.NewRepository(amb.db), chaves,
		suspensorNulo{}, encerradorLocal{svcUser},
		func() (string, error) { return "plataforma.teste", nil })

	org, err := svcOrg.Create(context.Background(), orgmodel.CreateInput{Nome: "Cascata"})
	require.NoError(t, err)
	ctxOrg := orgctx.WithOrganization(amb.ctx, org.UUID)

	ana, err := svcUser.Create(ctxOrg, dominioUsuario.EntradaCriacao{
		Dados: modeluser.CreateInput{Nome: "Ana Cascata", Email: "ana@cascata.teste"},
		Senha: "senha-segura-123",
	})
	require.NoError(t, err)

	gerenciador, err := jwt.Connect("segredo-de-teste-da-cascata-r4-32-bytes!!", 60, 168)
	require.NoError(t, err)
	app := aplicacaoauth.NewService(aplicacaoauth.Dependencias{
		Usuarios:     usuariosAuthLocal{svcUser},
		Emissor:      emissorLocal{gerenciador},
		Organizacoes: resolvedorFixo{org.UUID},
		Vitalidade:   vitalidadeLocal{svcOrg},
	})

	// Vida normal: login emite o par e o refresh renova.
	sessao, err := app.Login(amb.ctx, "cascata.exemplo.com", aplicacaoauth.LoginEntrada{
		Email: "ana@cascata.teste", Senha: "senha-segura-123",
	})
	require.NoError(t, err)
	renovado, err := app.Refresh(amb.ctx, sessao.RefreshToken)
	require.NoError(t, err)
	assert.NotEmpty(t, renovado.AccessToken)

	// Sessão extra registrada direto no subdomínio: prova a cascata no nível
	// do repositório, independente do ciclo JWT.
	require.NoError(t, svcUser.RegistrarSessao(ctxOrg, ana.UUID, "jti-cascata-1", time.Now().UTC().Add(time.Hour)))
	ativa, err := svcUser.SessaoAtiva(ctxOrg, ana.UUID, "jti-cascata-1")
	require.NoError(t, err)
	require.True(t, ativa)

	// Chave de API ativa resolve pelo hash antes da cascata.
	ctxAdmin := orgctx.WithPermissoes(ctxOrg, []string{"*:*"})
	_, chaveClara, err := svcOrg.CriarApiKey(ctxAdmin, org.UUID, dominioOrganizacao.ApiKeyEntrada{
		Nome:               "integração",
		EscopoOrganization: true,
		Permissoes:         []string{"identidade:user:ler"},
	})
	require.NoError(t, err)
	resolvida, err := chaves.BuscarApiKeyPorHash(amb.ctx, orgmodel.HashDeChave(chaveClara))
	require.NoError(t, err)
	require.NotNil(t, resolvida)

	// --- Inativação com cascata completa --------------------------------------
	inativo := orgmodel.StatusInativo
	_, err = svcOrg.Update(ctxOrg, org.UUID, orgmodel.UpdateInput{Status: &inativo})
	require.NoError(t, err)

	// Sessões mortas NO BANCO — não apenas invisíveis à aplicação.
	ativa, err = svcUser.SessaoAtiva(ctxOrg, ana.UUID, "jti-cascata-1")
	require.NoError(t, err)
	assert.False(t, ativa, "cascata revogou o refresh token persistido")
	revogado, err := dominioUsuario.NewRepository(amb.db).RefreshTokenRevogado("jti-cascata-1")
	require.NoError(t, err)
	assert.True(t, revogado, "revogação visível ao validador global do JWT")

	// Refresh falha FECHADO com a dona inativa — recusa genérica, sem vazamento.
	_, err = app.Refresh(amb.ctx, sessao.RefreshToken)
	assert.ErrorIs(t, err, aplicacaoauth.ErrSessaoInvalida)
	// R5: logout do token já revogado (rotação + cascata) é SUCESSO —
	// idempotente de verdade; destruição não pede licença à vitalidade.
	assert.NoError(t, app.Logout(amb.ctx, sessao.RefreshToken))

	// X-Api-Key de organization inativa falha fechada (defesa em profundidade
	// do JOIN além da revogação em cascata).
	_, err = chaves.BuscarApiKeyPorHash(amb.ctx, orgmodel.HashDeChave(chaveClara))
	assert.ErrorIs(t, err, dominioOrganizacao.ErrApiKeyNaoEncontrada)
}
