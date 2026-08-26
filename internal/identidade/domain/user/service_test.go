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

// BuscarPorUUIDs emula o lote real: escopado quando o ctx carrega
// organization, global caso contrário; ausentes simplesmente faltam.
func (r *repoFake) BuscarPorUUIDs(ctx context.Context, ids []uuid.UUID) ([]modeluser.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	encontrados := make([]modeluser.User, 0, len(ids))
	for _, id := range ids {
		u, ok := r.porUUID[id]
		if !ok {
			continue
		}
		org := orgctx.OrganizationUUID(ctx)
		if org != uuid.Nil && u.OrganizationUUID != org {
			continue // alheio não existe para este escopo
		}
		encontrados = append(encontrados, *u)
	}
	return encontrados, nil
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

func (a *atrFake) TemPapelNaOrganization(_ context.Context, usuarioUUID uuid.UUID, papelNome string) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, atrib := range a.atribuicoes {
		if atrib.UserUUID == usuarioUUID && a.papeis[atrib.PapelUUID] == papelNome {
			return true, nil
		}
	}
	return false, nil
}

func (a *atrFake) TemPapelEmQualquerOrganization(_ context.Context, usuarioUUID uuid.UUID, papelNome string) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, atrib := range a.atribuicoes {
		if atrib.UserUUID == usuarioUUID && a.papeis[atrib.PapelUUID] == papelNome {
			return true, nil
		}
	}
	return false, nil
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

func (a *atrFake) MaiorPapel(_ context.Context, usuarioUUID uuid.UUID) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	nomes := make([]string, 0)
	for _, atrib := range a.atribuicoes {
		if atrib.UserUUID != usuarioUUID {
			continue
		}
		nomes = append(nomes, a.papeis[atrib.PapelUUID])
	}
	return maiorPapelPorNome(nomes), nil
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

// seedAtribuicao cria uma atribuição diretamente no dublê, sem passar pelas
// validações de hierarquia do service — útil para montar cenários de teste.
func (a *atrFake) seedAtribuicao(usuarioUUID, workspaceUUID, papelUUID uuid.UUID) {
	a.mu.Lock()
	defer a.mu.Unlock()
	atrib := &modeluser.Atribuicao{
		UUID:             uuid.New(),
		OrganizationUUID: uuid.Nil,
		WorkspaceUUID:    workspaceUUID,
		UserUUID:         usuarioUUID,
		PapelUUID:        papelUUID,
	}
	a.atribuicoes[atrib.UUID] = atrib
	a.diretas[usuarioUUID.String()+"|"+workspaceUUID.String()] = true
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
	_, err = svc.Update(orgctx.WithUser(ctx, ana.UUID), ana.UUID, modeluser.UpdateInput{Status: &inativo})
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
	ctxComoAna := orgctx.WithUser(ctx, ana.UUID)
	atualizado, err := svc.Update(ctxComoAna, ana.UUID, modeluser.UpdateInput{Status: &inativo})
	require.NoError(t, err)
	assert.Equal(t, modeluser.StatusInativo, atualizado.Status)
	assert.Equal(t, 1, repo.revogacoes, "sessões abertas morrem com a conta")

	ativa, err := svc.SessaoAtiva(ctx, ana.UUID, jti)
	require.NoError(t, err)
	assert.False(t, ativa, "refresh token revogado na inativação")

	// Reativação devolve o acesso (sessões antigas continuam revogadas).
	ativo := modeluser.StatusAtivo
	_, err = svc.Update(ctxComoAna, ana.UUID, modeluser.UpdateInput{Status: &ativo})
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
	operador := criarUser(t, svc, ctx, "operador@exemplo.com")
	ana := criarUser(t, svc, ctx, "ana@exemplo.com")

	papelSuper := uuid.New()
	atr.seedPapel(papelSuper, papelSuperAdmin)
	papelOperador := uuid.New()
	atr.seedPapel(papelOperador, papelAdminWorkspace)
	ws := uuid.New()
	// super_admin no workspace para ter hierarquia suficiente.
	atr.seedAtribuicao(operador.UUID, ws, papelSuper)
	ctx = orgctx.WithUser(ctx, operador.UUID)

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
	operador := criarUser(t, svc, ctx, "operador@exemplo.com")
	ws := uuid.New()

	superOperador := criarUser(t, svc, ctx, "superoperador@exemplo.com")
	papelSuperUUID := uuid.New()
	atr.seedPapel(papelSuperUUID, papelSuperAdmin)
	atr.seedAtribuicao(superOperador.UUID, ws, papelSuperUUID)
	ctx = orgctx.WithUser(ctx, superOperador.UUID)

	// Sem nada: sem vínculo.
	vinculo, err := svc.TemVinculo(ctx, operador.UUID, ws)
	require.NoError(t, err)
	assert.False(t, vinculo)

	// Direto: atribuição no workspace.
	papel := uuid.New()
	atr.seedPapel(papel, papelUsuarioWorkspace)
	_, err = svc.AtribuirPapel(ctx, operador.UUID, ws, papel)
	require.NoError(t, err)
	vinculo, err = svc.TemVinculo(ctx, operador.UUID, ws)
	require.NoError(t, err)
	assert.True(t, vinculo)

	// Suporte: papel global de super_admin concede vínculo em qualquer lugar.
	super := uuid.New()
	atr.seedPapel(super, papelSuperAdmin)
	atr.seedAtribuicao(super, uuid.New(), super)
	outroWs := uuid.New()
	vinculo, err = svc.TemVinculo(ctx, super, outroWs)
	require.NoError(t, err)
	assert.True(t, vinculo, "super_admin atravessa organizations")
}

func TestUpdateERemoverRecusamUsuarioDeMaiorHierarquia(t *testing.T) {
	svc, _, atr, _ := montarServico(t, validadorFake{pertence: true})
	org := uuid.New()
	ctx := ctxDaOrganizacao(org)

	superOperador := criarUser(t, svc, ctx, "superoperador@exemplo.com")
	adminWs := criarUser(t, svc, ctx, "adminws@exemplo.com")
	super := criarUser(t, svc, ctx, "super@exemplo.com")
	usuario := criarUser(t, svc, ctx, "usuario@exemplo.com")

	papelSuperUUID := uuid.New()
	atr.seedPapel(papelSuperUUID, papelSuperAdmin)
	ws := uuid.New()
	atr.seedAtribuicao(superOperador.UUID, ws, papelSuperUUID)
	ctx = orgctx.WithUser(ctx, superOperador.UUID)

	atribuirPapel(t, svc, atr, ctx, superOperador.UUID, adminWs.UUID, papelAdminWorkspace)
	atribuirPapel(t, svc, atr, ctx, superOperador.UUID, super.UUID, papelSuperAdmin)
	atribuirPapel(t, svc, atr, ctx, superOperador.UUID, usuario.UUID, papelUsuarioWorkspace)

	ctxAdmin := orgctx.WithUser(ctx, adminWs.UUID)
	inativo := modeluser.StatusInativo

	_, err := svc.Update(ctxAdmin, super.UUID, modeluser.UpdateInput{Status: &inativo})
	assert.ErrorIs(t, err, ErrHierarquiaInsufficiente, "admin_workspace não inativa super_admin")

	outroAdmin := criarUser(t, svc, ctx, "outroadmin@exemplo.com")
	atribuirPapel(t, svc, atr, ctx, superOperador.UUID, outroAdmin.UUID, papelAdminWorkspace)
	_, err = svc.Update(ctxAdmin, outroAdmin.UUID, modeluser.UpdateInput{Status: &inativo})
	assert.ErrorIs(t, err, ErrHierarquiaInsufficiente, "admin_workspace não inativa par")

	_, err = svc.Update(ctxAdmin, usuario.UUID, modeluser.UpdateInput{Status: &inativo})
	assert.NoError(t, err, "admin_workspace inativa usuário de hierarquia menor")

	err = svc.Delete(ctxAdmin, super.UUID)
	assert.ErrorIs(t, err, ErrHierarquiaInsufficiente, "admin_workspace não remove super_admin")
}

func TestUpdatePermiteAutoGerenciamento(t *testing.T) {
	svc, _, atr, _ := montarServico(t, validadorFake{pertence: true})
	org := uuid.New()
	ctx := ctxDaOrganizacao(org)
	superOperador := criarUser(t, svc, ctx, "superoperador@exemplo.com")
	adminWs := criarUser(t, svc, ctx, "adminws@exemplo.com")

	papelSuperUUID := uuid.New()
	atr.seedPapel(papelSuperUUID, papelSuperAdmin)
	ws := uuid.New()
	atr.seedAtribuicao(superOperador.UUID, ws, papelSuperUUID)
	ctx = orgctx.WithUser(ctx, superOperador.UUID)

	atribuirPapel(t, svc, atr, ctx, superOperador.UUID, adminWs.UUID, papelAdminWorkspace)

	ctxAdmin := orgctx.WithUser(ctx, adminWs.UUID)
	inativo := modeluser.StatusInativo
	_, err := svc.Update(ctxAdmin, adminWs.UUID, modeluser.UpdateInput{Status: &inativo})
	assert.NoError(t, err, "usuário pode se inativar")
}

func TestUpdateSemOperadorNoContextoRecusa(t *testing.T) {
	svc, _, _, _ := montarServico(t, validadorFake{})
	org := uuid.New()
	ctx := ctxDaOrganizacao(org)
	ana := criarUser(t, svc, ctx, "ana@exemplo.com")

	inativo := modeluser.StatusInativo
	_, err := svc.Update(ctx, ana.UUID, modeluser.UpdateInput{Status: &inativo})
	assert.ErrorIs(t, err, ErrOperadorNaoIdentificado)
}

func TestAlterarSenhaPeloProprioUsuario(t *testing.T) {
	svc, repo, _, cred := montarServico(t, validadorFake{})
	org := uuid.New()
	ctx := ctxDaOrganizacao(org)
	ana := criarUser(t, svc, ctx, "ana@exemplo.com")

	err := svc.AlterarSenha(orgctx.WithUser(ctx, ana.UUID), ana.UUID, "senha-segura-123", "nova-senha-456")
	require.NoError(t, err)

	u, err := repo.BuscarPorUUID(ctx, ana.UUID)
	require.NoError(t, err)
	assert.True(t, cred.Comparar(u.SenhaHash, "nova-senha-456"), "hash novo gravado")

	err = svc.AlterarSenha(orgctx.WithUser(ctx, ana.UUID), ana.UUID, "senha-errada", "outra-senha-789")
	assert.ErrorIs(t, err, ErrCredenciaisInvalidas, "senha atual errada recusa")
}

func TestAlterarSenhaPorOperadorSuperior(t *testing.T) {
	svc, repo, atr, cred := montarServico(t, validadorFake{pertence: true})
	org := uuid.New()
	ctx := ctxDaOrganizacao(org)
	superOperador := criarUser(t, svc, ctx, "super@exemplo.com")
	adminWs := criarUser(t, svc, ctx, "adminws@exemplo.com")
	usuario := criarUser(t, svc, ctx, "usuario@exemplo.com")

	papelSuperUUID := uuid.New()
	atr.seedPapel(papelSuperUUID, papelSuperAdmin)
	ws := uuid.New()
	atr.seedAtribuicao(superOperador.UUID, ws, papelSuperUUID)
	ctx = orgctx.WithUser(ctx, superOperador.UUID)

	atribuirPapel(t, svc, atr, ctx, superOperador.UUID, adminWs.UUID, papelAdminWorkspace)
	atribuirPapel(t, svc, atr, ctx, superOperador.UUID, usuario.UUID, papelUsuarioWorkspace)

	// admin_workspace troca senha de usuario_workspace sem senha atual.
	err := svc.AlterarSenha(orgctx.WithUser(ctx, adminWs.UUID), usuario.UUID, "", "senha-do-usuario-999")
	require.NoError(t, err)
	u, err := repo.BuscarPorUUID(ctx, usuario.UUID)
	require.NoError(t, err)
	assert.True(t, cred.Comparar(u.SenhaHash, "senha-do-usuario-999"))

	// usuario_workspace NÃO troca senha de admin_workspace (hierarquia maior).
	err = svc.AlterarSenha(orgctx.WithUser(ctx, usuario.UUID), adminWs.UUID, "", "senha-roubada-000")
	assert.ErrorIs(t, err, ErrHierarquiaInsufficiente)

	// super_admin troca senha de admin_workspace.
	err = svc.AlterarSenha(orgctx.WithUser(ctx, superOperador.UUID), adminWs.UUID, "", "senha-do-admin-111")
	require.NoError(t, err)
	u, err = repo.BuscarPorUUID(ctx, adminWs.UUID)
	require.NoError(t, err)
	assert.True(t, cred.Comparar(u.SenhaHash, "senha-do-admin-111"))
}

func TestAlterarSenhaInvalidaERevogaSessoes(t *testing.T) {
	svc, _, _, _ := montarServico(t, validadorFake{})
	org := uuid.New()
	ctx := ctxDaOrganizacao(org)
	ana := criarUser(t, svc, ctx, "ana@exemplo.com")

	// Sessões abertas antes da troca.
	require.NoError(t, svc.RegistrarSessao(ctx, ana.UUID, "jti-1", time.Now().Add(time.Hour)))
	require.NoError(t, svc.RegistrarSessao(ctx, ana.UUID, "jti-2", time.Now().Add(time.Hour)))

	err := svc.AlterarSenha(orgctx.WithUser(ctx, ana.UUID), ana.UUID, "senha-segura-123", "nova-senha-456")
	require.NoError(t, err)

	ativa, err := svc.SessaoAtiva(ctx, ana.UUID, "jti-1")
	require.NoError(t, err)
	assert.False(t, ativa, "sessão jti-1 encerrada")
	ativa, err = svc.SessaoAtiva(ctx, ana.UUID, "jti-2")
	require.NoError(t, err)
	assert.False(t, ativa, "sessão jti-2 encerrada")

	// Política de senha: abaixo do mínimo recusa.
	err = svc.AlterarSenha(orgctx.WithUser(ctx, ana.UUID), ana.UUID, "nova-senha-456", "curta")
	assert.ErrorIs(t, err, modeluser.ErrSenhaInvalida)
}

func TestAlterarSenhaSemOperadorRecusa(t *testing.T) {
	svc, _, _, _ := montarServico(t, validadorFake{})
	org := uuid.New()
	ctx := ctxDaOrganizacao(org)
	ana := criarUser(t, svc, ctx, "ana@exemplo.com")

	err := svc.AlterarSenha(ctx, ana.UUID, "senha-segura-123", "nova-senha-456")
	assert.ErrorIs(t, err, ErrOperadorNaoIdentificado)
}

func TestAtribuirPapelERemoverRespeitamHierarquia(t *testing.T) {
	svc, _, atr, _ := montarServico(t, validadorFake{pertence: true})
	org := uuid.New()
	ctx := ctxDaOrganizacao(org)

	superOperador := criarUser(t, svc, ctx, "superoperador@exemplo.com")
	adminWs := criarUser(t, svc, ctx, "adminws@exemplo.com")
	usuario := criarUser(t, svc, ctx, "usuario@exemplo.com")

	papelSuperUUID := uuid.New()
	atr.seedPapel(papelSuperUUID, papelSuperAdmin)
	ws := uuid.New()
	atr.seedAtribuicao(superOperador.UUID, ws, papelSuperUUID)
	ctx = orgctx.WithUser(ctx, superOperador.UUID)

	atribuirPapel(t, svc, atr, ctx, superOperador.UUID, adminWs.UUID, papelAdminWorkspace)

	ctxAdmin := orgctx.WithUser(ctx, adminWs.UUID)
	ctxSuper := orgctx.WithUser(ctx, superOperador.UUID)

	// admin_workspace NÃO pode atribuir papéis acima dele.
	papelAdminOrg := uuid.New()
	atr.seedPapel(papelAdminOrg, papelAdminOrganization)
	_, err := svc.AtribuirPapel(ctxAdmin, usuario.UUID, uuid.New(), papelAdminOrg)
	assert.ErrorIs(t, err, ErrHierarquiaInsufficiente, "admin_workspace não atribui admin_organization")

	papelSuper := uuid.New()
	atr.seedPapel(papelSuper, papelSuperAdmin)
	_, err = svc.AtribuirPapel(ctxAdmin, usuario.UUID, uuid.New(), papelSuper)
	assert.ErrorIs(t, err, ErrHierarquiaInsufficiente, "admin_workspace não atribui super_admin")

	// admin_workspace PODE atribuir papéis iguais ou inferiores a ele.
	papelUsuario := uuid.New()
	atr.seedPapel(papelUsuario, papelUsuarioWorkspace)
	atrib, err := svc.AtribuirPapel(ctxAdmin, usuario.UUID, uuid.New(), papelUsuario)
	require.NoError(t, err, "admin_workspace atribui usuario_workspace")

	// admin_workspace PODE remover atribuição de papel inferior a ele de um
	// usuário cuja hierarquia seja menor.
	err = svc.RemoverAtribuicao(ctxAdmin, usuario.UUID, atrib.UUID)
	assert.NoError(t, err, "admin_workspace remove usuario_workspace de usuário inferior")

	papelAdminWs := uuid.New()
	atr.seedPapel(papelAdminWs, papelAdminWorkspace)
	atribAdminWs, err := svc.AtribuirPapel(ctxAdmin, usuario.UUID, uuid.New(), papelAdminWs)
	require.NoError(t, err, "admin_workspace atribui admin_workspace a usuário inferior")

	// admin_workspace NÃO pode remover atribuição de um par (mesmo papel).
	outroAdmin := criarUser(t, svc, ctx, "outroadmin@exemplo.com")
	atribuirPapel(t, svc, atr, ctx, superOperador.UUID, outroAdmin.UUID, papelAdminWorkspace)
	itensPar, err := svc.Atribuicoes(ctxAdmin, outroAdmin.UUID)
	require.NoError(t, err)
	require.Len(t, itensPar, 1)
	err = svc.RemoverAtribuicao(ctxAdmin, outroAdmin.UUID, itensPar[0].UUID)
	assert.ErrorIs(t, err, ErrHierarquiaInsufficiente, "admin_workspace não remove atribuição de par")

	// super_admin pode remover qualquer atribuição.
	err = svc.RemoverAtribuicao(ctxSuper, usuario.UUID, atribAdminWs.UUID)
	assert.NoError(t, err, "super_admin remove atribuição de admin_workspace")
}

func atribuirPapel(t *testing.T, svc Service, atr *atrFake, ctx context.Context, operadorUUID, usuarioUUID uuid.UUID, papelNome string) {
	t.Helper()
	papelUUID := uuid.New()
	atr.seedPapel(papelUUID, papelNome)
	ws := uuid.New()
	_, err := svc.AtribuirPapel(orgctx.WithUser(ctx, operadorUUID), usuarioUUID, ws, papelUUID)
	require.NoError(t, err)
}

// ListarOpcoes emula a projeção real: workspace vence (via atribuições do
// dublê de atribuições quando disponível — aqui só org/sem filtro), org
// escopa, nenhum ponteiro devolve todos.
func (r *repoFake) ListarOpcoes(_ context.Context, organizacaoUUID, workspaceUUID *uuid.UUID) ([]modeluser.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	itens := make([]modeluser.User, 0)
	for _, u := range r.porUUID {
		switch {
		case workspaceUUID != nil:
			// atribuição não vive neste dublê — sem dado, ninguém casa
			continue
		case organizacaoUUID != nil && u.OrganizationUUID != *organizacaoUUID:
			continue
		}
		itens = append(itens, *u)
	}
	return itens, nil
}
