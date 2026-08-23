package organization

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	orgmodel "workspace-api/internal/identidade/model/organization"
	"workspace-api/internal/pkg/orgctx"
	"workspace-api/internal/pkg/pagination"
)

// --- Dublês (regra: testes de service NUNCA passam pelo singleton) ----------

type repoFake struct {
	porUUID      map[uuid.UUID]*orgmodel.Organization
	erroAoSalvar error // sentinela injetada para simular conflito de unicidade
}

func novoRepoFake() *repoFake { return &repoFake{porUUID: map[uuid.UUID]*orgmodel.Organization{}} }

func (r *repoFake) Criar(ctx context.Context, o *orgmodel.Organization) error {
	if r.erroAoSalvar != nil {
		return r.erroAoSalvar
	}
	copia := *o // guarda CÓPIA: mutação em memória depois não vira persistência
	r.porUUID[o.UUID] = &copia
	return nil
}

func (r *repoFake) BuscarPorUUID(ctx context.Context, id uuid.UUID) (*orgmodel.Organization, error) {
	if o, ok := r.porUUID[id]; ok {
		copia := *o // entidade carregada é CÓPIA: só Atualizar persiste mutação
		return &copia, nil
	}
	return nil, ErrNotFound
}

func (r *repoFake) Listar(ctx context.Context, f orgmodel.ListFilter) ([]orgmodel.Organization, int64, error) {
	items := make([]orgmodel.Organization, 0, len(r.porUUID))
	for _, o := range r.porUUID {
		items = append(items, *o)
	}
	return items, int64(len(items)), nil
}

func (r *repoFake) Atualizar(ctx context.Context, o *orgmodel.Organization) error {
	if r.erroAoSalvar != nil {
		return r.erroAoSalvar
	}
	copia := *o // guarda CÓPIA: só Atualizar persiste o novo estado
	r.porUUID[o.UUID] = &copia
	return nil
}

func (r *repoFake) Remover(ctx context.Context, id uuid.UUID) error {
	delete(r.porUUID, id)
	return nil
}

func (r *repoFake) ListarDominiosAtivos(ctx context.Context) ([]LinhaDominioAtivo, error) {
	lista := []LinhaDominioAtivo{}
	for _, o := range r.porUUID {
		if o.Status == orgmodel.StatusAtivo && o.TemDominio() {
			lista = append(lista, LinhaDominioAtivo{Valor: o.Dominio.String(), OrganizationUUID: o.UUID})
		}
	}
	return lista, nil
}

type chavesFake struct {
	porOrg map[uuid.UUID][]*orgmodel.ApiKey
	// Registro da cascata (R4): quantas chaves foram revogadas em cascata,
	// sobre qual organization e qual erro forçado.
	revogadasEmCascata int64
	alvoUltimaCascata  uuid.UUID
	erroNaCascata      error
}

func novasChavesFake() *chavesFake { return &chavesFake{porOrg: map[uuid.UUID][]*orgmodel.ApiKey{}} }

func (c *chavesFake) CriarApiKey(ctx context.Context, k *orgmodel.ApiKey) error {
	c.porOrg[k.OrganizationUUID] = append(c.porOrg[k.OrganizationUUID], k)
	return nil
}

func (c *chavesFake) ListarApiKeys(ctx context.Context, organizationUUID uuid.UUID, p pagination.Pagination) ([]orgmodel.ApiKey, int64, error) {
	todas := c.porOrg[organizationUUID]
	items := make([]orgmodel.ApiKey, 0, len(todas))
	for _, k := range todas {
		items = append(items, *k)
	}
	return items, int64(len(items)), nil
}

func (c *chavesFake) BuscarApiKeyPorUUID(ctx context.Context, organizationUUID, chaveUUID uuid.UUID) (*orgmodel.ApiKey, error) {
	for _, k := range c.porOrg[organizationUUID] {
		if k.UUID == chaveUUID {
			return k, nil
		}
	}
	return nil, ErrApiKeyNaoEncontrada
}

func (c *chavesFake) BuscarApiKeyPorHash(ctx context.Context, hash string) (*orgmodel.ApiKey, error) {
	for _, ks := range c.porOrg {
		for _, k := range ks {
			if k.KeyHash == hash {
				return k, nil
			}
		}
	}
	return nil, ErrApiKeyNaoEncontrada
}

func (c *chavesFake) RemoverApiKey(ctx context.Context, organizationUUID, chaveUUID uuid.UUID) error {
	resto := c.porOrg[organizationUUID][:0]
	for _, k := range c.porOrg[organizationUUID] {
		if k.UUID != chaveUUID {
			resto = append(resto, k)
		}
	}
	if len(resto) == len(c.porOrg[organizationUUID]) {
		return ErrApiKeyNaoEncontrada
	}
	c.porOrg[organizationUUID] = resto
	return nil
}

func (c *chavesFake) RevogarApiKeysDaOrganization(_ context.Context, organizationUUID uuid.UUID) (int64, error) {
	if c.erroNaCascata != nil {
		return 0, c.erroNaCascata
	}
	c.revogadasEmCascata += int64(len(c.porOrg[organizationUUID]))
	c.alvoUltimaCascata = organizationUUID
	c.porOrg[organizationUUID] = nil
	return c.revogadasEmCascata, nil
}

type suspensorFake struct {
	chamadas int
	ultimo   uuid.UUID
	erro     error
}

func (s *suspensorFake) SuspenderPorOrganization(ctx context.Context, organizationUUID uuid.UUID) (int, error) {
	s.chamadas++
	s.ultimo = organizationUUID
	if s.erro != nil {
		return 0, s.erro
	}
	return s.chamadas, nil
}

// encerradorFake é o dublê do contrato com o subdomínio user (R4): registra a
// organization ALVO PELO CTX — exatamente como o repositório real escopa.
type encerradorFake struct {
	chamadas     int
	ultimoCtxOrg uuid.UUID
	revogados    int64
	erro         error
}

func (e *encerradorFake) RevogarTokensDaOrganization(ctx context.Context) (int64, error) {
	e.chamadas++
	e.ultimoCtxOrg = orgctx.OrganizationUUID(ctx)
	if e.erro != nil {
		return 0, e.erro
	}
	return e.revogados, nil
}

// --- Suíte -------------------------------------------------------------------

const baseDomainTeste = "plataforma.exemplo"

func montarServico(t *testing.T) (Service, *repoFake, *chavesFake, *suspensorFake, *encerradorFake) {
	t.Helper()
	repo := novoRepoFake()
	chaves := novasChavesFake()
	suspensore := &suspensorFake{}
	sessoes := &encerradorFake{}
	svc := NewService(repo, chaves, suspensore, sessoes, func() (string, error) { return baseDomainTeste, nil })
	return svc, repo, chaves, suspensore, sessoes
}

func ctxDaOrganizacao(id uuid.UUID) context.Context {
	return orgctx.WithOrganization(context.Background(), id)
}

func TestCreateValidaInvariantsPeloConstrutor(t *testing.T) {
	svc, repo, _, _, _ := montarServico(t)

	o, err := svc.Create(context.Background(), orgmodel.CreateInput{Nome: "Acme"})
	require.NoError(t, err)
	assert.Equal(t, orgmodel.StatusAtivo, o.Status)
	require.Len(t, repo.porUUID, 1)

	_, err = svc.Create(context.Background(), orgmodel.CreateInput{Nome: "a"})
	assert.ErrorIs(t, err, orgmodel.ErrNomeInvalido)

	repo.erroAoSalvar = ErrDominioEmUso
	_, err = svc.Create(context.Background(), orgmodel.CreateInput{Nome: "Outra"})
	assert.ErrorIs(t, err, ErrDominioEmUso)
}

func TestLeituraNaoVazaOrganizacaoAlheia(t *testing.T) {
	svc, _, _, _, _ := montarServico(t)
	alvo, err := svc.Create(context.Background(), orgmodel.CreateInput{Nome: "Alvo"})
	require.NoError(t, err)

	casos := []struct {
		nome string
		ctx  context.Context
	}{
		{"sem escopo no ctx", context.Background()},
		{"organization alheia no ctx", ctxDaOrganizacao(uuid.New())},
	}
	for _, caso := range casos {
		_, err := svc.Read(caso.ctx, alvo.UUID)
		assert.ErrorIs(t, err, ErrNotFound, caso.nome, "não vaza existência")
	}

	vista, err := svc.Read(ctxDaOrganizacao(alvo.UUID), alvo.UUID)
	require.NoError(t, err)
	assert.Equal(t, alvo.UUID, vista.UUID)
}

func TestUpdateInativaComCascataERecusaRepeticao(t *testing.T) {
	svc, _, _, suspensore, _ := montarServico(t)
	o, err := svc.Create(context.Background(), orgmodel.CreateInput{Nome: "Acme"})
	require.NoError(t, err)
	ctx := ctxDaOrganizacao(o.UUID)

	inativo := orgmodel.StatusInativo
	atualizada, err := svc.Update(ctx, o.UUID, orgmodel.UpdateInput{Nome: ptrTexto("Acme Ltda"), Status: &inativo})
	require.NoError(t, err)
	assert.Equal(t, orgmodel.StatusInativo, atualizada.Status)
	assert.Equal(t, 1, suspensore.chamadas, "inativar dispara a cascata UMA vez")
	assert.Equal(t, o.UUID, suspensore.ultimo)

	_, err = svc.Update(ctx, o.UUID, orgmodel.UpdateInput{Status: &inativo})
	assert.ErrorIs(t, err, orgmodel.ErrJaInativo)
	assert.Equal(t, 1, suspensore.chamadas, "invariante violada nem chega à cascata")

	// Cascata falhando impede a persistência — pai nunca fica inativo com
	// filho vivo.
	suspensorQuebrado := &suspensorFake{erro: assert.AnError}
	svcQuebrado := NewService(novoRepoFake(), novasChavesFake(), suspensorQuebrado, &encerradorFake{},
		func() (string, error) { return baseDomainTeste, nil })
	outro, err := svcQuebrado.Create(context.Background(), orgmodel.CreateInput{Nome: "Fragil"})
	require.NoError(t, err)
	_, err = svcQuebrado.Update(ctxDaOrganizacao(outro.UUID), outro.UUID,
		orgmodel.UpdateInput{Status: &inativo})
	assert.ErrorIs(t, err, assert.AnError)
}

// R4 (issue #22): inativar a organization encerra TODOS os acessos dela —
// chaves de API (mesmo agregado) e sessões dos usuários (contrato com o
// subdomínio user) — e o alvo da cascata é A organization do ctx.
func TestInativacaoRevogaChavesESessoesNaCascata(t *testing.T) {
	svc, _, chaves, suspensore, sessoes := montarServico(t)
	o, err := svc.Create(context.Background(), orgmodel.CreateInput{Nome: "Acme"})
	require.NoError(t, err)

	ctxAdmin := orgctx.WithPermissoes(ctxDaOrganizacao(o.UUID), []string{"*:*"})
	for i := range 2 {
		_, _, err = svc.CriarApiKey(ctxAdmin, o.UUID, ApiKeyEntrada{
			Nome:               "chave " + string(rune('a'+i)),
			EscopoOrganization: true,
			Permissoes:         []string{"identidade:workspace:ler"},
		})
		require.NoError(t, err)
	}
	sessoes.revogados = 3 // dublê devolve "3 sessões encerradas"

	inativo := orgmodel.StatusInativo
	_, err = svc.Update(ctxDaOrganizacao(o.UUID), o.UUID, orgmodel.UpdateInput{Status: &inativo})
	require.NoError(t, err)
	assert.Equal(t, 1, suspensore.chamadas)
	assert.Equal(t, int64(2), chaves.revogadasEmCascata, "todas as chaves ativas saem do ar")
	assert.Empty(t, chaves.porOrg[o.UUID])
	assert.Equal(t, 1, sessoes.chamadas, "sessões encerradas UMA vez")
	assert.Equal(t, o.UUID, sessoes.ultimoCtxOrg, "alvo da cascata é a organization do ctx")

	// Inativar de novo é recusado ANTES da cascata — nada roda duas vezes.
	_, err = svc.Update(ctxDaOrganizacao(o.UUID), o.UUID, orgmodel.UpdateInput{Status: &inativo})
	assert.ErrorIs(t, err, orgmodel.ErrJaInativo)
	assert.Equal(t, 1, sessoes.chamadas)
	assert.EqualValues(t, 2, chaves.revogadasEmCascata)
}

func TestDeleteTambemExecutaACascataDeAcessos(t *testing.T) {
	svc, repo, chaves, suspensore, sessoes := montarServico(t)
	o, err := svc.Create(context.Background(), orgmodel.CreateInput{Nome: "Acme"})
	require.NoError(t, err)
	ctxAdmin := orgctx.WithPermissoes(ctxDaOrganizacao(o.UUID), []string{"*:*"})
	_, _, err = svc.CriarApiKey(ctxAdmin, o.UUID, ApiKeyEntrada{
		Nome:               "integração",
		EscopoOrganization: true,
		Permissoes:         []string{"identidade:workspace:ler"},
	})
	require.NoError(t, err)

	require.NoError(t, svc.Delete(ctxDaOrganizacao(o.UUID), o.UUID))
	assert.Equal(t, 1, suspensore.chamadas)
	assert.Equal(t, 1, sessoes.chamadas)
	assert.EqualValues(t, 1, chaves.revogadasEmCascata)
	assert.NotContains(t, repo.porUUID, o.UUID)

	assert.ErrorIs(t, svc.Delete(ctxDaOrganizacao(uuid.New()), uuid.New()), ErrNotFound)
}

// R4 fail-closed: qualquer passo da cascata falhando impede a persistência do
// novo estado — a organization continua ATIVA no repositório (o pior caso é
// acessos encerrados com pai vivo, direção segura).
func TestCascataFalhaImpedePersistirInativacao(t *testing.T) {
	casos := []struct {
		nome   string
		quebra func(*repoFake, *chavesFake, *encerradorFake)
	}{
		{
			nome:   "encerrador de sessões falhou",
			quebra: func(_ *repoFake, _ *chavesFake, e *encerradorFake) { e.erro = assert.AnError },
		},
		{
			nome:   "revogação de chaves falhou",
			quebra: func(_ *repoFake, c *chavesFake, _ *encerradorFake) { c.erroNaCascata = assert.AnError },
		},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			repo, chaves, sessoes := novoRepoFake(), novasChavesFake(), &encerradorFake{}
			caso.quebra(repo, chaves, sessoes)
			svc := NewService(repo, chaves, &suspensorFake{}, sessoes,
				func() (string, error) { return baseDomainTeste, nil })
			o, err := svc.Create(context.Background(), orgmodel.CreateInput{Nome: "Fragil"})
			require.NoError(t, err)

			inativo := orgmodel.StatusInativo
			_, err = svc.Update(ctxDaOrganizacao(o.UUID), o.UUID, orgmodel.UpdateInput{Status: &inativo})
			assert.ErrorIs(t, err, assert.AnError)
			assert.Equal(t, orgmodel.StatusAtivo, repo.porUUID[o.UUID].Status,
				"organization NÃO é persistida inativa com a cascata quebrada")
		})
	}
}

func TestReativarETransicaoUnica(t *testing.T) {
	svc, _, _, _, _ := montarServico(t)
	o, err := svc.Create(context.Background(), orgmodel.CreateInput{Nome: "Acme"})
	require.NoError(t, err)
	ctx := ctxDaOrganizacao(o.UUID)

	_, err = svc.Reativar(ctx, o.UUID)
	assert.ErrorIs(t, err, orgmodel.ErrJaAtivo, "reativar ativa é recusado")

	inativo := orgmodel.StatusInativo
	_, err = svc.Update(ctx, o.UUID, orgmodel.UpdateInput{Status: &inativo})
	require.NoError(t, err)

	reativada, err := svc.Reativar(ctx, o.UUID)
	require.NoError(t, err)
	assert.Equal(t, orgmodel.StatusAtivo, reativada.Status)
}

func TestDeleteExecutaCascataAntesDaRemocao(t *testing.T) {
	svc, repo, _, suspensore, _ := montarServico(t)
	o, err := svc.Create(context.Background(), orgmodel.CreateInput{Nome: "Acme"})
	require.NoError(t, err)

	require.NoError(t, svc.Delete(ctxDaOrganizacao(o.UUID), o.UUID))
	assert.Equal(t, 1, suspensore.chamadas)
	assert.NotContains(t, repo.porUUID, o.UUID)

	assert.ErrorIs(t, svc.Delete(ctxDaOrganizacao(uuid.New()), uuid.New()), ErrNotFound)
}

func TestDefinirEDepoisRemoverDominioCustom(t *testing.T) {
	svc, repo, _, _, _ := montarServico(t)
	o, err := svc.Create(context.Background(), orgmodel.CreateInput{Nome: "Acme"})
	require.NoError(t, err)
	ctx := ctxDaOrganizacao(o.UUID)

	recusados := []struct{ valor, motivo string }{
		{"plataforma.exemplo", "igual ao base_domain"},
		{"sub.plataforma.exemplo", "descendente do base_domain"},
		{"co.uk", "public suffix puro"},
		{"localhost", "rótulo único"},
		{"ruim_.com", "formato DNS"},
	}
	for _, caso := range recusados {
		_, err := svc.DefinirDominio(ctx, o.UUID, caso.valor)
		assert.ErrorIs(t, err, orgmodel.ErrDominioInvalido, "%s deveria ser recusado (%s)", caso.valor, caso.motivo)
	}

	_, err = svc.DefinirDominio(ctx, o.UUID, " Parceiro.COM ")
	require.NoError(t, err)
	assert.Equal(t, "parceiro.com", repo.porUUID[o.UUID].Dominio.String())

	registrados, err := svc.ListarDominiosAtivos(ctx)
	require.NoError(t, err)
	require.Len(t, registrados, 1)
	assert.Equal(t, "parceiro.com", registrados[0].Valor)
	assert.Equal(t, o.UUID, registrados[0].OrganizationUUID)

	repo.erroAoSalvar = ErrDominioEmUso
	_, err = svc.DefinirDominio(ctx, o.UUID, "outro.com")
	assert.ErrorIs(t, err, ErrDominioEmUso, "unicidade global vem do índice único")
	repo.erroAoSalvar = nil

	_, err = svc.RemoverDominio(ctx, o.UUID)
	require.NoError(t, err)
	_, err = svc.RemoverDominio(ctx, o.UUID)
	assert.ErrorIs(t, err, orgmodel.ErrDominioNaoDefinido)

	registrados, err = svc.ListarDominiosAtivos(ctx)
	require.NoError(t, err)
	assert.Empty(t, registrados)
}

func TestCicloCompletoDaApiKey(t *testing.T) {
	svc, _, chaves, _, _ := montarServico(t)
	o, err := svc.Create(context.Background(), orgmodel.CreateInput{Nome: "Acme"})
	require.NoError(t, err)
	// O criador só consegue conceder permissões que POSSUI (R1): o ctx da
	// requisição carrega as efetivas injetadas pela cadeia do middleware.
	ctx := orgctx.WithPermissoes(ctxDaOrganizacao(o.UUID), []string{"identidade:workspace:ler"})

	// Chave em claro nasce uma única vez e nunca coincide com o hash.
	k, chave, err := svc.CriarApiKey(ctx, o.UUID, ApiKeyEntrada{
		Nome:               "integração",
		EscopoOrganization: true,
		Permissoes:         []string{"identidade:workspace:ler"},
	})
	require.NoError(t, err)
	assert.NotEqual(t, chave, k.KeyHash, "hash persistido não expõe a chave")
	assert.Contains(t, chave, "wka_")

	_, _, err = svc.CriarApiKey(ctx, o.UUID, ApiKeyEntrada{
		Nome:                 "loja",
		WorkspacesPermitidos: []uuid.UUID{uuid.New()},
		Permissoes:           []string{"identidade:workspace:ler"},
		ExpiresAt:            ptrTempo(time.Now().UTC().Add(time.Hour)),
	})
	require.NoError(t, err)

	itens, total, err := svc.ListarApiKeys(ctx, o.UUID, pagination.Pagination{})
	require.NoError(t, err)
	assert.EqualValues(t, 2, total)
	require.Len(t, itens, 2)
	require.Len(t, chaves.porOrg[o.UUID], 2)

	// Cross-org: nem ler nem revogar.
	forasteiro := ctxDaOrganizacao(uuid.New())
	_, _, err = svc.CriarApiKey(forasteiro, o.UUID, ApiKeyEntrada{Nome: "x", EscopoOrganization: true, Permissoes: []string{"*:*"}})
	assert.ErrorIs(t, err, ErrNotFound)
	_, _, err = svc.ListarApiKeys(forasteiro, o.UUID, pagination.Pagination{})
	assert.ErrorIs(t, err, ErrNotFound)
	assert.ErrorIs(t, svc.RevogarApiKey(forasteiro, o.UUID, k.UUID), ErrNotFound)

	// Entradas inválidas batem nas sentinelas do modelo.
	_, _, err = svc.CriarApiKey(ctx, o.UUID, ApiKeyEntrada{
		Nome:               "ruim",
		EscopoOrganization: true,
		Permissoes:         []string{"workspace:ler"},
	})
	assert.ErrorIs(t, err, orgmodel.ErrPermissaoInvalida)
	_, _, err = svc.CriarApiKey(ctx, o.UUID, ApiKeyEntrada{Nome: "sem escopo", Permissoes: []string{"*:*"}})
	assert.ErrorIs(t, err, orgmodel.ErrEscopoApiKeyInvalido)

	require.NoError(t, svc.RevogarApiKey(ctx, o.UUID, k.UUID))
	assert.ErrorIs(t, svc.RevogarApiKey(ctx, o.UUID, k.UUID), ErrApiKeyNaoEncontrada, "revogar duas vezes não encontra")
	require.Len(t, chaves.porOrg[o.UUID], 1)
}

// R1 (issue #19): a chave nunca concede poder que quem a criou não tem —
// cada permissão pedida é casada contra as EFETIVAS do ctx com o mesmo
// matcher do RequirePermission (middleware.Atende); *:* só vale para quem
// possui *:*.
func TestCriarApiKeyNaoEscalaPrivilegio(t *testing.T) {
	svc, _, chaves, _, _ := montarServico(t)
	o, err := svc.Create(context.Background(), orgmodel.CreateInput{Nome: "Acme"})
	require.NoError(t, err)

	// Conjunto efetivo do admin_organization no seed: curingas de workspace e
	// user + a ÚNICA ação de organization que ele recebe (gerenciar_apikeys).
	adminOrganization := []string{
		"identidade:workspace:*", "identidade:user:*",
		"identidade:organization:gerenciar_apikeys", "identidade:catalogo:ler",
	}

	casos := []struct {
		nome     string
		efetivas []string // nil = ctx sem permissões injetadas
		pedidas  []string
		recusada bool
	}{
		{
			nome:     "admin_organization tentando *:*",
			efetivas: adminOrganization,
			pedidas:  []string{"*:*"},
			recusada: true,
		},
		{
			nome:     "admin_organization pedindo ação de organization fora do papel",
			efetivas: adminOrganization,
			pedidas:  []string{"identidade:workspace:ler", "identidade:organization:criar"},
			recusada: true,
		},
		{
			nome:     "ctx sem permissões injetadas recusa tudo (fail-closed)",
			efetivas: nil,
			pedidas:  []string{"identidade:workspace:ler"},
			recusada: true,
		},
		{
			nome:     "admin_organization concede o que possui via curinga de subdomínio",
			efetivas: adminOrganization,
			pedidas:  []string{"identidade:user:remover"},
			recusada: false,
		},
		{
			nome:     "super_admin (*:*) pode conceder *:*",
			efetivas: []string{"*:*"},
			pedidas:  []string{"*:*"},
			recusada: false,
		},
	}

	for _, caso := range casos {
		ctx := ctxDaOrganizacao(o.UUID)
		if caso.efetivas != nil {
			ctx = orgctx.WithPermissoes(ctx, caso.efetivas)
		}
		_, _, err := svc.CriarApiKey(ctx, o.UUID, ApiKeyEntrada{
			Nome:               "chave de " + caso.nome,
			EscopoOrganization: true,
			Permissoes:         caso.pedidas,
		})
		if caso.recusada {
			assert.ErrorIs(t, err, ErrPermissaoNaoPossuida, caso.nome)
			continue
		}
		require.NoError(t, err, caso.nome)
	}
	// Só os dois casos atendidos chegaram ao repositório.
	assert.Len(t, chaves.porOrg[o.UUID], 2, "nenhuma chave recusada chega a persistir")
}

func ptrTexto(s string) *string       { return &s }
func ptrTempo(t time.Time) *time.Time { return &t }
