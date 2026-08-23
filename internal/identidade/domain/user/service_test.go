package user

import (
	"context"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	modeluser "workspace-api/internal/identidade/model/user"
	"workspace-api/internal/pkg/orgctx"
)

// --- Dublês (regra: testes de service NUNCA passam pelo singleton) ----------

// Nomes canônicos dos papéis seed (mesmos do bootstrap/agents/03).
const (
	papelAdminWorkspace   = "admin_workspace"
	papelUsuarioWorkspace = "usuario_workspace"
)

type repoFake struct {
	mu           sync.Mutex
	porUUID      map[uuid.UUID]*modeluser.User
	emails       map[string]modeluser.Email // "org|email" — simula o índice único parcial
	tokens       map[string]*modeluser.RefreshToken
	revogacoes   int // vezes que RevogarTokensAtivosDoUsuario rodou
	erroAoSalvar error
}

func novoRepoFake() *repoFake {
	return &repoFake{
		porUUID: map[uuid.UUID]*modeluser.User{},
		emails:  map[string]modeluser.Email{},
		tokens:  map[string]*modeluser.RefreshToken{},
	}
}

// exigirEscopo emula o fail-closed do orgctx.ScopeOrganization no banco real:
// ctx sem organization = query falha; organization divergente = não encontra.
func exigirEscopo(ctx context.Context, registro *modeluser.User) error {
	org := orgctx.OrganizationUUID(ctx)
	if org == uuid.Nil {
		return orgctx.ErrEscopoAusente
	}
	if registro != nil && registro.OrganizationUUID != org {
		return ErrNotFound // filtro de escopo não encontra → não vaza existência
	}
	return nil
}

func (r *repoFake) Criar(ctx context.Context, u *modeluser.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.erroAoSalvar != nil {
		return r.erroAoSalvar
	}
	chave := u.OrganizationUUID.String() + "|" + u.Email.String()
	if _, dup := r.emails[chave]; dup {
		return ErrEmailEmUso
	}
	r.emails[chave] = u.Email
	copia := *u
	r.porUUID[u.UUID] = &copia
	return nil
}

func (r *repoFake) BuscarPorUUID(ctx context.Context, id uuid.UUID) (*modeluser.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.porUUID[id]
	if !ok {
		return nil, ErrNotFound
	}
	if err := exigirEscopo(ctx, u); err != nil {
		return nil, err
	}
	copia := *u
	return &copia, nil
}

func (r *repoFake) BuscarPorEmail(ctx context.Context, email modeluser.Email) (*modeluser.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, u := range r.porUUID {
		if u.Email == email {
			if err := exigirEscopo(ctx, u); err != nil {
				if err == orgctx.ErrEscopoAusente {
					return nil, orgctx.ErrEscopoAusente
				}
				return nil, ErrNotFound
			}
			copia := *u
			return &copia, nil
		}
	}
	return nil, ErrNotFound
}

func (r *repoFake) Listar(_ context.Context, f modeluser.ListFilter) ([]modeluser.User, int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]modeluser.User, 0)
	for _, u := range r.porUUID {
		if f.Status != nil && u.Status != *f.Status {
			continue
		}
		items = append(items, *u)
	}
	return items, int64(len(items)), nil
}

func (r *repoFake) Atualizar(ctx context.Context, u *modeluser.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	existente, ok := r.porUUID[u.UUID]
	if !ok {
		return ErrNotFound
	}
	if err := exigirEscopo(ctx, existente); err != nil {
		return err
	}
	copia := *u
	r.porUUID[u.UUID] = &copia
	return nil
}

func (r *repoFake) Remover(ctx context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.porUUID[id]
	if !ok {
		return ErrNotFound
	}
	if err := exigirEscopo(ctx, u); err != nil {
		return err
	}
	delete(r.porUUID, id)
	delete(r.emails, u.OrganizationUUID.String()+"|"+u.Email.String())
	return nil
}

func (r *repoFake) RegistrarRefreshToken(ctx context.Context, t *modeluser.RefreshToken) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := exigirEscopo(ctx, &modeluser.User{OrganizationUUID: t.OrganizationUUID}); err != nil {
		return err
	}
	copia := *t
	r.tokens[t.JTI] = &copia
	return nil
}

func (r *repoFake) BuscarRefreshToken(ctx context.Context, usuarioUUID uuid.UUID, jti string) (*modeluser.RefreshToken, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tokens[jti]
	if !ok || t.UserUUID != usuarioUUID || orgctx.OrganizationUUID(ctx) != t.OrganizationUUID {
		return nil, ErrRefreshTokenInvalido
	}
	copia := *t
	return &copia, nil
}

func (r *repoFake) RevogarRefreshToken(ctx context.Context, t *modeluser.RefreshToken) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := exigirEscopo(ctx, &modeluser.User{OrganizationUUID: t.OrganizationUUID}); err != nil {
		return err
	}
	copia := *t
	r.tokens[t.JTI] = &copia
	return nil
}

func (r *repoFake) RevogarTokensAtivosDoUsuario(_ context.Context, usuarioUUID uuid.UUID) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.revogacoes++
	var total int64
	for _, t := range r.tokens {
		if t.UserUUID == usuarioUUID && t.RevogadoEm == nil {
			momento := time.Now().UTC()
			t.RevogadoEm = &momento
			total++
		}
	}
	return total, nil
}

// RevogarTokensAtivosDaOrganization emula o fail-closed do escopo: ctx sem
// organization falha; organization divergente não encontra nenhuma linha.
func (r *repoFake) RevogarTokensAtivosDaOrganization(ctx context.Context) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	org := orgctx.OrganizationUUID(ctx)
	if org == uuid.Nil {
		return 0, orgctx.ErrEscopoAusente
	}
	var total int64
	for _, t := range r.tokens {
		if t.OrganizationUUID == org && t.RevogadoEm == nil {
			momento := time.Now().UTC()
			t.RevogadoEm = &momento
			total++
		}
	}
	return total, nil
}

func (r *repoFake) RefreshTokenRevogado(jti string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tokens[jti]
	if !ok {
		return false, nil
	}
	return t.RevogadoEm != nil, nil
}

type atrFake struct {
	mu           sync.Mutex
	atribuicoes  map[uuid.UUID]*modeluser.Atribuicao
	diretas      map[string]bool // "user|ws" com atribuição viva
	papeis       map[uuid.UUID]string
	papelPorNome map[string][]uuid.UUID
	permissoes   map[uuid.UUID][]string
	catalogo     []modeluser.Papel // papéis globais devolvidos por ListarPapeis
	erroAoSalvar error
}

func novoAtrFake() *atrFake {
	return &atrFake{
		atribuicoes:  map[uuid.UUID]*modeluser.Atribuicao{},
		diretas:      map[string]bool{},
		papeis:       map[uuid.UUID]string{},
		papelPorNome: map[string][]uuid.UUID{},
		permissoes:   map[uuid.UUID][]string{},
	}
}

func (a *atrFake) Criar(_ context.Context, atrib *modeluser.Atribuicao) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.erroAoSalvar != nil {
		return a.erroAoSalvar
	}
	for _, existente := range a.atribuicoes {
		if existente.WorkspaceUUID == atrib.WorkspaceUUID &&
			existente.UserUUID == atrib.UserUUID && existente.PapelUUID == atrib.PapelUUID {
			return ErrAtribuicaoDuplicada
		}
	}
	copia := *atrib
	a.atribuicoes[atrib.UUID] = &copia
	a.diretas[atrib.UserUUID.String()+"|"+atrib.WorkspaceUUID.String()] = true
	return nil
}

func (a *atrFake) ListarPorUsuario(_ context.Context, usuarioUUID uuid.UUID) ([]modeluser.AtribuicaoComPapel, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	itens := make([]modeluser.AtribuicaoComPapel, 0)
	for _, atrib := range a.atribuicoes {
		if atrib.UserUUID != usuarioUUID {
			continue
		}
		itens = append(itens, modeluser.AtribuicaoComPapel{
			Atribuicao: *atrib,
			PapelNome:  a.papeis[atrib.PapelUUID],
		})
	}
	return itens, nil
}

func (a *atrFake) Remover(_ context.Context, usuarioUUID, atribuicaoUUID uuid.UUID) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	atrib, ok := a.atribuicoes[atribuicaoUUID]
	if !ok || atrib.UserUUID != usuarioUUID {
		return ErrAtribuicaoNaoEncontrada
	}
	delete(a.atribuicoes, atribuicaoUUID)
	delete(a.diretas, usuarioUUID.String()+"|"+atrib.WorkspaceUUID.String())
	return nil
}

func (a *atrFake) TemAtribuicaoDireta(_ context.Context, usuarioUUID, workspaceUUID uuid.UUID) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.diretas[usuarioUUID.String()+"|"+workspaceUUID.String()], nil
}

func (a *atrFake) TemPapelNaOrganization(_ context.Context, _ uuid.UUID, papelNome string) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	// Dublê simplificado: papel exercido em algum lugar da organization.
	return len(a.papelPorNome[papelNome]) > 0, nil
}

func (a *atrFake) TemPapelEmQualquerOrganization(_ context.Context, _ uuid.UUID, papelNome string) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.papelPorNome[papelNome]) > 0, nil
}

func (a *atrFake) PapelPorUUID(_ context.Context, papelUUID uuid.UUID) (*modeluser.Papel, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	nome, ok := a.papeis[papelUUID]
	if !ok {
		return nil, ErrPapelNaoEncontrado
	}
	return &modeluser.Papel{UUID: papelUUID, Nome: nome}, nil
}

func (a *atrFake) ListarPapeis(_ context.Context) ([]modeluser.Papel, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	itens := make([]modeluser.Papel, 0, len(a.catalogo))
	itens = append(itens, a.catalogo...)
	sort.Slice(itens, func(i, j int) bool { return itens[i].Nome < itens[j].Nome })
	return itens, nil
}

func (a *atrFake) PermissoesEfetivas(_ context.Context, _ uuid.UUID, _ uuid.UUID) ([]string, error) {
	return []string{}, nil
}

// seedPapel registra um papel no dublê e devolve o uuid dele.
func (a *atrFake) seedPapel(id uuid.UUID, nome string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.papeis[id] = nome
	a.papelPorNome[nome] = append(a.papelPorNome[nome], id)
}

// seedCatalogo popula o catálogo global devolvido por ListarPapeis.
func (a *atrFake) seedCatalogo(papeis ...modeluser.Papel) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.catalogo = append(a.catalogo, papeis...)
}

type validadorFake struct{ pertence bool }

func (v validadorFake) Pertence(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return v.pertence, nil
}

// credFalso é determinístico e conta chamadas — prova que a comparação roda
// TAMBÉM quando o usuário não existe (indistinguibilidade temporal).
type credFalso struct {
	mu       sync.Mutex
	geracoes int
	comparos int
}

func (c *credFalso) Gerar(senha string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.geracoes++
	return "hash::" + strings.ToUpper(senha), nil
}

func (c *credFalso) Comparar(hash, senha string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.comparos++
	return hash == "hash::"+strings.ToUpper(senha)
}

// --- Suíte -------------------------------------------------------------------

func montarServico(t *testing.T, validador ValidadorWorkspaces) (Service, *repoFake, *atrFake, *credFalso) {
	t.Helper()
	repo := novoRepoFake()
	atr := novoAtrFake()
	cred := &credFalso{}
	return NewService(repo, atr, validador, cred), repo, atr, cred
}

func ctxDaOrganizacao(id uuid.UUID) context.Context {
	return orgctx.WithOrganization(context.Background(), id)
}

func criarUser(t *testing.T, svc Service, ctx context.Context, email string) *modeluser.User {
	t.Helper()
	u, err := svc.Create(ctx, EntradaCriacao{
		Dados: modeluser.CreateInput{Nome: "Ana Lima", Email: email},
		Senha: "senha-segura-123",
	})
	require.NoError(t, err)
	return u
}

func TestCriarAplicaPoliticaGeraHashEEscopaPeloContexto(t *testing.T) {
	svc, repo, _, cred := montarServico(t, validadorFake{pertence: true})
	org := uuid.New()

	u, err := svc.Create(ctxDaOrganizacao(org), EntradaCriacao{
		Dados: modeluser.CreateInput{Nome: "  Ana Lima  ", Email: " Ana@Exemplo.COM "},
		Senha: "senha-segura-123",
	})
	require.NoError(t, err)
	assert.Equal(t, org, u.OrganizationUUID, "escopo vem do ctx, nunca do corpo")
	assert.Equal(t, modeluser.Email("ana@exemplo.com"), u.Email)
	assert.True(t, strings.HasPrefix(u.SenhaHash, "hash::"), "hash nasce no service")
	assert.Equal(t, 1, cred.geracoes)
	require.Len(t, repo.porUUID, 1)

	// Política de senha recusa antes de gerar hash nenhum.
	_, err = svc.Create(ctxDaOrganizacao(org), EntradaCriacao{
		Dados: modeluser.CreateInput{Nome: "Outro", Email: "outro@exemplo.com"},
		Senha: "curta",
	})
	assert.ErrorIs(t, err, modeluser.ErrSenhaInvalida)
	assert.Equal(t, 1, cred.geracoes, "senha fora da política não chega ao gerador")
}

func TestCriarRecusaEmailDuplicadoNaMesmaOrganization(t *testing.T) {
	svc, _, _, _ := montarServico(t, validadorFake{})
	ctx := ctxDaOrganizacao(uuid.New())

	criarUser(t, svc, ctx, "ana@exemplo.com")

	// Mesma organization: conflito. Outra organization: o MESMO e-mail vale.
	_, err := svc.Create(ctx, EntradaCriacao{Dados: modeluser.CreateInput{Nome: "Clone", Email: "ana@exemplo.com"}, Senha: "senha-segura-123"})
	assert.ErrorIs(t, err, ErrEmailEmUso)

	_, err = svc.Create(ctxDaOrganizacao(uuid.New()), EntradaCriacao{Dados: modeluser.CreateInput{Nome: "Homônima", Email: "ana@exemplo.com"}, Senha: "senha-segura-123"})
	assert.NoError(t, err, "unicidade de e-mail é POR organization")
}

func TestLeituraIsoladaPorOrganizacaoEFailClosed(t *testing.T) {
	svc, _, _, _ := montarServico(t, validadorFake{})
	dono := uuid.New()
	u := criarUser(t, svc, ctxDaOrganizacao(dono), "ana@exemplo.com")

	visto, err := svc.Read(ctxDaOrganizacao(dono), u.UUID)
	require.NoError(t, err)
	assert.Equal(t, u.UUID, visto.UUID)

	_, err = svc.Read(context.Background(), u.UUID)
	assert.ErrorIs(t, err, orgctx.ErrEscopoAusente, "sem escopo a query falha")

	_, err = svc.Read(ctxDaOrganizacao(uuid.New()), u.UUID)
	assert.ErrorIs(t, err, ErrNotFound, "organization alheia não vaza existência")
}

// O coração da fase: login indistinguível — não existe vs senha errada vs
// inativo devolvem o MESMO sentinela, e a comparação de hash RODA em todas.
func TestAutenticarEIndistinguivel(t *testing.T) {
	svc, _, _, cred := montarServico(t, validadorFake{})
	dono := uuid.New()
	ctx := ctxDaOrganizacao(dono)
	ana := criarUser(t, svc, ctx, "ana@exemplo.com")

	ok, err := svc.Autenticar(ctx, "ana@exemplo.com", "senha-segura-123")
	require.NoError(t, err)
	assert.Equal(t, ana.UUID, ok.UUID)
	comparosLogin := cred.comparos
	require.GreaterOrEqual(t, comparosLogin, 1)

	// Senha errada: comparação real contra o hash verdadeiro.
	_, err = svc.Autenticar(ctx, "ana@exemplo.com", "errada-de-propósito")
	assert.ErrorIs(t, err, ErrCredenciaisInvalidas)

	// Usuário inexistente: comparação contra hash de mentira — MESMO erro,
	// MESMO número de comparações (não vaza existência nem por timing).
	cred.mu.Lock()
	antes := cred.comparos
	cred.mu.Unlock()
	_, err = svc.Autenticar(ctx, "fantasma@exemplo.com", "qualquer-coisa")
	assert.ErrorIs(t, err, ErrCredenciaisInvalidas)
	cred.mu.Lock()
	depois := cred.comparos
	cred.mu.Unlock()
	assert.Equal(t, antes+1, depois, "hash de mentira mantém o custo temporal")

	// E-mail de OUTRA organization não existe para esta.
	_, err = svc.Autenticar(ctxDaOrganizacao(uuid.New()), "ana@exemplo.com", "senha-segura-123")
	assert.ErrorIs(t, err, ErrCredenciaisInvalidas)

	// Inativo também vira credencial inválida — estado não se revela.
	inativo := modeluser.StatusInativo
	_, err = svc.Update(ctx, ana.UUID, modeluser.UpdateInput{Status: &inativo})
	require.NoError(t, err)
	_, err = svc.Autenticar(ctx, "ana@exemplo.com", "senha-segura-123")
	assert.ErrorIs(t, err, ErrCredenciaisInvalidas)
}

func TestUpdateInativarRevogaSessoesAbertas(t *testing.T) {
	svc, repo, _, _ := montarServico(t, validadorFake{})
	dono := uuid.New()
	ctx := ctxDaOrganizacao(dono)
	ana := criarUser(t, svc, ctx, "ana@exemplo.com")

	jti := "jti-da-sessao"
	require.NoError(t, svc.RegistrarSessao(ctx, ana.UUID, jti, time.Now().Add(time.Hour).UTC()))

	inativo := modeluser.StatusInativo
	atualizado, err := svc.Update(ctx, ana.UUID, modeluser.UpdateInput{Status: &inativo})
	require.NoError(t, err)
	assert.Equal(t, modeluser.StatusInativo, atualizado.Status)
	assert.Equal(t, 1, repo.revogacoes, "sessões abertas morrem com a conta")

	ativa, err := svc.SessaoAtiva(ctx, ana.UUID, jti)
	require.NoError(t, err)
	assert.False(t, ativa, "refresh token revogado na inativação")

	// Reativação devolve o acesso (sessões antigas continuam revogadas).
	ativo := modeluser.StatusAtivo
	_, err = svc.Update(ctx, ana.UUID, modeluser.UpdateInput{Status: &ativo})
	require.NoError(t, err)
}

func TestSessoesPersistidasERevogacaoIdempotente(t *testing.T) {
	svc, _, _, _ := montarServico(t, validadorFake{})
	ctx := ctxDaOrganizacao(uuid.New())
	ana := criarUser(t, svc, ctx, "ana@exemplo.com")
	expira := time.Now().UTC().Add(time.Hour)

	require.NoError(t, svc.RegistrarSessao(ctx, ana.UUID, "jti-1", expira))

	ativa, err := svc.SessaoAtiva(ctx, ana.UUID, "jti-1")
	require.NoError(t, err)
	assert.True(t, ativa)

	// Sessão de outro usuário/organization não existe aqui.
	assert.ErrorIs(t, func() error { _, err := svc.SessaoAtiva(ctxDaOrganizacao(uuid.New()), ana.UUID, "jti-1"); return err }(), ErrRefreshTokenInvalido)

	require.NoError(t, svc.EncerrarSessao(ctx, ana.UUID, "jti-1"))
	ativa, err = svc.SessaoAtiva(ctx, ana.UUID, "jti-1")
	require.NoError(t, err)
	assert.False(t, ativa)

	// Logout idempotente: já revogado é sucesso; linha ausente é recusa.
	assert.NoError(t, svc.EncerrarSessao(ctx, ana.UUID, "jti-1"))
	assert.ErrorIs(t, svc.EncerrarSessao(ctx, ana.UUID, "inexistente"), ErrRefreshTokenInvalido)
}

// R4 (issue #22): lado user da cascata da organization — TODAS as sessões
// abertas da organization escopada no ctx morrem; ctx sem escopo falha
// fechado e organization divergente não encerra sessão alheia.
func TestRevogarSessoesDaOrganizationEscopada(t *testing.T) {
	svc, _, _, _ := montarServico(t, validadorFake{})
	orgA, orgB := uuid.New(), uuid.New()
	ctxA, ctxB := ctxDaOrganizacao(orgA), ctxDaOrganizacao(orgB)
	ana := criarUser(t, svc, ctxA, "ana@exemplo.com")
	bruno := criarUser(t, svc, ctxB, "bruno@exemplo.com")
	expira := time.Now().UTC().Add(time.Hour)
	require.NoError(t, svc.RegistrarSessao(ctxA, ana.UUID, "jti-a-1", expira))
	require.NoError(t, svc.RegistrarSessao(ctxB, bruno.UUID, "jti-b-1", expira))

	// Ctx SEM escopo: fail-closed.
	_, err := svc.RevogarSessoesDaOrganization(context.Background())
	assert.ErrorIs(t, err, orgctx.ErrEscopoAusente)

	// A cascata da org A encerra SÓ as sessões da org A.
	total, err := svc.RevogarSessoesDaOrganization(ctxA)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)

	ativa, err := svc.SessaoAtiva(ctxA, ana.UUID, "jti-a-1")
	require.NoError(t, err)
	assert.False(t, ativa)
	ativa, err = svc.SessaoAtiva(ctxB, bruno.UUID, "jti-b-1")
	require.NoError(t, err)
	assert.True(t, ativa, "sessão de outra organization sobrevive")

	// Idempotente: rodar de novo devolve 0 sem erro.
	total, err = svc.RevogarSessoesDaOrganization(ctxA)
	require.NoError(t, err)
	assert.EqualValues(t, 0, total)
}

func TestAtribuirPapelValidaOTripeAntesDeGravar(t *testing.T) {
	svc, repo, atr, cred := montarServico(t, validadorFake{pertence: true})
	ctx := ctxDaOrganizacao(uuid.New())
	ana := criarUser(t, svc, ctx, "ana@exemplo.com")
	papelOperador := uuid.New()
	atr.seedPapel(papelOperador, papelAdminWorkspace)
	ws := uuid.New()

	a, err := svc.AtribuirPapel(ctx, ana.UUID, ws, papelOperador)
	require.NoError(t, err)
	assert.Equal(t, ws, a.WorkspaceUUID)

	// Duplicada: mesmo papel para o mesmo par usuário×workspace.
	_, err = svc.AtribuirPapel(ctx, ana.UUID, ws, papelOperador)
	assert.ErrorIs(t, err, ErrAtribuicaoDuplicada)

	// Papel inexistente.
	_, err = svc.AtribuirPapel(ctx, ana.UUID, uuid.New(), uuid.New())
	assert.ErrorIs(t, err, ErrPapelNaoEncontrado)

	// Workspace de outra organization: recusado SEM gravar nada — mesmo
	// repositório (o usuário existe), só muda o veredito do contrato.
	svcAlheio := NewService(repo, atr, validadorFake{pertence: false}, cred)
	antes := len(atr.atribuicoes)
	_, err = svcAlheio.AtribuirPapel(ctx, ana.UUID, ws, papelOperador)
	assert.ErrorIs(t, err, ErrWorkspaceInvalido)
	assert.Len(t, atr.atribuicoes, antes, "nada é gravado quando o workspace não pertence")

	// Listagem e remoção pelo uuid da atribuição.
	itens, err := svc.Atribuicoes(ctx, ana.UUID)
	require.NoError(t, err)
	require.Len(t, itens, 1)
	assert.Equal(t, papelAdminWorkspace, itens[0].PapelNome)

	require.NoError(t, svc.RemoverAtribuicao(ctx, ana.UUID, a.UUID))
	assert.ErrorIs(t, svc.RemoverAtribuicao(ctx, ana.UUID, a.UUID), ErrAtribuicaoNaoEncontrada)
}

func TestTemVinculoDiretoESuporteAuditado(t *testing.T) {
	svc, _, atr, _ := montarServico(t, validadorFake{pertence: true})
	ctx := ctxDaOrganizacao(uuid.New())
	operador := criarUser(t, svc, ctx, "operador@exemplo.com").UUID
	ws := uuid.New()

	// Sem nada: sem vínculo.
	vinculo, err := svc.TemVinculo(ctx, operador, ws)
	require.NoError(t, err)
	assert.False(t, vinculo)

	// Direto: atribuição no workspace.
	papel := uuid.New()
	atr.seedPapel(papel, papelUsuarioWorkspace)
	_, err = svc.AtribuirPapel(ctx, operador, ws, papel)
	require.NoError(t, err)
	vinculo, err = svc.TemVinculo(ctx, operador, ws)
	require.NoError(t, err)
	assert.True(t, vinculo)

	// Suporte: papel global de super_admin concede vínculo em qualquer lugar.
	super := uuid.New()
	atr.seedPapel(super, papelSuperAdmin)
	outroWs := uuid.New()
	vinculo, err = svc.TemVinculo(ctx, super, outroWs)
	require.NoError(t, err)
	assert.True(t, vinculo, "super_admin atravessa organizations")
}
