package user

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	modeluser "workspace-api/internal/identidade/model/user"
)

// TestPapeisListaCatalogoGlobal prova a listagem dos papéis globais (issue
// #28): o service devolve o catálogo inteiro SEM escopo — as tabelas de
// papéis são globais da plataforma (exceção documentada em agents/03 e no
// AGENTS.md do pacote) — e o DTO expõe exatamente uuid/nome/descricao.
func TestPapeisListaCatalogoGlobal(t *testing.T) {
	svc, _, atr, _ := montarServico(t, validadorFake{pertence: true})

	super := uuid.MustParse("11111111-0000-4000-8000-00000000ba01")
	leitor := uuid.MustParse("22222222-0000-4000-8000-00000000ba02")
	atr.seedCatalogo(
		modeluser.Papel{UUID: super, Nome: "super_admin", Descricao: "Admin da plataforma."},
		modeluser.Papel{UUID: leitor, Nome: "somente_leitura", Descricao: "Consulta sem escrita."},
	)

	papeis, err := svc.Papeis(context.Background()) // ctx CRU: consulta global não escopa
	require.NoError(t, err)
	require.Len(t, papeis, 2)

	dtos := NovosPapeisResponseDto(papeis)
	require.Len(t, dtos, 2)

	// Ordenado por nome — saída determinística para o painel.
	assert.Equal(t, "somente_leitura", dtos[0].Nome)
	assert.Equal(t, leitor, dtos[0].UUID)
	assert.Equal(t, "Consulta sem escrita.", dtos[0].Descricao)
	assert.Equal(t, "super_admin", dtos[1].Nome)
	assert.Equal(t, super, dtos[1].UUID)
	assert.Equal(t, "Admin da plataforma.", dtos[1].Descricao)
}

// Catálogo vazio (banco recém-migrado sem seed) devolve lista VAZIA com
// envelope JSON válido ([]), nunca null.
func TestPapeisCatalogoVazioDevolveListaVazia(t *testing.T) {
	svc, _, _, _ := montarServico(t, validadorFake{pertence: true})

	papeis, err := svc.Papeis(context.Background())
	require.NoError(t, err)
	require.NotNil(t, papeis)
	assert.Empty(t, papeis)

	dtos := NovosPapeisResponseDto(papeis)
	require.NotNil(t, dtos)
	assert.Empty(t, dtos)
}

// A permissão da rota nova é PermAtribuirPapel: quem atribui papel precisa da
// referência — e a rota entra no Catalogo() como par rota+método (contrato do
// endpoint de permissões).
func TestCatalogoExpoeRotaDeListagemDePapeis(t *testing.T) {
	encontrada := false
	for _, meta := range Catalogo() {
		if meta.Permissao != PermAtribuirPapel {
			continue
		}
		for _, rota := range meta.Rotas {
			if rota.Rota == prefixoPublicoPapeis && rota.Metodo == "GET" {
				encontrada = true
			}
		}
	}
	assert.True(t, encontrada,
		"PermAtribuirPapel deve expor GET %s no Catalogo()", prefixoPublicoPapeis)
}
