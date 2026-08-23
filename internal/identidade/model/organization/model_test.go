package organization

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDominio(t *testing.T) {
	const baseDomain = "exemplo.com"

	aceitos := []struct {
		valor    string
		esperado string
	}{
		{"Parceiro.COM", "parceiro.com"},
		{"  parceiro.com  ", "parceiro.com"},
		{"sub.parceiro.com.", "sub.parceiro.com"},
		{"foo.co.uk", "foo.co.uk"},       // eTLD+1 válido (co.uk é o public suffix)
		{"minha.app.br", "minha.app.br"}, // app.br não é public suffix conhecido
	}
	for _, caso := range aceitos {
		vo, err := ParseDominio(caso.valor, baseDomain)
		require.NoError(t, err, "valor %q deveria ser aceito", caso.valor)
		assert.Equal(t, caso.esperado, vo.String())
	}

	recusados := map[string]string{
		"":                                "vazio",
		"Parceiro.Com:8080":               "com porta",
		"-ruim.com":                       "rótulo começando em hífen",
		"ruim-.com":                       "rótulo terminando em hífen",
		"a_b.com":                         "underline",
		"localhost":                       "rótulo único",
		"com":                             "TLD",
		"co.uk":                           "public suffix puro",
		"com.br":                          "public suffix composto",
		"exemplo.com":                     "igual ao base_domain",
		"sub.exemplo.com":                 "descendente do base_domain",
		strings.Repeat("a.", 126) + "com": "acima de 253 caracteres",
	}
	if baseDomain != "" && strings.Contains(baseDomain, ".") {
		parts := strings.SplitN(baseDomain, ".", 2)
		recusados[parts[1]] = "ascendente do base_domain"
	}
	for valor, motivo := range recusados {
		if valor == "" {
			continue
		}
		_, err := ParseDominio(valor, baseDomain)
		assert.ErrorIs(t, err, ErrDominioInvalido, "valor %q (%s) deveria ser recusado", valor, motivo)
	}

	// base_domain vazio: só as regras universais valem.
	vo, err := ParseDominio("exemplo.com", "")
	require.NoError(t, err)
	assert.Equal(t, "exemplo.com", vo.String())
}

func TestStatusValido(t *testing.T) {
	assert.True(t, StatusAtivo.Valido())
	assert.True(t, StatusInativo.Valido())
	assert.False(t, Status("estranho").Valido())
}

func TestNewOrganizationEComportamentos(t *testing.T) {
	o, err := NewOrganization(CreateInput{Nome: "  Acme  ", Documento: "12345678900"})
	require.NoError(t, err)
	assert.Equal(t, "Acme", o.Nome)
	assert.Equal(t, StatusAtivo, o.Status)
	assert.NotEqual(t, uuid.Nil, o.UUID)

	_, err = NewOrganization(CreateInput{Nome: "a"})
	assert.ErrorIs(t, err, ErrNomeInvalido)

	require.NoError(t, o.Renomear("  Nova Acme "))
	assert.Equal(t, "Nova Acme", o.Nome)
	assert.ErrorIs(t, o.Renomear(" "), ErrNomeInvalido)
}

func TestTransicoesDeEstado(t *testing.T) {
	o, err := NewOrganization(CreateInput{Nome: "Acme"})
	require.NoError(t, err)

	require.NoError(t, o.Inativar())
	assert.Equal(t, StatusInativo, o.Status)
	assert.ErrorIs(t, o.Inativar(), ErrJaInativo)

	require.NoError(t, o.Reativar())
	assert.Equal(t, StatusAtivo, o.Status)
	assert.ErrorIs(t, o.Reativar(), ErrJaAtivo)
}

func TestCicloDoDominioCustomNaEntidade(t *testing.T) {
	o, err := NewOrganization(CreateInput{Nome: "Acme"})
	require.NoError(t, err)

	assert.False(t, o.TemDominio())
	assert.ErrorIs(t, o.RemoverDominio(), ErrDominioNaoDefinido)

	vo, err := ParseDominio("parceiro.com", "exemplo.com")
	require.NoError(t, err)
	o.DefinirDominio(vo)
	assert.True(t, o.TemDominio())

	require.NoError(t, o.RemoverDominio())
	assert.False(t, o.TemDominio())
}

func TestValueEScanDoVO(t *testing.T) {
	var d DominioCustom
	require.NoError(t, d.Scan(nil))
	assert.Empty(t, d)

	require.NoError(t, d.Scan([]byte("parceiro.com")))
	assert.Equal(t, DominioCustom("parceiro.com"), d)
	v, err := d.Value()
	require.NoError(t, err)
	assert.Equal(t, "parceiro.com", v.(string))

	vazia := DominioCustom("")
	v, err = vazia.Value()
	require.NoError(t, err)
	assert.Nil(t, v, "domínio ausente persiste como NULL")
}

func TestNewApiKeyValidacoes(t *testing.T) {
	org := uuid.MustParse("aaaaaaaa-0000-4000-8000-000000000001")
	ws := uuid.MustParse("bbbbbbbb-0000-4000-8000-000000000001")
	expira := time.Now().UTC().Add(24 * time.Hour)

	base := CreateApiKeyInput{
		OrganizationUUID:     org,
		Nome:                 "integração",
		KeyHash:              "hash",
		EscopoOrganization:   false,
		WorkspacesPermitidos: []uuid.UUID{ws},
		Permissoes:           []string{"identidade:workspace:ler"},
		ExpiresAt:            &expira,
	}

	k, err := NewApiKey(base)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, k.UUID)
	assert.Equal(t, StatusAtivo, k.Status)

	casos := map[string]func(in CreateApiKeyInput) CreateApiKeyInput{
		"nome curto":         func(in CreateApiKeyInput) CreateApiKeyInput { in.Nome = "ab"; return in },
		"sem hash":           func(in CreateApiKeyInput) CreateApiKeyInput { in.KeyHash = " "; return in },
		"sem organization":   func(in CreateApiKeyInput) CreateApiKeyInput { in.OrganizationUUID = uuid.Nil; return in },
		"sem permissões":     func(in CreateApiKeyInput) CreateApiKeyInput { in.Permissoes = nil; return in },
		"permissão inválida": func(in CreateApiKeyInput) CreateApiKeyInput { in.Permissoes = []string{"workspace:ler"}; return in },
		"escopo vazio": func(in CreateApiKeyInput) CreateApiKeyInput {
			in.WorkspacesPermitidos = nil
			return in
		},
	}
	for nome, mutar := range casos {
		_, err := NewApiKey(mutar(base))
		switch nome {
		case "permissão inválida":
			assert.ErrorIs(t, err, ErrPermissaoInvalida, nome)
		case "escopo vazio":
			assert.ErrorIs(t, err, ErrEscopoApiKeyInvalido, nome)
		default:
			assert.True(t, errors.Is(err, ErrApiKeyInvalida), "%s: erro inesperado %v", nome, err)
		}
	}

	// Escopo organization inteira dispensa lista de workspaces.
	escopoOrg := base
	escopoOrg.EscopoOrganization = true
	escopoOrg.WorkspacesPermitidos = nil
	_, err = NewApiKey(escopoOrg)
	assert.NoError(t, err)
}

func TestChavesSegredo(t *testing.T) {
	chave, hash, err := NovaChaveSegredo()
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(chave, "wka_"))
	assert.Len(t, hash, 64)
	assert.Equal(t, HashDeChave(chave), hash)
	assert.NotContains(t, hash, chave)

	chave2, _, err := NovaChaveSegredo()
	require.NoError(t, err)
	assert.NotEqual(t, chave, chave2, "chaves geradas nunca se repetem")
}

func TestListasJSONBRoundtrip(t *testing.T) {
	textos := ListaTextos{"identidade:workspace:ler", "*:*"}
	v, err := textos.Value()
	require.NoError(t, err)
	var deVoltaTextos ListaTextos
	require.NoError(t, deVoltaTextos.Scan(v))
	assert.Equal(t, textos, deVoltaTextos)

	uuids := ListaUUIDs{uuid.New(), uuid.New()}
	v, err = uuids.Value()
	require.NoError(t, err)
	var deVoltaUUIDs ListaUUIDs
	require.NoError(t, deVoltaUUIDs.Scan(v))
	assert.Equal(t, uuids, deVoltaUUIDs)

	var vazia ListaTextos
	require.NoError(t, vazia.Scan(nil))
	assert.Empty(t, vazia)

	require.NoError(t, vazia.Scan([]byte("[]")))
	assert.NotNil(t, vazia)
	assert.Empty(t, vazia)

	err = (&deVoltaUUIDs).Scan([]byte(`["nao-e-uuid"]`))
	assert.Error(t, err)
}
