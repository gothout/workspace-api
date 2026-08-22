package catalogo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"workspace-api/internal/pkg/orgctx"
	"workspace-api/internal/pkg/rest_err"
)

// catalogoFalso é um Catalogo() de mentira: duas permissões de workspace,
// uma de user com DUAS rotas e uma malformada que nunca pode vazar na árvore.
func catalogoFalso() []PermissaoMeta {
	return []PermissaoMeta{
		{
			Permissao: "identidade:workspace:criar",
			Descricao: "Criar workspace",
			Rotas:     []RotaMeta{{Rota: "/api/domain/identidade/workspaces", Metodo: http.MethodPost}},
			GrupoMenu: "Identidade · Workspaces",
		},
		{
			Permissao: "identidade:workspace:editar",
			Descricao: "Editar workspace",
			Rotas:     []RotaMeta{{Rota: "/api/domain/identidade/workspaces/{uuid}", Metodo: http.MethodPatch}},
			GrupoMenu: "Identidade · Workspaces",
		},
		{
			Permissao: "identidade:user:ler",
			Descricao: "Listar usuários",
			Rotas: []RotaMeta{
				{Rota: "/api/domain/identidade/users", Metodo: http.MethodGet},
				{Rota: "/api/domain/identidade/users/{uuid}", Metodo: http.MethodGet},
			},
			GrupoMenu: "Identidade · Usuários",
		},
		{Permissao: "malformada", Descricao: "nunca deve aparecer", Rotas: []RotaMeta{{Rota: "/x", Metodo: "GET"}}},
	}
}

type provedorFalso struct{ itens []PermissaoMeta }

func (p provedorFalso) Catalogo() []PermissaoMeta { return p.itens }

func servicoComCatalogoFalso() Service {
	return NewService(provedorFalso{itens: catalogoFalso()})
}

func TestMinhasPermissoesFiltraPeloUsuario(t *testing.T) {
	svc := servicoComCatalogoFalso()

	t.Run("permissão exata vê só as ações dela", func(t *testing.T) {
		ctx := orgctx.WithPermissoes(context.Background(), []string{"identidade:workspace:criar"})
		arvore := svc.MinhasPermissoes(ctx)
		require.Len(t, arvore.Dominios, 1)
		require.Len(t, arvore.Dominios[0].Subdominios, 1)
		acoes := arvore.Dominios[0].Subdominios[0]
		assert.Equal(t, "identidade", arvore.Dominios[0].Dominio)
		assert.Equal(t, "workspace", acoes.Subdominio)
		require.Len(t, acoes.Acoes, 1)
		assert.Equal(t, AcaoDto{
			Permissao: "identidade:workspace:criar",
			Descricao: "Criar workspace",
			Rota:      "/api/domain/identidade/workspaces",
			Metodo:    http.MethodPost,
			GrupoMenu: "Identidade · Workspaces",
		}, acoes.Acoes[0])
	})

	t.Run("curinga por segmento libera o subdomínio inteiro e nada de irmãos", func(t *testing.T) {
		ctx := orgctx.WithPermissoes(context.Background(), []string{"identidade:workspace:*"})
		arvore := svc.MinhasPermissoes(ctx)
		require.Len(t, arvore.Dominios, 1)
		subdominios := arvore.Dominios[0].Subdominios
		require.Len(t, subdominios, 1, "user não pode aparecer para quem só tem curinga de workspace")
		assert.Equal(t, "workspace", subdominios[0].Subdominio)
		assert.Len(t, subdominios[0].Acoes, 2)
	})

	t.Run("super_admin com *:* recebe a árvore inteira", func(t *testing.T) {
		ctx := orgctx.WithPermissoes(context.Background(), []string{"*:*"})
		arvore := svc.MinhasPermissoes(ctx)
		require.Len(t, arvore.Dominios, 1)
		total := 0
		for _, dominio := range arvore.Dominios {
			for _, sub := range dominio.Subdominios {
				total += len(sub.Acoes)
			}
		}
		assert.Equal(t, 4, total, "3 permissões válidas × suas rotas (1+1+2); malformada nunca entra")
	})

	t.Run("usuário sem nenhuma permissão recebe árvore vazia", func(t *testing.T) {
		arvore := svc.MinhasPermissoes(context.Background())
		require.NotNil(t, arvore.Dominios, "dominios nulo serializa como null — contrato pede lista")
		assert.Empty(t, arvore.Dominios)
	})
}

func TestUmParRotaMetodoEmitUmaAcao(t *testing.T) {
	svc := servicoComCatalogoFalso()
	ctx := orgctx.WithPermissoes(context.Background(), []string{"identidade:user:ler"})
	arvore := svc.MinhasPermissoes(ctx)
	require.Len(t, arvore.Dominios, 1)
	require.Len(t, arvore.Dominios[0].Subdominios, 1)
	acoes := arvore.Dominios[0].Subdominios[0].Acoes
	require.Len(t, acoes, 2, "permissão com 2 rotas emite 2 ações")
	assert.Equal(t, "/api/domain/identidade/users", acoes[0].Rota)
	assert.Equal(t, http.MethodGet, acoes[0].Metodo)
	assert.Equal(t, "/api/domain/identidade/users/{uuid}", acoes[1].Rota)
}

func TestFormatoSegueContratoDoDoc04(t *testing.T) {
	ctx := orgctx.WithPermissoes(context.Background(), []string{"identidade:user:ler"})
	corpo, err := json.Marshal(servicoComCatalogoFalso().MinhasPermissoes(ctx))
	require.NoError(t, err)
	texto := string(corpo)
	for _, chave := range []string{
		`"dominios"`, `"dominio":"identidade"`, `"subdominios"`, `"subdominio":"user"`,
		`"acoes"`, `"permissao"`, `"descricao"`, `"rota"`, `"metodo"`, `"grupo_menu"`,
	} {
		assert.Contains(t, texto, chave, "contrato do doc 04 exige %s no corpo", chave)
	}
}

func TestMapaDeErrosRefleteRegistroSemDuplicar(t *testing.T) {
	rest_err.ResetarRegistroParaTeste()
	defer rest_err.ResetarRegistroParaTeste()

	rest_err.RegistrarCatalogo("identidade", "fake_a", map[error]rest_err.ErroCatalogado{
		errors.New("a1"): {Codigo: "identidade.fake_a.um", Mensagem: "Um.", Status: http.StatusBadRequest},
		errors.New("a2"): {Codigo: "identidade.fake_a.dois", Mensagem: "Dois.", Status: http.StatusConflict},
	})
	// Duas sentinelas apontando o MESMO código dentro de um catálogo = UMA
	// entrada no mapa (dedupe do rest_err) — o front nunca vê duplicata.
	rest_err.RegistrarCatalogo("identidade", "fake_b", map[error]rest_err.ErroCatalogado{
		errors.New("b1"): {Codigo: "identidade.fake_b.um", Mensagem: "Um.", Status: http.StatusNotFound},
		errors.New("b2"): {Codigo: "identidade.fake_b.um", Mensagem: "Um.", Status: http.StatusNotFound},
	})

	mapa := NewService(provedorFalso{}).MapaDeErros()
	require.Len(t, mapa.Erros, 2, "um grupo por domínio.subdomínio, sem duplicação")

	primeiro := mapa.Erros[0]
	assert.Equal(t, "identidade", primeiro.Dominio)
	assert.Equal(t, "fake_a", primeiro.Subdominio)
	require.Len(t, primeiro.Erros, 2)
	assert.Equal(t, "identidade.fake_a.dois", primeiro.Erros[0].Code, "entradas em ordem determinística")
	assert.Equal(t, "Dois.", primeiro.Erros[0].Message)
	assert.Equal(t, http.StatusConflict, primeiro.Erros[0].Status)

	segundo := mapa.Erros[1]
	assert.Equal(t, "fake_b", segundo.Subdominio)
	require.Len(t, segundo.Erros, 1, "código repetido no catálogo = UMA entrada")

	corpo, err := json.Marshal(mapa)
	require.NoError(t, err)
	for _, chave := range []string{`"erros"`, `"dominio"`, `"subdominio"`, `"code"`, `"message"`, `"status"`} {
		assert.Contains(t, string(corpo), chave, "contrato do doc 04 exige %s no corpo", chave)
	}
}

func TestMapaDeErrosVazioSerializaComoLista(t *testing.T) {
	rest_err.ResetarRegistroParaTeste()
	defer rest_err.ResetarRegistroParaTeste()

	mapa := NewService(provedorFalso{}).MapaDeErros()
	corpo, err := json.Marshal(mapa)
	require.NoError(t, err)
	assert.JSONEq(t, `{"erros":[]}`, string(corpo), "registro vazio = lista vazia, nunca null")
}
