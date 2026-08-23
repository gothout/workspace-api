package logs

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"workspace-api/internal/infra/clickhouse"
	"workspace-api/internal/pkg/errobserve"
	"workspace-api/internal/pkg/log/access_log"
	"workspace-api/internal/pkg/log/audit_log"
	"workspace-api/internal/pkg/orgctx"
	"workspace-api/internal/pkg/pagination"
)

// --- Dublês -----------------------------------------------------------------

type consultorFake struct {
	filtros []clickhouse.FiltroTrilha
	itens   int
	err     error
}

func (f *consultorFake) Auditoria(_ context.Context, filtro clickhouse.FiltroTrilha) ([]audit_log.Evento, int64, error) {
	f.filtros = append(f.filtros, filtro)
	return f.respostaAuditoria()
}

func (f *consultorFake) Acesso(_ context.Context, filtro clickhouse.FiltroTrilha) ([]access_log.Evento, int64, error) {
	f.filtros = append(f.filtros, filtro)
	if f.err != nil {
		return nil, 0, f.err
	}
	return []access_log.Evento{{RayTrace: "ray"}}, 1, nil
}

func (f *consultorFake) Erros(_ context.Context, filtro clickhouse.FiltroTrilha) ([]errobserve.Evento, int64, error) {
	f.filtros = append(f.filtros, filtro)
	if f.err != nil {
		return nil, 0, f.err
	}
	return []errobserve.Evento{{Codigo: "identidade.workspace.slug_em_uso"}}, 1, nil
}

func (f *consultorFake) respostaAuditoria() ([]audit_log.Evento, int64, error) {
	if f.err != nil {
		return nil, 0, f.err
	}
	itens := make([]audit_log.Evento, f.itens)
	for i := range itens {
		itens[i] = audit_log.Evento{Dominio: "identidade", Subdominio: "workspace", Acao: "criar", Sucesso: true}
	}
	return itens, int64(f.itens), nil
}

// --- Identificadores e ctxs de teste ---------------------------------------

var (
	orgA    = uuid.MustParse("aaaaaaaa-0000-4000-8000-00000000aa01")
	orgB    = uuid.MustParse("bbbbbbbb-0000-4000-8000-00000000bb02")
	wsA1    = uuid.MustParse("cccccccc-0000-4000-8000-00000000cca1")
	wsA2    = uuid.MustParse("cccccccc-0000-4000-8000-00000000cca2")
	usuario = uuid.MustParse("dddddddd-0000-4000-8000-00000000dd01")
)

const (
	permsSoWorkspace    = "perms_so_workspace"
	permsDeOrganization = "perms_de_organization"
	permsDePlataforma   = "perms_de_plataforma"
)

func ctxComRecorte(recorte string) context.Context {
	ctx := context.Background()
	ctx = orgctx.WithUser(ctx, usuario)
	switch recorte {
	case permsDePlataforma:
		// Plataforma resolve workspace como qualquer outro, mas o curinga
		// global a libera dos recortes.
		ctx = orgctx.WithOrganization(ctx, orgA)
		ctx = orgctx.WithWorkspace(ctx, wsA1)
		return orgctx.WithPermissoes(ctx, []string{"*:*"})
	case permsDeOrganization:
		ctx = orgctx.WithOrganization(ctx, orgA)
		ctx = orgctx.WithWorkspace(ctx, wsA1)
		return orgctx.WithPermissoes(ctx, []string{PermLer, PermLerOrganization})
	default:
		ctx = orgctx.WithOrganization(ctx, orgA)
		ctx = orgctx.WithWorkspace(ctx, wsA1)
		return orgctx.WithPermissoes(ctx, []string{PermLer})
	}
}

var paginaPadrao = pagination.Pagination{Page: 1, PageSize: 10}

// --- Recortes de escopo (os três níveis + fail-closed) ----------------------

func TestRecortesDeEscopoImpostosPeloCtx(t *testing.T) {
	testes := []struct {
		nome        string
		recorte     string
		filtro      LogsFiltroRequestDto // filtros INFORMADOS pelo cliente
		orgEsperada string               // organization que chega ao consultor
		wsEsperado  string               // workspace que chega ao consultor ("" = sem filtro)
		querEscopo  bool                 // espera ErrForaDoEscopo SEM tocar o consultor
	}{
		{
			nome:        "workspace: escopo do ctx imposto mesmo sem pedir",
			recorte:     permsSoWorkspace,
			filtro:      LogsFiltroRequestDto{},
			orgEsperada: orgA.String(), wsEsperado: wsA1.String(),
		},
		{
			nome:       "workspace: irmão da mesma org é FORA do recorte (404)",
			recorte:    permsSoWorkspace,
			filtro:     LogsFiltroRequestDto{WorkspaceUUID: wsA2.String()},
			querEscopo: true,
		},
		{
			nome:       "workspace: organization alheia é FORA do recorte (404)",
			recorte:    permsSoWorkspace,
			filtro:     LogsFiltroRequestDto{OrganizationUUID: orgB.String()},
			querEscopo: true,
		},
		{
			nome:        "workspace: pedindo o próprio workspace passa com escopo reforçado",
			recorte:     permsSoWorkspace,
			filtro:      LogsFiltroRequestDto{WorkspaceUUID: wsA1.String()},
			orgEsperada: orgA.String(), wsEsperado: wsA1.String(),
		},
		{
			nome:        "organization: lê a própria inteira (todos os workspaces)",
			recorte:     permsDeOrganization,
			filtro:      LogsFiltroRequestDto{},
			orgEsperada: orgA.String(), wsEsperado: "",
		},
		{
			nome:       "organization: workspace alheio na consulta vaza para o consultor? NÃO — org alheia é 404",
			recorte:    permsDeOrganization,
			filtro:     LogsFiltroRequestDto{OrganizationUUID: orgB.String()},
			querEscopo: true,
		},
		{
			nome:        "plataforma: sem escopo imposto, filtro honrado como veio",
			recorte:     permsDePlataforma,
			filtro:      LogsFiltroRequestDto{OrganizationUUID: orgB.String()},
			orgEsperada: orgB.String(), wsEsperado: "",
		},
		{
			nome:        "plataforma: sem filtro nenhum, consulta aberta",
			recorte:     permsDePlataforma,
			filtro:      LogsFiltroRequestDto{},
			orgEsperada: "", wsEsperado: "",
		},
	}

	for _, tt := range testes {
		t.Run(tt.nome, func(t *testing.T) {
			fake := &consultorFake{}
			svc := NewService(fake)
			filtro, err := tt.filtro.ParaFiltro()
			require.NoError(t, err)

			resp, err := svc.Auditoria(ctxComRecorte(tt.recorte), filtro, paginaPadrao)
			if tt.querEscopo {
				require.ErrorIs(t, err, ErrForaDoEscopo)
				assert.Empty(t, fake.filtros, "recusa NÃO consulta as trilhas")
				return
			}
			require.NoError(t, err)
			require.Len(t, fake.filtros, 1)
			assert.Equal(t, tt.orgEsperada, fake.filtros[0].OrganizationUUID)
			assert.Equal(t, tt.wsEsperado, fake.filtros[0].WorkspaceUUID)
			assert.EqualValues(t, 0, fake.filtros[0].Offset)
			assert.Equal(t, paginaPadrao.Limit(), fake.filtros[0].Limite)
			assert.NotNil(t, resp.Items)
		})
	}
}

// TestFailClosedSemEscopoNoCtx: chamador sem organization/workspace resolvidos
// não lê nada — nem plataforma falsificada sem permissão global.
func TestFailClosedSemEscopoNoCtx(t *testing.T) {
	fake := &consultorFake{}
	svc := NewService(fake)

	semNada := orgctx.WithPermissoes(context.Background(), []string{PermLer})
	_, err := svc.Auditoria(semNada, clickhouse.FiltroTrilha{}, paginaPadrao)
	require.ErrorIs(t, err, ErrForaDoEscopo)

	// Workspace exigido pelo recorte básico ausente (org resolvida, ws não).
	semWs := orgctx.WithOrganization(
		orgctx.WithPermissoes(context.Background(), []string{PermLer}), orgA)
	_, err = svc.Acesso(semWs, clickhouse.FiltroTrilha{}, paginaPadrao)
	require.ErrorIs(t, err, ErrForaDoEscopo)

	_, err = svc.Erros(semWs, clickhouse.FiltroTrilha{}, paginaPadrao)
	require.ErrorIs(t, err, ErrForaDoEscopo)
	assert.Empty(t, fake.filtros, "nenhuma recusa alcança as trilhas")
}

// TestFiltrosInvalidos: uuid/timestamp malformados e janela invertida = 400.
func TestFiltrosInvalidos(t *testing.T) {
	testes := []LogsFiltroRequestDto{
		{OrganizationUUID: "nao-e-uuid"},
		{WorkspaceUUID: "tambem-nao"},
		{Inicio: "ontem"},
		{Fim: "2026-13-45T99:00:00Z"},
		{Inicio: "2026-08-23T10:00:00Z", Fim: "2026-08-23T09:00:00Z"},
	}
	for _, dto := range testes {
		_, err := dto.ParaFiltro()
		require.ErrorIs(t, err, ErrFiltroInvalido, "dto %+v deveria recusar", dto)
	}

	bom := LogsFiltroRequestDto{
		Acao: "criar", RayTrace: "ray-01",
		Inicio: "2026-08-23T08:00:00Z", Fim: "2026-08-23T12:00:00Z",
	}
	filtro, err := bom.ParaFiltro()
	require.NoError(t, err)
	assert.False(t, filtro.InstanteInicio.IsZero())
	assert.False(t, filtro.InstanteFim.IsZero())
	assert.Equal(t, "criar", filtro.Acao)
}

// TestPaginacaoPropagada: offset/limit saem do pacote padrão (teto 100).
func TestPaginacaoPropagada(t *testing.T) {
	fake := &consultorFake{itens: 3}
	svc := NewService(fake)
	p := pagination.Pagination{Page: 3, PageSize: 500} // teto clamp para 100

	resp, err := svc.Auditoria(ctxComRecorte(permsDeOrganization), clickhouse.FiltroTrilha{}, p)
	require.NoError(t, err)
	require.Len(t, fake.filtros, 1)
	assert.Equal(t, (3-1)*100, fake.filtros[0].Offset)
	assert.Equal(t, 100, fake.filtros[0].Limite)
	assert.EqualValues(t, 3, resp.Total, "total vem SEM a página")
	assert.Len(t, resp.Items, 3)
}

// TestDegradaçãoSurfaComoIndisponivel: falha do consultor sobe intacta — o
// adaptador real devolve ErrIndisponivel quando o ClickHouse está ausente.
func TestDegradacaoSurfaComoIndisponivel(t *testing.T) {
	fake := &consultorFake{err: ErrIndisponivel}
	svc := NewService(fake)

	_, err := svc.Erros(ctxComRecorte(permsDePlataforma), clickhouse.FiltroTrilha{}, paginaPadrao)
	require.ErrorIs(t, err, ErrIndisponivel)
}

// TestTraducaoParaRespostaHttp: cada sentinela vira o status do catálogo.
func TestTraducaoParaRespostaHttp(t *testing.T) {
	require.Equal(t, 400, traduzir(ErrFiltroInvalido).Status)
	require.Equal(t, 404, traduzir(ErrForaDoEscopo).Status)
	require.Equal(t, 503, traduzir(ErrIndisponivel).Status)
	require.Equal(t, 500, traduzir(errors.New("qualquer")).Status)

	catalogada := traduzir(ErrForaDoEscopo)
	assert.Equal(t, "identidade.logs.fora_do_escopo", catalogada.Code)
}

// TestObservadorCobreTodoOCatalogo: sentinela nova sem severidade reprova no
// boot — aqui só conferimos o vocabulário observável da aplicação.
func TestObservadorCobreTodoOCatalogo(t *testing.T) {
	metas := observadorErros.Catalogo()
	codigos := map[string]bool{}
	for _, m := range metas {
		codigos[m.Codigo] = true
	}
	for _, entrada := range errorCatalog {
		assert.True(t, codigos[entrada.Codigo], "código %s fora do observador", entrada.Codigo)
	}
}
