// Cenário R5 (issue #23) ponta a ponta sobre o esquema migrado, com JWT
// REAL e Postgres efêmero:
//
//  1. Logout IDEMPOTENTE — repetir o logout do mesmo refresh é sucesso;
//     só a entrada inválida recusa.
//  2. ROTAÇÃO de refresh — renovar revoga o jti anterior NO BANCO e o reuso
//     dele falha fechado; a sessão nova segue viva.
//
// Montagem por construtores PUROS (mesmo desenho da cascata_organization_test.go):
// o singleton é POR PROCESSO e este pacote tem teste que boota os contêineres.
package bootstrap

import (
	"context"
	"testing"

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

// montarAuthReal sobe banco efêmero + org/user reais + aplicação auth com
// Manager de JWT verdadeiro (assinatura HMAC, sem revogador ligado ao
// manager — quem confere linha ativa aqui é o contrato Usuarios, como no boot).
func montarAuthReal(t *testing.T) (aplicacaoauth.Service, *ambienteBanco, *jwt.Manager) {
	t.Helper()
	amb := subirAmbiente(t)

	svcUser := dominioUsuario.NewService(
		dominioUsuario.NewRepository(amb.db),
		dominioUsuario.NewRepositorioAtribuicoes(amb.db),
		validadorSemprePertence{}, dominioUsuario.NovasCredenciaisBcrypt())
	svcOrg := dominioOrganizacao.NewService(
		dominioOrganizacao.NewRepository(amb.db),
		dominioOrganizacao.NewRepositorioApiKeys(amb.db),
		suspensorNulo{}, encerradorLocal{svcUser},
		func() (string, error) { return "plataforma.teste", nil })

	org, err := svcOrg.Create(context.Background(), orgmodel.CreateInput{Nome: "Rotação"})
	require.NoError(t, err)
	ctxOrg := orgctx.WithOrganization(amb.ctx, org.UUID)
	_, err = svcUser.Create(ctxOrg, dominioUsuario.EntradaCriacao{
		Dados: modeluser.CreateInput{Nome: "Ana Rotação", Email: "ana@rotacao.teste"},
		Senha: "senha-segura-123",
	})
	require.NoError(t, err)

	gerenciador, err := jwt.Connect("segredo-de-teste-da-rotacao-r5-32-bytes!", 60, 168)
	require.NoError(t, err)
	app := aplicacaoauth.NewService(aplicacaoauth.Dependencias{
		Usuarios:     usuariosAuthLocal{svcUser},
		Emissor:      emissorLocal{gerenciador},
		Organizacoes: resolvedorFixo{org.UUID},
		Vitalidade:   vitalidadeLocal{svcOrg},
	})
	return app, amb, gerenciador
}

// jtiDe extrai o jti de um refresh token assinado (sem revogador ligado,
// Validar não recusa token já rodado).
func jtiDe(t *testing.T, gerenciador *jwt.Manager, refreshToken string) string {
	t.Helper()
	claims, err := gerenciador.Validar(refreshToken)
	require.NoError(t, err)
	return claims.JTI
}

const hostRotacao = "rotacao.exemplo.com"

func logarAna(t *testing.T, app aplicacaoauth.Service, amb *ambienteBanco) *aplicacaoauth.SessaoResponseDto {
	t.Helper()
	sessao, err := app.Login(amb.ctx, hostRotacao, aplicacaoauth.LoginEntrada{
		Email: "ana@rotacao.teste", Senha: "senha-segura-123",
	})
	require.NoError(t, err)
	return sessao
}

// Fluxo 1 — logout idempotente: o primeiro logout revoga NO BANCO; repetir
// com o MESMO token é sucesso; o refresh com ele falha fechado; lixo recusa.
func TestLogoutIdempotentePontaAPonta(t *testing.T) {
	if !dockerDisponivel(t) {
		t.Skip("docker indisponível — teste de integração pulado")
	}
	app, amb, gerenciador := montarAuthReal(t)
	repo := dominioUsuario.NewRepository(amb.db)

	sessao := logarAna(t, app, amb)
	jti := jtiDe(t, gerenciador, sessao.RefreshToken)

	revogado, err := repo.RefreshTokenRevogado(jti)
	require.NoError(t, err)
	require.False(t, revogado, "pré-condição: linha do jti ativa após o login")

	// Primeiro logout: revoga no banco.
	require.NoError(t, app.Logout(amb.ctx, sessao.RefreshToken))
	revogado, err = repo.RefreshTokenRevogado(jti)
	require.NoError(t, err)
	assert.True(t, revogado, "revogação persistida na primeira chamada")

	// Segundo (e terceiro): caminho idempotente alcançável — sucesso, sem erro.
	assert.NoError(t, app.Logout(amb.ctx, sessao.RefreshToken), "logout repetido não falha")
	assert.NoError(t, app.Logout(amb.ctx, sessao.RefreshToken))

	// A sessão continua morta para o refresh.
	_, err = app.Refresh(amb.ctx, sessao.RefreshToken)
	assert.ErrorIs(t, err, aplicacaoauth.ErrSessaoInvalida)

	// Lixo segue recusando — idempotente não é porta aberta.
	assert.ErrorIs(t, app.Logout(amb.ctx, "nao-e-token"), aplicacaoauth.ErrSessaoInvalida)
}

// Fluxo 2 — rotação: renovar revoga o jti anterior NO BANCO; reuso do antigo
// falha fechado; a cadeia segue viva pelo par novo; logout do token já
// rodado pela rotação é sucesso.
func TestRefreshRotacionaPontaAPonta(t *testing.T) {
	if !dockerDisponivel(t) {
		t.Skip("docker indisponível — teste de integração pulado")
	}
	app, amb, gerenciador := montarAuthReal(t)
	repo := dominioUsuario.NewRepository(amb.db)

	primeira := logarAna(t, app, amb)
	jti1 := jtiDe(t, gerenciador, primeira.RefreshToken)

	segunda, err := app.Refresh(amb.ctx, primeira.RefreshToken)
	require.NoError(t, err)
	jti2 := jtiDe(t, gerenciador, segunda.RefreshToken)
	require.NotEqual(t, jti1, jti2, "rotação emite jti novo")

	revogado1, err := repo.RefreshTokenRevogado(jti1)
	require.NoError(t, err)
	assert.True(t, revogado1, "rotação revogou o jti anterior no Postgres")

	// Reuso do antigo: falha fechado.
	_, err = app.Refresh(amb.ctx, primeira.RefreshToken)
	assert.ErrorIs(t, err, aplicacaoauth.ErrSessaoInvalida, "refresh renovado não serve de novo")

	// A sessão atual segue viva — e rotaciona de novo.
	terceira, err := app.Refresh(amb.ctx, segunda.RefreshToken)
	require.NoError(t, err)
	jti3 := jtiDe(t, gerenciador, terceira.RefreshToken)
	require.NotEqual(t, jti2, jti3)
	revogado2, err := repo.RefreshTokenRevogado(jti2)
	require.NoError(t, err)
	assert.True(t, revogado2)

	// Logout do token JÁ RODADO pela rotação: idempotente, sucesso.
	assert.NoError(t, app.Logout(amb.ctx, segunda.RefreshToken))
}
