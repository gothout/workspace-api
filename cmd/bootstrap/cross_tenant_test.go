package bootstrap

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dominioOrganizacao "workspace-api/internal/identidade/domain/organization"
	dominioWorkspace "workspace-api/internal/identidade/domain/workspace"
	orgmodel "workspace-api/internal/identidade/model/organization"
	modelworkspace "workspace-api/internal/identidade/model/workspace"
	"workspace-api/internal/pkg/log/audit_log"
	"workspace-api/internal/pkg/orgctx"
)

// --- Gestão cross-tenant (UX4) sobre o esquema migrado, FUNÇÕES PURAS -------

// estadoOrganizacaoPuro espelha o adaptador de boot (estadoOrganizacaoAlvo)
// sem o singleton: resolve a face do irmão organization por CONSTRUTOR PURO
// (mesma regra do provisionamento da R3).
type estadoOrganizacaoPuro struct{ svc dominioOrganizacao.Service }

func (e estadoOrganizacaoPuro) Estado(ctx context.Context, organizationUUID uuid.UUID) (bool, bool, error) {
	o, err := e.svc.Read(orgctx.WithOrganization(ctx, organizationUUID), organizationUUID)
	if err != nil {
		if errors.Is(err, dominioOrganizacao.ErrNotFound) {
			return false, false, nil
		}
		return false, false, err
	}
	return true, o.Status == orgmodel.StatusAtivo, nil
}

// trilhaCaptura guarda os eventos de auditoria emitidos para conferir o
// payload montado à mão (doc 04) da criação cross-tenant.
type trilhaCaptura struct {
	mu      sync.Mutex
	eventos []audit_log.Evento
}

func (t *trilhaCaptura) Registrar(ev audit_log.Evento) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.eventos = append(t.eventos, ev)
}

func TestGestaoCrossTenantDaPlataforma(t *testing.T) {
	if !dockerDisponivel(t) {
		t.Skip("docker indisponível — teste de integração pulado")
	}
	amb := subirAmbiente(t)

	orgRepo := dominioOrganizacao.NewRepository(amb.db)
	orgSvc := dominioOrganizacao.NewService(orgRepo, nil, nil, nil, nil)
	trilha := &trilhaCaptura{}
	wsSvc := dominioWorkspace.NewService(dominioWorkspace.NewRepository(amb.db), nil,
		dominioWorkspace.ComEstadoOrganizacao(estadoOrganizacaoPuro{svc: orgSvc}),
		dominioWorkspace.ComTrilha(trilha))

	novaOrg := func(nome string, status orgmodel.Status) *orgmodel.Organization {
		o, err := orgmodel.NewOrganization(orgmodel.CreateInput{Nome: nome})
		require.NoError(t, err)
		require.NoError(t, orgRepo.Criar(amb.ctx, o))
		if status == orgmodel.StatusInativo {
			require.NoError(t, amb.db.Exec(`UPDATE identidade_organization_organization SET status = 'inativo' WHERE uuid = ?`, o.UUID).Error)
		}
		return o
	}
	ctxFundo := amb.ctx
	ctxPlataforma := func() context.Context {
		return orgctx.WithPermissoes(ctxFundo, []string{"*:*"})
	}
	filtro := func(org uuid.UUID) modelworkspace.ListFilter {
		return modelworkspace.ListFilter{OrganizationUUID: &org}
	}

	orgNova := novaOrg("Recém Contratada", orgmodel.StatusAtivo)
	orgMorta := novaOrg("Fora do Ar", orgmodel.StatusInativo)
	orgAdmin := novaOrg("Dono Próprio", orgmodel.StatusAtivo)
	ctxAdminB := func() context.Context {
		return orgctx.WithPermissoes(orgctx.WithOrganization(ctxFundo, orgAdmin.UUID),
			[]string{"identidade:workspace:*", "identidade:user:*", "identidade:catalogo:ler"})
	}

	// --- CRIAÇÃO cross-tenant --------------------------------------------------

	// Plataforma cria o PRIMEIRO workspace da organization nova — sem estar
	// dentro dela (console master não resolve workspace nenhum).
	pedida := orgNova.UUID
	w1, err := wsSvc.Create(ctxPlataforma(), modelworkspace.CreateInput{
		Nome: "Primeiro", Slug: "recem-primeiro", OrganizationPedida: &pedida,
	})
	require.NoError(t, err)
	assert.Equal(t, orgNova.UUID, w1.OrganizationUUID, "workspace nasce na organization PEDIDA")

	// Auditoria da criação cross-tenant: payload montado à mão com a flag.
	trilha.mu.Lock()
	var eventoCriar *audit_log.Evento
	for i, ev := range trilha.eventos {
		if ev.Acao == "criar" && ev.WorkspaceUUID == w1.UUID.String() {
			eventoCriar = &trilha.eventos[i]
		}
	}
	trilha.mu.Unlock()
	require.NotNil(t, eventoCriar, "escrita cross-tenant AUDITADA")
	assert.Equal(t, "true", eventoCriar.Detalhes["cross_tenant"])
	assert.Equal(t, orgNova.UUID.String(), eventoCriar.OrganizationUUID)

	// Segundo workspace na MESMA organization pela plataforma segue válido
	// (slug global é o limite, não a quantidade).
	pedida2 := orgNova.UUID
	_, err = wsSvc.Create(ctxPlataforma(), modelworkspace.CreateInput{
		Nome: "Segundo", Slug: "recem-segundo", OrganizationPedida: &pedida2,
	})
	require.NoError(t, err)

	// Organization inexistente → 404; inativa → 422 (filho nunca mais vivo
	// que o pai); nada persiste em nenhum dos casos.
	totalAntes := len(trilha.eventos)
	fantasma := uuid.New()
	_, err = wsSvc.Create(ctxPlataforma(), modelworkspace.CreateInput{
		Nome: "Fantasma", Slug: "org-fantasma", OrganizationPedida: &fantasma,
	})
	assert.ErrorIs(t, err, dominioWorkspace.ErrOrganizacaoNaoEncontrada)

	pedidaMorta := orgMorta.UUID
	_, err = wsSvc.Create(ctxPlataforma(), modelworkspace.CreateInput{
		Nome: "Sob Morta", Slug: "sob-morta", OrganizationPedida: &pedidaMorta,
	})
	assert.ErrorIs(t, err, dominioWorkspace.ErrOrganizacaoInativa)
	trilha.mu.Lock()
	assert.Len(t, trilha.eventos, totalAntes, "recusas NUNCA auditam sucesso nem persistem")
	trilha.mu.Unlock()

	// Não-plataforma apontando organization alheia: fora_do_escopo, mesmo com
	// permissão plena de workspace no próprio recorte.
	pedidaAlheia := orgNova.UUID
	_, err = wsSvc.Create(ctxAdminB(), modelworkspace.CreateInput{
		Nome: "Invasão", Slug: "invasao-alheia", OrganizationPedida: &pedidaAlheia,
	})
	assert.ErrorIs(t, err, dominioWorkspace.ErrForaDoEscopo)

	// Apontando a PRÓPRIA organization é aceito (equivalente ao escopo).
	pedidaPropria := orgAdmin.UUID
	proprio, err := wsSvc.Create(ctxAdminB(), modelworkspace.CreateInput{
		Nome: "Própria", Slug: "propria-explicita", OrganizationPedida: &pedidaPropria,
	})
	require.NoError(t, err)
	assert.Equal(t, orgAdmin.UUID, proprio.OrganizationUUID)

	// --- LISTAGEM pelo filtro organization_uuid --------------------------------

	// Plataforma (sem escopo no ctx) lista qualquer organization pelo filtro.
	itens, total, err := wsSvc.List(ctxPlataforma(), filtro(orgNova.UUID))
	require.NoError(t, err)
	assert.EqualValues(t, 2, total)
	for _, w := range itens {
		assert.Equal(t, orgNova.UUID, w.OrganizationUUID)
	}

	// Sem filtro e sem escopo, plataforma segue fail-closed (comportamento atual).
	_, _, err = wsSvc.List(ctxPlataforma(), modelworkspace.ListFilter{})
	assert.ErrorIs(t, err, orgctx.ErrEscopoAusente)

	// Organization vê só a própria; alheia (exista ou não) vira fora_do_escopo.
	_, _, err = wsSvc.List(ctxAdminB(), filtro(orgNova.UUID))
	assert.ErrorIs(t, err, dominioWorkspace.ErrForaDoEscopo)
	_, _, err = wsSvc.List(ctxAdminB(), filtro(uuid.New()))
	assert.ErrorIs(t, err, dominioWorkspace.ErrForaDoEscopo)
	itens, total, err = wsSvc.List(ctxAdminB(), filtro(orgAdmin.UUID))
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	assert.Equal(t, proprio.UUID, itens[0].UUID)

	// Isolamento intacto: leitura direta do workspace alheio continua 404.
	_, err = wsSvc.Read(ctxAdminB(), w1.UUID)
	assert.ErrorIs(t, err, dominioWorkspace.ErrNotFound, "cross-tenant não abre a porta do Read escopado")
}
