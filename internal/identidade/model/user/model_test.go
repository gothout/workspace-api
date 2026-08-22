package user

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseEmailValidaFormatoENormaliza(t *testing.T) {
	casos := []struct {
		nome     string
		entrada  string
		esperado Email
		erro     error
	}{
		{"válido", " Ana.Lima@Exemplo.COM ", "ana.lima@exemplo.com", nil},
		{"com pontos e traço", "joao.silva+ops@sub.dominio.io", "joao.silva+ops@sub.dominio.io", nil},
		{"sem arroba", "ana.exemplo.com", "", ErrEmailInvalido},
		{"sem domínio", "ana@", "", ErrEmailInvalido},
		{"sem tld", "ana@exemplo", "", ErrEmailInvalido},
		{"espaço interno", "an a@exemplo.com", "", ErrEmailInvalido},
		{"vazio", "   ", "", ErrEmailInvalido},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			email, err := ParseEmail(caso.entrada)
			if caso.erro != nil {
				assert.ErrorIs(t, err, caso.erro)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, caso.esperado, email)
		})
	}
}

func TestValidarSenhaAplicaAPolitica(t *testing.T) {
	assert.ErrorIs(t, ValidarSenha("curta"), ErrSenhaInvalida, "abaixo do mínimo")
	assert.ErrorIs(t, ValidarSenha(string(make([]byte, TamanhoMaximoSenha+1))), ErrSenhaInvalida, "acima do limite do bcrypt")
	assert.NoError(t, ValidarSenha("senha-segura-123"))
}

func TestNewUserValidaInvariantesEGuardaHash(t *testing.T) {
	org := uuid.New()
	u, err := NewUser(CreateInput{
		OrganizationUUID: org,
		Nome:             "  Ana Lima  ",
		Email:            "Ana@Exemplo.COM",
		SenhaHash:        "$2a$10$hash-falso-para-teste",
	})
	require.NoError(t, err)
	assert.Equal(t, org, u.OrganizationUUID)
	assert.Equal(t, "Ana Lima", u.Nome)
	assert.Equal(t, Email("ana@exemplo.com"), u.Email)
	assert.Equal(t, StatusAtivo, u.Status)
	assert.Equal(t, "identidade_user_user", u.TableName())

	casos := []struct {
		nome    string
		entrada CreateInput
		erro    error
	}{
		{"nome curto", CreateInput{OrganizationUUID: org, Nome: "a", Email: "a@b.co", SenhaHash: "x"}, ErrNomeInvalido},
		{"email inválido", CreateInput{OrganizationUUID: org, Nome: "Válido", Email: "ruim", SenhaHash: "x"}, ErrEmailInvalido},
		{"hash ausente", CreateInput{OrganizationUUID: org, Nome: "Válido", Email: "a@b.co"}, ErrHashAusente},
		{"organization ausente", CreateInput{Nome: "Válido", Email: "a@b.co", SenhaHash: "x"}, ErrHashAusente},
	}
	for _, caso := range casos {
		_, err := NewUser(caso.entrada)
		assert.ErrorIs(t, err, caso.erro, "%s deveria recusar", caso.nome)
	}
}

// O hash NUNCA vaza em serialização — nem no usuário, nem em envelopes.
func TestHashNaoVazaEmNenhumaSerializacao(t *testing.T) {
	u, err := NewUser(CreateInput{
		OrganizationUUID: uuid.New(),
		Nome:             "Ana Lima",
		Email:            "ana@exemplo.com",
		SenhaHash:        "$2a$10$segredo-muito-secreto",
	})
	require.NoError(t, err)

	bruto, err := json.Marshal(u)
	require.NoError(t, err)
	assert.NotContains(t, string(bruto), "segredo-muito-secreto")
	assert.NotContains(t, string(bruto), "senha_hash")
	assert.NotContains(t, string(bruto), "deleted_at")
}

func TestTransicoesDeEstadoDoUsuario(t *testing.T) {
	u, err := NewUser(CreateInput{OrganizationUUID: uuid.New(), Nome: "Ana", Email: "a@b.co", SenhaHash: "x"})
	require.NoError(t, err)

	assert.ErrorIs(t, u.Reativar(), ErrJaAtivo, "reativar ativo recusa")
	require.NoError(t, u.Inativar())
	assert.ErrorIs(t, u.Inativar(), ErrJaInativo, "inativar inativo recusa")
	assert.False(t, u.Autenticavel(), "inativo não autentica")
	require.NoError(t, u.Reativar())
	assert.True(t, u.Autenticavel())
}

func TestRefreshTokenCicloDeVida(t *testing.T) {
	agora := time.Now().UTC()
	token, err := NewRefreshToken(CreateRefreshTokenInput{
		OrganizationUUID: uuid.New(),
		UserUUID:         uuid.New(),
		JTI:              "jti-unico",
		ExpiraEm:         agora.Add(time.Hour),
	})
	require.NoError(t, err)
	assert.Equal(t, "identidade_user_refresh_token", token.TableName())
	assert.True(t, token.Ativo(agora))

	// Revogação persistida e idempotente — o primeiro carimbo fica.
	token.Revogar(agora.Add(time.Minute))
	require.NotNil(t, token.RevogadoEm)
	primeiro := *token.RevogadoEm
	token.Revogar(agora.Add(2 * time.Minute))
	assert.Equal(t, primeiro, *token.RevogadoEm)
	assert.False(t, token.Ativo(agora.Add(3*time.Minute)))

	_, err = NewRefreshToken(CreateRefreshTokenInput{JTI: " ", ExpiraEm: agora})
	assert.ErrorIs(t, err, ErrRefreshInvalido, "jti vazio e sem expiração recusa")
}

func TestNovaAtribuicaoValidaVinculos(t *testing.T) {
	completa := CreateAtribuicaoInput{
		OrganizationUUID: uuid.New(), WorkspaceUUID: uuid.New(),
		UserUUID: uuid.New(), PapelUUID: uuid.New(),
	}
	a, err := NovaAtribuicao(completa)
	require.NoError(t, err)
	assert.Equal(t, "identidade_user_atribuicao", a.TableName())
	assert.Equal(t, StatusAtivo, StatusAtivo) // conjunto fechado segue válido

	semWorkspace := completa
	semWorkspace.WorkspaceUUID = uuid.Nil
	_, err = NovaAtribuicao(semWorkspace)
	assert.ErrorIs(t, err, ErrAtribuicaoInvalida)
}

func TestConjuntoFechadoDeStatus(t *testing.T) {
	assert.True(t, StatusAtivo.Valido())
	assert.True(t, StatusInativo.Valido())
	assert.False(t, StatusUsuario("qualquer").Valido())
}
