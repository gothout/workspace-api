package bootstrap

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dominioUsuario "workspace-api/internal/identidade/domain/user"
	modeluser "workspace-api/internal/identidade/model/user"
	"workspace-api/internal/middleware"
)

// TestListarPapeisSobreEsquemaReal (issue #28): a listagem de papéis globais
// devolve EXATAMENTE os 5 papéis seed com uuid, nome e descrição — sobre o
// esquema migrado e semeado, via CONSTRUTORES PUROS (nunca o singleton do
// processo). A consulta é GLOBAL por natureza: ctx sem escopo funciona
// (exceção documentada das tabelas de papéis em agents/03).
func TestListarPapeisSobreEsquemaReal(t *testing.T) {
	if !dockerDisponivel(t) {
		t.Skip("docker indisponível — teste de integração pulado")
	}
	amb := subirAmbiente(t)
	require.NoError(t, semearPapeis(amb.ctx, amb.db))

	atrib := dominioUsuario.NewRepositorioAtribuicoes(amb.db)
	papeis, err := atrib.ListarPapeis(amb.ctx)
	require.NoError(t, err)
	require.Len(t, papeis, 5, "os 5 papéis seed devem ser listados")

	nomes := make(map[string]modeluser.Papel, len(papeis))
	for _, p := range papeis {
		require.NotEqual(t, uuid.Nil, p.UUID, "papel %s deve ter uuid", p.Nome)
		require.NotEmpty(t, p.Descricao, "papel %s deve ter descrição", p.Nome)
		nomes[p.Nome] = p
	}
	for _, esperado := range []string{papelSuperAdmin, papelAdminOrganization,
		papelAdminWorkspace, papelUsuarioWorkspace, papelSomenteLeitura} {
		p, ok := nomes[esperado]
		require.True(t, ok, "papel seed %s ausente na listagem", esperado)
		assert.Equal(t, esperado, p.Nome)
	}

	// Ordenado por nome — saída determinística para o Select do painel.
	for i := 1; i < len(papeis); i++ {
		require.Less(t, papeis[i-1].Nome, papeis[i].Nome,
			"listagem deve vir ordenada por nome")
	}

	// Pelo SERVICE (mesmo caminho da rota): delega ao repositório.
	svc := dominioUsuario.NewService(
		dominioUsuario.NewRepository(amb.db), atrib,
		validadorSemprePertence{}, dominioUsuario.NovasCredenciaisBcrypt())
	peloServico, err := svc.Papeis(context.Background())
	require.NoError(t, err)
	assert.Len(t, peloServico, 5)

	// DTO expõe exatamente o trio do contrato (#28): uuid, nome, descricao.
	dtos := dominioUsuario.NovosPapeisResponseDto(peloServico)
	require.Len(t, dtos, 5)
	for _, dto := range dtos {
		assert.NotEmpty(t, dto.Nome)
		assert.NotEmpty(t, dto.Descricao)
	}
}

// TestSeedCobreRotaDePapeis: a rota nova exige identidade:user:atribuir_papel
// — o seed concede a permissão aos administradores PELO CURINGA
// identidade:user:* (super_admin passa pelo *:*). Divergência seed × rota é
// bug de contrato; usuario_workspace e somente_leitura NÃO atribuem papel e
// não precisam do Select.
func TestSeedCobreRotaDePapeis(t *testing.T) {
	exigida := dominioUsuario.PermAtribuirPapel

	concedidos := map[string][]string{
		papelSuperAdmin:        {"*:*"},
		papelAdminOrganization: {"identidade:user:*"},
		papelAdminWorkspace:    {"identidade:user:*"},
		papelUsuarioWorkspace:  {},
		papelSomenteLeitura:    {},
	}
	for _, papel := range papeisSeed {
		efetivas := concedidos[papel.nome]
		deveAtender := papel.nome == papelSuperAdmin ||
			papel.nome == papelAdminOrganization || papel.nome == papelAdminWorkspace
		assert.Equal(t, deveAtender, middleware.Atende(efetivas, exigida),
			"papel %s: concessão da rota de papéis diverge do desenho", papel.nome)
	}

	// E o catálogo do subdomínio carrega a permissão exigida pela rota —
	// sem ela o endpoint não apareceria na árvore de permissões do front.
	catalogada := false
	for _, meta := range dominioUsuario.Catalogo() {
		if meta.Permissao == exigida {
			catalogada = true
		}
	}
	assert.True(t, catalogada, "%s deve estar no Catalogo() do user", exigida)
}
