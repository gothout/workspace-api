package bootstrap

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	aplicacaoprovisionamento "workspace-api/internal/identidade/application/provisionamento"
	dominioOrganizacao "workspace-api/internal/identidade/domain/organization"
	dominioUsuario "workspace-api/internal/identidade/domain/user"
	dominioWorkspace "workspace-api/internal/identidade/domain/workspace"
	orgmodel "workspace-api/internal/identidade/model/organization"
	modeluser "workspace-api/internal/identidade/model/user"
	modelworkspace "workspace-api/internal/identidade/model/workspace"
	"workspace-api/internal/pkg/orgctx"
)

// --- Provisionamento por endpoint (UX5) sobre o esquema migrado --------------

// Dobras locais dos contratos sobre os SERVICES PUROS dos subdomínios —
// espelham os adaptadores do boot (provisionamento_app.go) sem singletons
// (mesma regra dos demais testes de integração deste pacote).
type workspacesProvisionamentoLocal struct{ svc dominioWorkspace.Service }

func (w workspacesProvisionamentoLocal) TemWorkspaces(ctx context.Context, organizationUUID uuid.UUID) (bool, error) {
	_, total, err := w.svc.List(orgctx.WithOrganization(ctx, organizationUUID), modelworkspace.ListFilter{})
	return total > 0, err
}

func (w workspacesProvisionamentoLocal) Criar(ctx context.Context, organizationUUID uuid.UUID, slug string) (uuid.UUID, error) {
	pedida := organizationUUID
	ws, err := w.svc.Create(ctx, modelworkspace.CreateInput{OrganizationPedida: &pedida, Nome: slug, Slug: slug})
	if err != nil {
		switch {
		case errors.Is(err, dominioWorkspace.ErrSlugEmUso):
			return uuid.Nil, aplicacaoprovisionamento.ErrSlugIndisponivel
		case errors.Is(err, dominioWorkspace.ErrForaDoEscopo):
			return uuid.Nil, aplicacaoprovisionamento.ErrSemPoderPlataforma
		}
		return uuid.Nil, err
	}
	return ws.UUID, nil
}

type usuariosProvisionamentoLocal struct {
	svc  dominioUsuario.Service
	repo dominioUsuario.Repository
}

func (u usuariosProvisionamentoLocal) UUIDPorEmail(ctx context.Context, email string) (uuid.UUID, bool, error) {
	pessoa, err := u.repo.BuscarPorEmail(ctx, modeluser.Email(email))
	if err != nil {
		if errors.Is(err, dominioUsuario.ErrNotFound) {
			return uuid.Nil, false, nil
		}
		return uuid.Nil, false, err
	}
	return pessoa.UUID, true, nil
}

func (u usuariosProvisionamentoLocal) CriarAdmin(ctx context.Context, nome, email, senha string) (uuid.UUID, error) {
	pessoa, err := u.svc.Create(ctx, dominioUsuario.EntradaCriacao{
		Dados: modeluser.CreateInput{Nome: nome, Email: email}, Senha: senha,
	})
	if err != nil {
		if errors.Is(err, dominioUsuario.ErrEmailEmUso) {
			return uuid.Nil, aplicacaoprovisionamento.ErrEmailEmUso
		}
		return uuid.Nil, err
	}
	return pessoa.UUID, nil
}

func (u usuariosProvisionamentoLocal) AtribuirPapel(ctx context.Context, usuarioUUID, workspaceUUID, papelUUID uuid.UUID) error {
	_, err := u.svc.AtribuirPapel(ctx, usuarioUUID, workspaceUUID, papelUUID)
	if err != nil && errors.Is(err, dominioUsuario.ErrAtribuicaoDuplicada) {
		return aplicacaoprovisionamento.ErrAtribuicaoExistente
	}
	return err
}

type papeisProvisionamentoLocal struct{ svc dominioUsuario.Service }

func (p papeisProvisionamentoLocal) PorNome(ctx context.Context, nome string) (uuid.UUID, error) {
	lista, err := p.svc.Papeis(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	for _, papel := range lista {
		if papel.Nome == nome {
			return papel.UUID, nil
		}
	}
	return uuid.Nil, aplicacaoprovisionamento.ErrPapelAusente
}

// TestProvisionamentoDeOrganizationPontaAPonta prova o fluxo inteiro da UX5
// com banco REAL: admin criado com bcrypt + login funcionando, workspace
// inicial pertencendo à organization alvo, papel admin_organization atribuído,
// idempotência explícita (409) na repetição e recusas de alvo inválido sem
// criar nada.
func TestProvisionamentoDeOrganizationPontaAPonta(t *testing.T) {
	if !dockerDisponivel(t) {
		t.Skip("docker indisponível — teste de integração pulado")
	}
	amb := subirAmbiente(t)
	require.NoError(t, semearPapeis(amb.ctx, amb.db), "o provisionamento depende do seed dos papéis")

	orgRepo := dominioOrganizacao.NewRepository(amb.db)
	svcOrg := dominioOrganizacao.NewService(orgRepo, nil, nil, nil, nil)
	wsSvc := dominioWorkspace.NewService(dominioWorkspace.NewRepository(amb.db), nil,
		dominioWorkspace.ComEstadoOrganizacao(estadoOrganizacaoPuro{svc: svcOrg}))
	userRepo := dominioUsuario.NewRepository(amb.db)
	svcUser := dominioUsuario.NewService(userRepo, dominioUsuario.NewRepositorioAtribuicoes(amb.db),
		validadorSemprePertence{}, dominioUsuario.NovasCredenciaisBcrypt())

	trilha := &trilhaCaptura{}
	app := aplicacaoprovisionamento.NewService(aplicacaoprovisionamento.Dependencias{
		Organizacoes: estadoOrganizacaoPuro{svc: svcOrg},
		Workspaces:   workspacesProvisionamentoLocal{svc: wsSvc},
		Usuarios:     usuariosProvisionamentoLocal{svc: svcUser, repo: userRepo},
		Papeis:       papeisProvisionamentoLocal{svc: svcUser},
		Trilha:       trilha,
	})

	ctxPlat := func() context.Context { return orgctx.WithPermissoes(amb.ctx, []string{"*:*"}) }
	novaOrg := func(nome string) *orgmodel.Organization {
		o, err := orgmodel.NewOrganization(orgmodel.CreateInput{Nome: nome})
		require.NoError(t, err)
		require.NoError(t, orgRepo.Criar(amb.ctx, o))
		return o
	}

	orgNova := novaOrg("Parceiro Endpoint")

	// --- Caminho feliz --------------------------------------------------------
	resp, err := app.Provisionar(ctxPlat(), orgNova.UUID, aplicacaoprovisionamento.ProvisionamentoRequestDto{
		Nome: "Ana Fundadora", Email: "ana@endpoint.teste", Senha: "senha-segura-123", Slug: "endpoint-principal",
	})
	require.NoError(t, err)
	assert.Equal(t, orgNova.UUID, resp.OrganizationUUID)

	// Workspace inicial pertence à organization ALVO (criação cross-tenant).
	inicial, err := wsSvc.Read(orgctx.WithOrganization(amb.ctx, orgNova.UUID), resp.WorkspaceUUID)
	require.NoError(t, err)
	assert.Equal(t, orgNova.UUID, inicial.OrganizationUUID)
	assert.Equal(t, "endpoint-principal", inicial.Slug.String())

	// Admin existe NA organization e autentica (bcrypt real).
	admin, err := svcUser.Read(orgctx.WithOrganization(amb.ctx, orgNova.UUID), resp.AdminUUID)
	require.NoError(t, err)
	assert.Equal(t, "ana@endpoint.teste", admin.Email.String())
	_, err = svcUser.Autenticar(orgctx.WithOrganization(amb.ctx, orgNova.UUID), "ana@endpoint.teste", "senha-segura-123")
	require.NoError(t, err, "senha escolhida pelo chamador autentica de verdade")

	// Papel admin_organization atribuído ao par (admin × workspace inicial).
	atribuido := false
	atribuicoes, err := svcUser.Atribuicoes(orgctx.WithOrganization(amb.ctx, orgNova.UUID), admin.UUID)
	require.NoError(t, err)
	for _, a := range atribuicoes {
		if a.WorkspaceUUID == inicial.UUID && a.PapelNome == papelAdminOrganization {
			atribuido = true
		}
	}
	assert.True(t, atribuido, "admin nasce com o papel admin_organization no workspace inicial")

	// Auditoria da orquestração com e-mail mascarado.
	require.NotEmpty(t, trilha.eventos)
	achou := false
	for _, evento := range trilha.eventos {
		if evento.Subdominio == "provisionamento" && evento.Acao == "provisionar" &&
			evento.OrganizationUUID == orgNova.UUID.String() {
			achou = true
			assert.Contains(t, evento.Detalhes["email_mascarado"], "***")
			assert.NotContains(t, evento.Detalhes["email_mascarado"], "ana@endpoint")
		}
	}
	assert.True(t, achou, "orquestração auditada como evento próprio")

	// --- Idempotência EXPLÍCITA: repetir é 409 ja_provisionado ----------------
	_, err = app.Provisionar(ctxPlat(), orgNova.UUID, aplicacaoprovisionamento.ProvisionamentoRequestDto{
		Nome: "Outra", Email: "outra@endpoint.teste", Senha: "senha-segura-456", Slug: "outro-slug",
	})
	assert.ErrorIs(t, err, aplicacaoprovisionamento.ErrJaProvisionado)

	// --- Recusas de alvo inválido SEM criar nada ------------------------------
	_, err = app.Provisionar(ctxPlat(), uuid.New(), aplicacaoprovisionamento.ProvisionamentoRequestDto{
		Nome: "Fantasma", Email: "fantasma@endpoint.teste", Senha: "senha-segura-789", Slug: "org-fantasma",
	})
	assert.ErrorIs(t, err, aplicacaoprovisionamento.ErrOrganizacaoNaoEncontrada)

	orgMorta := novaOrg("Parceiro Inativo")
	require.NoError(t, amb.db.Exec(`UPDATE identidade_organization_organization SET status = 'inativo' WHERE uuid = ?`, orgMorta.UUID).Error)
	_, err = app.Provisionar(ctxPlat(), orgMorta.UUID, aplicacaoprovisionamento.ProvisionamentoRequestDto{
		Nome: "Morta", Email: "morta@endpoint.teste", Senha: "senha-segura-999", Slug: "parceiro-morto",
	})
	assert.ErrorIs(t, err, aplicacaoprovisionamento.ErrOrganizacaoInativa)

	// Slug globalmente tomado por OUTRA organization → conflito mapeado.
	orgTomada := novaOrg("Parceiro Tomador")
	ctxTomada := orgctx.WithOrganization(amb.ctx, orgTomada.UUID)
	_, err = wsSvc.Create(ctxTomada, modelworkspace.CreateInput{Nome: "tomada", Slug: "slug-tomado"})
	require.NoError(t, err)
	orgTerceira := novaOrg("Parceiro Terceiro")
	_, err = app.Provisionar(ctxPlat(), orgTerceira.UUID, aplicacaoprovisionamento.ProvisionamentoRequestDto{
		Nome: "Conflito", Email: "conflito@endpoint.teste", Senha: "senha-segura-321", Slug: "slug-tomado",
	})
	assert.ErrorIs(t, err, aplicacaoprovisionamento.ErrSlugIndisponivel)

	// --- Recuperação de tentativa interrompida --------------------------------
	// Admin já existia (falha antes do workspace): reconhecido, não duplicado.
	orgQuarta := novaOrg("Parceiro Quarta")
	ctxQuarta := orgctx.WithOrganization(amb.ctx, orgQuarta.UUID)
	preExistente, err := svcUser.Create(ctxQuarta, dominioUsuario.EntradaCriacao{
		Dados: modeluser.CreateInput{Nome: "Admin Cansado", Email: "cansado@endpoint.teste"}, Senha: "senha-segura-654",
	})
	require.NoError(t, err)
	resp4, err := app.Provisionar(ctxPlat(), orgQuarta.UUID, aplicacaoprovisionamento.ProvisionamentoRequestDto{
		Nome: "Admin Cansado", Email: "cansado@endpoint.teste", Senha: "senha-segura-654", Slug: "quarto-principal",
	})
	require.NoError(t, err)
	assert.Equal(t, preExistente.UUID, resp4.AdminUUID, "admin pré-existente é reconhecido, nunca duplicado")
}

var _ = dominioUsuario.ErrNotFound
