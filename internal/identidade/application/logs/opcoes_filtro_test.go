package logs

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"workspace-api/internal/pkg/orgctx"
)

// --- Dublê do provedor de opções (UX3) ---------------------------------------

type provedorFake struct {
	orgsPedidas  []*uuid.UUID
	wsPedidas    []*uuid.UUID
	usuariosOrg  *uuid.UUID
	usuariosWs   *uuid.UUID
	respostaOrgs []OpcaoFiltro
	err          error
}

func novoProvedorFake() *provedorFake {
	return &provedorFake{respostaOrgs: []OpcaoFiltro{
		{UUID: orgA.String(), Nome: "Org A"},
	}}
}

func (p *provedorFake) Organizacoes(_ context.Context, organizationUUID *uuid.UUID) ([]OpcaoFiltro, error) {
	p.orgsPedidas = append(p.orgsPedidas, organizationUUID)
	if p.err != nil {
		return nil, p.err
	}
	return p.respostaOrgs, nil
}

func (p *provedorFake) Workspaces(_ context.Context, organizationUUID *uuid.UUID) ([]OpcaoFiltro, error) {
	p.wsPedidas = append(p.wsPedidas, organizationUUID)
	return []OpcaoFiltro{{UUID: wsA1.String(), Nome: "Workspace A1"}}, nil
}

func (p *provedorFake) Usuarios(_ context.Context, organizationUUID, workspaceUUID *uuid.UUID) ([]OpcaoFiltro, error) {
	p.usuariosOrg, p.usuariosWs = organizationUUID, workspaceUUID
	return []OpcaoFiltro{}, nil
}

func ponteiro(id uuid.UUID) *uuid.UUID { return &id }

// Os TRÊS recortes chegam ao provedor com a granularidade certa e o chamador
// sem escopo derivável recebe ErrForaDoEscopo antes de tocar o provedor.
func TestOpcoesFiltroRecortes(t *testing.T) {
	testes := []struct {
		nome        string
		recorte     string
		orgEsperada *uuid.UUID // ponteiro esperado em Organizacoes
		wsEsperado  *uuid.UUID // ponteiro esperado em Workspaces
		userOrg     *uuid.UUID // ponteiro esperado em Usuarios (org)
		userWs      *uuid.UUID // ponteiro esperado em Usuarios (workspace)
	}{
		{
			nome:        "plataforma: listas completas (ponteiro nil)",
			recorte:     permsDePlataforma,
			orgEsperada: nil, wsEsperado: nil, userOrg: nil, userWs: nil,
		},
		{
			nome:        "organization: preso à própria",
			recorte:     permsDeOrganization,
			orgEsperada: ponteiro(orgA), wsEsperado: ponteiro(orgA), userOrg: ponteiro(orgA), userWs: nil,
		},
		{
			nome:        "workspace: só o próprio recorte",
			recorte:     permsSoWorkspace,
			orgEsperada: ponteiro(orgA), wsEsperado: ponteiro(orgA), userOrg: ponteiro(orgA), userWs: ponteiro(wsA1),
		},
	}

	for _, tt := range testes {
		t.Run(tt.nome, func(t *testing.T) {
			provedor := novoProvedorFake()
			svc := NewService(&consultorFake{}, ComProvedorOpcoes(provedor))

			resp, err := svc.OpcoesFiltro(ctxComRecorte(tt.recorte))
			require.NoError(t, err)
			require.Len(t, provedor.orgsPedidas, 1)
			assert.Equal(t, tt.orgEsperada, provedor.orgsPedidas[0])
			assert.Equal(t, tt.wsEsperado, provedor.wsPedidas[0])
			assert.Equal(t, tt.userOrg, provedor.usuariosOrg)
			assert.Equal(t, tt.userWs, provedor.usuariosWs)

			// Listas vazias saem como [] (nunca null).
			require.NotNil(t, resp.Usuarios)
			assert.NotEmpty(t, resp.Organizacoes)
			assert.Equal(t, orgA.String(), resp.Organizacoes[0].UUID)
			assert.Equal(t, "Org A", resp.Organizacoes[0].Nome)
		})
	}
}

// Fail-closed: chamador sem escopo derivável não consulta o provedor.
func TestOpcoesFiltroFailClosed(t *testing.T) {
	provedor := novoProvedorFake()
	svc := NewService(&consultorFake{}, ComProvedorOpcoes(provedor))

	semNada := orgctx.WithPermissoes(context.Background(), []string{PermLer})
	_, err := svc.OpcoesFiltro(semNada)
	require.ErrorIs(t, err, ErrForaDoEscopo)
	assert.Empty(t, provedor.orgsPedidas, "recusa NÃO toca o provedor")
}

// Falha do provedor sobe intacta (traduzir → 500 genérico com ray_trace).
func TestOpcoesFiltroFalhaDoProvedor(t *testing.T) {
	provedor := novoProvedorFake()
	provedor.err = assert.AnError
	svc := NewService(&consultorFake{}, ComProvedorOpcoes(provedor))

	_, err := svc.OpcoesFiltro(ctxComRecorte(permsDeOrganization))
	require.ErrorIs(t, err, assert.AnError)
}
