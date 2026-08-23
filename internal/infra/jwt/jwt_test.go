package jwt

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConnectValidaParametros(t *testing.T) {
	manager, err := Connect("segredo-suficientemente-longo-do-teste", 60, 168)
	require.NoError(t, err)
	assert.Equal(t, 60*time.Minute, manager.TTLAccess())
	assert.Equal(t, 168*time.Hour, manager.TTLRefresh())
}

func TestConnectReprovaParametrosInvalidos(t *testing.T) {
	casos := []struct {
		nome    string
		segredo string
		ttl     int
		refresh int
	}{
		{"segredo vazio", "", 60, 168},
		{"segredo curto", "curto", 60, 168},
		{"segredo com 31 bytes", strings.Repeat("a", 31), 60, 168},
		{"ttl zero", "segredo-suficientemente-longo-do-teste", 0, 168},
		{"refresh zero", "segredo-suficientemente-longo-do-teste", 60, 0},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			_, err := Connect(caso.segredo, caso.ttl, caso.refresh)
			assert.Error(t, err)
		})
	}
}

// R6 (issue #24): HS256 exige chave de 256 bits — 31 bytes recusa com erro
// claro; exatamente 32 bytes passa.
func TestConnectExigeSegredoDe32Bytes(t *testing.T) {
	_, err := Connect(strings.Repeat("a", 31), 60, 168)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "32 bytes")

	m, err := Connect(strings.Repeat("a", 32), 60, 168)
	require.NoError(t, err)
	require.NotNil(t, m)
}

// entradaExemplo monta uma identidade de teste reutilizável.
func entradaExemplo() EntradaToken {
	return EntradaToken{
		UserUUID:         uuid.MustParse("11111111-1111-4111-8111-111111111111"),
		OrganizationUUID: uuid.MustParse("22222222-2222-4222-8222-222222222222"),
		WorkspaceUUID:    uuid.MustParse("33333333-3333-4333-8333-333333333333"),
		Nome:             "Maria",
		Email:            "maria@example.com",
	}
}

func TestEmitirEValidarAcesso(t *testing.T) {
	m, err := Connect("segredo-suficientemente-longo-do-teste", 30, 24)
	require.NoError(t, err)

	token, err := m.EmitirAcesso(entradaExemplo())
	require.NoError(t, err)

	claims, err := m.Validar(token)
	require.NoError(t, err)
	assert.Equal(t, ClaimTipoAccess, claims.Tipo)
	assert.Equal(t, "11111111-1111-4111-8111-111111111111", claims.UserUUID)
	assert.Equal(t, "22222222-2222-4222-8222-222222222222", claims.OrganizationUUID)
	assert.Equal(t, "33333333-3333-4333-8333-333333333333", claims.WorkspaceUUID)
	assert.Equal(t, "Maria", claims.Nome)
	assert.Equal(t, "maria@example.com", claims.Email)
}

func TestAcessoSemWorkspaceOmiteClaimWks(t *testing.T) {
	m, err := Connect("segredo-suficientemente-longo-do-teste", 30, 24)
	require.NoError(t, err)

	in := entradaExemplo()
	in.WorkspaceUUID = uuid.Nil
	token, err := m.EmitirAcesso(in)
	require.NoError(t, err)

	claims, err := m.Validar(token)
	require.NoError(t, err)
	assert.Empty(t, claims.WorkspaceUUID, "sem workspace ativo a claim wks não vai")
}

func TestEmitirEValidarRefreshComJtiUnico(t *testing.T) {
	m, err := Connect("segredo-suficientemente-longo-do-teste", 30, 24)
	require.NoError(t, err)

	token1, jti1, expira1, err := m.EmitirRefresh(entradaExemplo())
	require.NoError(t, err)
	_, jti2, _, err := m.EmitirRefresh(entradaExemplo())
	require.NoError(t, err)

	assert.NotEqual(t, jti1, jti2, "jti é único por refresh")
	assert.WithinDuration(t, time.Now().Add(24*time.Hour), expira1, time.Minute)

	claims, err := m.Validar(token1)
	require.NoError(t, err)
	assert.Equal(t, ClaimTipoRefresh, claims.Tipo)
	assert.Equal(t, jti1, claims.JTI)
}

func TestValidarReprovaCasosDeAtaque(t *testing.T) {
	m, err := Connect("segredo-suficientemente-longo-do-teste", 30, 24)
	require.NoError(t, err)
	outro, _ := Connect("outro-segredo-suficiente-para-testar!", 30, 24)

	falsificado, err := outro.EmitirAcesso(entradaExemplo())
	require.NoError(t, err)

	casos := []struct {
		nome  string
		token string
		erro  error
	}{
		{"assinatura errada", falsificado, ErrTokenInvalido},
		{"lixo", "nao-e-um-token", ErrTokenInvalido},
		{"vazio", "", ErrTokenInvalido},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			_, err := m.Validar(caso.token)
			assert.ErrorIs(t, err, caso.erro)
		})
	}
}

func TestValidarReprovaTokenExpirado(t *testing.T) {
	m, err := Connect("segredo-suficientemente-longo-do-teste", 30, 24)
	require.NoError(t, err)
	// Token vencido: emite com validade negativa direto no manager de teste.
	m.ttlAccess = -time.Minute

	token, err := m.EmitirAcesso(entradaExemplo())
	require.NoError(t, err)

	_, err = m.Validar(token)
	assert.ErrorIs(t, err, ErrTokenExpirado)
}

func TestValidarReprovaMetodoDiferente(t *testing.T) {
	m, err := Connect("segredo-suficientemente-longo-do-teste", 30, 24)
	require.NoError(t, err)

	// Assina com NONE — algoritmo fora da lista permitida é recusado.
	agora := time.Now().UTC()
	claims := &Claims{
		Tipo:             ClaimTipoAccess,
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(agora.Add(time.Hour))},
	}
	falso := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	token, err := falso.SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	_, err = m.Validar(token)
	assert.ErrorIs(t, err, ErrTokenInvalido)
}

func TestValidarRefreshRevogado(t *testing.T) {
	m, err := Connect("segredo-suficientemente-longo-do-teste", 30, 24)
	require.NoError(t, err)

	token, jti, _, err := m.EmitirRefresh(entradaExemplo())
	require.NoError(t, err)

	revogados := map[string]bool{jti: true}
	m.DefinirRevogador(revogadorFalso(revogados))

	_, err = m.Validar(token)
	assert.ErrorIs(t, err, ErrTokenInvalido, "refresh revogado não valida")

	delete(revogados, jti)
	_, err = m.Validar(token)
	assert.NoError(t, err, "sem revogação o refresh volta a validar")
}

// revogadorFalso implementa o contrato RevogadorDeRefresh em memória.
type revogadorFalso map[string]bool

func (r revogadorFalso) Revogado(jti string) (bool, error) { return r[jti], nil }

// Access token NÃO passa pela conferência de revogação — só o refresh.
func TestAccessTokenIgnoraRevogador(t *testing.T) {
	m, err := Connect("segredo-suficientemente-longo-do-teste", 30, 24)
	require.NoError(t, err)
	m.DefinirRevogador(revogadorFalso{})

	token, err := m.EmitirAcesso(entradaExemplo())
	require.NoError(t, err)

	_, err = m.Validar(token)
	assert.NoError(t, err)
}

func TestGetAntesDoInitDevolveErro(t *testing.T) {
	ResetarParaTeste()
	_, err := Get()
	assert.ErrorIs(t, err, ErrNaoInicializado)
}

func TestCicloDoSingletonUmaUnicaVez(t *testing.T) {
	ResetarParaTeste()
	primeiro, err := Connect("segredo-do-teste-de-ciclo-do-singleton", 30, 24)
	require.NoError(t, err)

	// Simula o boot: monta o singleton sem passar por config (injeção direta
	// no estado do pacote — o sync.Once real é exercido no bootstrap).
	instance = primeiro

	obtido, err := Get()
	require.NoError(t, err)
	assert.Same(t, primeiro, obtido, "Get devolve sempre a mesma instância")

	Close()
	_, err = Get()
	assert.ErrorIs(t, err, ErrNaoInicializado, "após Close o singleton não responde mais")
}
