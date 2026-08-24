package provisionamento

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"workspace-api/internal/pkg/log/audit_log"
	"workspace-api/internal/pkg/orgctx"
)

// --- Dublês dos quatro contratos (regra: teste NUNCA passa pelo singleton) ---

type organizacoesFake struct {
	existe bool
	ativa  bool
	err    error
}

func (o organizacoesFake) Estado(context.Context, uuid.UUID) (bool, bool, error) {
	return o.existe, o.ativa, o.err
}

type workspacesFake struct {
	tem        bool
	criado     uuid.UUID
	ultimoSlug string
	err        error
}

func (w *workspacesFake) TemWorkspaces(context.Context, uuid.UUID) (bool, error) {
	return w.tem, nil
}

func (w *workspacesFake) Criar(_ context.Context, organizationUUID uuid.UUID, slug string) (uuid.UUID, error) {
	if w.err != nil {
		return uuid.Nil, w.err
	}
	w.ultimoSlug = slug
	id := uuid.NewSHA1(uuid.NameSpaceURL, []byte(slug+"|"+organizationUUID.String()))
	w.criado = id
	return id, nil
}

type usuariosFake struct {
	porEmail        map[string]uuid.UUID
	criados         []string
	atribuidos      [][3]uuid.UUID
	recusarCriar    error
	recusarAtrib    error
	senhasRecebidas []string
}

func novoUsuariosFake() *usuariosFake {
	return &usuariosFake{porEmail: map[string]uuid.UUID{}}
}

func (u *usuariosFake) UUIDPorEmail(_ context.Context, email string) (uuid.UUID, bool, error) {
	id, ok := u.porEmail[email]
	return id, ok, nil
}

func (u *usuariosFake) CriarAdmin(ctx context.Context, nome, email, senha string) (uuid.UUID, error) {
	if u.recusarCriar != nil {
		return uuid.Nil, u.recusarCriar
	}
	u.criados = append(u.criados, email)
	u.senhasRecebidas = append(u.senhasRecebidas, senha)
	id := uuid.NewSHA1(uuid.NameSpaceURL, []byte(email))
	u.porEmail[email] = id
	return id, nil
}

func (u *usuariosFake) AtribuirPapel(_ context.Context, usuarioUUID, workspaceUUID, papelUUID uuid.UUID) error {
	if u.recusarAtrib != nil {
		return u.recusarAtrib
	}
	u.atribuidos = append(u.atribuidos, [3]uuid.UUID{usuarioUUID, workspaceUUID, papelUUID})
	return nil
}

type papeisFake struct {
	uuids map[string]uuid.UUID
}

func (p papeisFake) PorNome(_ context.Context, nome string) (uuid.UUID, error) {
	if id, ok := p.uuids[nome]; ok {
		return id, nil
	}
	return uuid.Nil, ErrPapelAusente
}

// trilhaFake captura os eventos de auditoria da orquestração.
type trilhaFake struct {
	eventos []audit_log.Evento
}

func (t *trilhaFake) Registrar(ev audit_log.Evento) { t.eventos = append(t.eventos, ev) }

// --- Suíte -------------------------------------------------------------------

func montarApp(t *testing.T, mutar func(*organizacoesFake, *workspacesFake, *usuariosFake, *papeisFake)) (Service, *workspacesFake, *usuariosFake, *trilhaFake) {
	t.Helper()
	orgs := &organizacoesFake{existe: true, ativa: true}
	ws := &workspacesFake{}
	us := novoUsuariosFake()
	pp := &papeisFake{uuids: map[string]uuid.UUID{"admin_organization": uuid.MustParse("aaaaaaaa-0000-0000-8000-00000000ba01")}}
	if mutar != nil {
		mutar(orgs, ws, us, pp)
	}
	trilha := &trilhaFake{}
	app := NewService(Dependencias{
		Organizacoes: orgs,
		Workspaces:   ws,
		Usuarios:     us,
		Papeis:       pp,
		Trilha:       trilha,
	})
	return app, ws, us, trilha
}

func entradaValida() ProvisionamentoRequestDto {
	return ProvisionamentoRequestDto{
		Nome:  "Ana Fundadora",
		Email: "ana@parceiro.teste",
		Senha: "senha-segura-123",
		Slug:  "parceiro-principal",
	}
}

func TestProvisionarFeliz(t *testing.T) {
	app, ws, us, trilha := montarApp(t, nil)
	alvo := uuid.New()

	resp, err := app.Provisionar(ctxPlataformaProvisionamento(), alvo, entradaValida())
	require.NoError(t, err)
	assert.Equal(t, alvo, resp.OrganizationUUID)
	assert.Equal(t, ws.criado, resp.WorkspaceUUID)
	assert.NotEqual(t, uuid.Nil, resp.AdminUUID)

	// Workspace inicial criado e papel da plataforma atribuído ao admin.
	require.Len(t, us.atribuidos, 1)
	assert.Equal(t, resp.AdminUUID, us.atribuidos[0][0])
	assert.Equal(t, resp.WorkspaceUUID, us.atribuidos[0][1])

	// Auditoria da ORQUESTRAÇÃO: e-mail mascarado, senha em lugar nenhum,
	// identificadores presentes.
	require.Len(t, trilha.eventos, 1)
	ev := trilha.eventos[0]
	assert.Equal(t, "provisionar", ev.Acao)
	assert.Equal(t, Subdominio, ev.Subdominio)
	assert.Equal(t, alvo.String(), ev.OrganizationUUID)
	assert.Equal(t, "a***@parceiro.teste", ev.Detalhes["email_mascarado"], "e-mail sai MASCARADO no log")
	for chave, valor := range ev.Detalhes {
		assert.NotContains(t, valor, "senha-segura-123", "senha nunca entra no log (campo %s)", chave)
	}
	assert.False(t, strings.Contains(resp.Email, "*"), "a RESPOSTA carrega o e-mail informado (o chamador o definiu)")
}

func ctxPlataformaProvisionamento() context.Context {
	return orgctx.WithPermissoes(context.Background(), []string{"*:*"})
}

func TestProvisionarRecusaEntradaInvalidaAntesDoBanco(t *testing.T) {
	casos := []struct {
		nome string
		mut  func(*ProvisionamentoRequestDto)
	}{
		{"slug malformado", func(d *ProvisionamentoRequestDto) { d.Slug = "Ruim!" }},
		{"e-mail inválido", func(d *ProvisionamentoRequestDto) { d.Email = "sem-arroba" }},
		{"senha curta", func(d *ProvisionamentoRequestDto) { d.Senha = "curta" }},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			app, _, _, _ := montarApp(t, nil)
			entrada := entradaValida()
			caso.mut(&entrada)
			_, err := app.Provisionar(ctxPlataformaProvisionamento(), uuid.New(), entrada)
			assert.ErrorIs(t, err, ErrInvalidInput, "falha de formato morre nos VOs ANTES de qualquer contrato")
		})
	}
}

func TestProvisionarValidaOAlvoEAIdempotencia(t *testing.T) {
	casos := []struct {
		nome     string
		mutar    func(*organizacoesFake, *workspacesFake, *usuariosFake, *papeisFake)
		esperado error
	}{
		{
			nome:     "organization inexistente",
			mutar:    func(o *organizacoesFake, _ *workspacesFake, _ *usuariosFake, _ *papeisFake) { o.existe = false },
			esperado: ErrOrganizacaoNaoEncontrada,
		},
		{
			nome:     "organization inativa",
			mutar:    func(o *organizacoesFake, _ *workspacesFake, _ *usuariosFake, _ *papeisFake) { o.ativa = false },
			esperado: ErrOrganizacaoInativa,
		},
		{
			nome: "organization já provisionada",
			mutar: func(_ *organizacoesFake, w *workspacesFake, _ *usuariosFake, _ *papeisFake) {
				w.tem = true
			},
			esperado: ErrJaProvisionado,
		},
		{
			nome: "slug globalmente indisponível",
			mutar: func(_ *organizacoesFake, w *workspacesFake, _ *usuariosFake, _ *papeisFake) {
				w.err = ErrSlugIndisponivel
			},
			esperado: ErrSlugIndisponivel,
		},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			app, _, us, _ := montarApp(t, caso.mutar)
			_, err := app.Provisionar(ctxPlataformaProvisionamento(), uuid.New(), entradaValida())
			assert.ErrorIs(t, err, caso.esperado)
			assert.Empty(t, us.criados, "recusa NUNCA cria admin")
			assert.Empty(t, us.atribuidos, "recusa NUNCA atribui papel")
		})
	}
}

func TestProvisionarReconheceAdminDeTentativaAnterior(t *testing.T) {
	app, _, us, _ := montarApp(t, nil)
	preExistente := uuid.New()

	// Admin criado numa tentativa interrompida (workspace falhou depois):
	// reconhecido pelo e-mail, NUNCA duplicado.
	us.porEmail["ana@parceiro.teste"] = preExistente

	resp, err := app.Provisionar(ctxPlataformaProvisionamento(), uuid.New(), entradaValida())
	require.NoError(t, err)
	assert.Empty(t, us.criados, "admin pré-existente não é recriado")
	assert.Equal(t, preExistente, resp.AdminUUID)
}

func TestProvisionarTrataAtribuicaoDuplicadaComoIdempotencia(t *testing.T) {
	app, _, _, _ := montarApp(t, func(_ *organizacoesFake, _ *workspacesFake, u *usuariosFake, _ *papeisFake) {
		u.recusarAtrib = ErrAtribuicaoExistente // tradução do adaptador para a duplicata do subdomínio
	})

	resp, err := app.Provisionar(ctxPlataformaProvisionamento(), uuid.New(), entradaValida())
	require.NoError(t, err, "duplicata de atribuição é idempotência interna, não erro")
	assert.NotEqual(t, uuid.Nil, resp.AdminUUID)
}

func TestProvisionarSemOPapelSeedFalhaComErroMapeado(t *testing.T) {
	app, _, _, _ := montarApp(t, func(_ *organizacoesFake, _ *workspacesFake, _ *usuariosFake, p *papeisFake) {
		p.uuids = map[string]uuid.UUID{} // seed não rodou
	})

	_, err := app.Provisionar(ctxPlataformaProvisionamento(), uuid.New(), entradaValida())
	assert.ErrorIs(t, err, ErrPapelAusente)
}
