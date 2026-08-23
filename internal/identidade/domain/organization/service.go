package organization

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"workspace-api/internal/middleware"
	orgmodel "workspace-api/internal/identidade/model/organization"
	"workspace-api/internal/pkg/log/audit_log"
	"workspace-api/internal/pkg/orgctx"
	"workspace-api/internal/pkg/pagination"
)

// Service é o domain service dos agregados do subdomínio: TODA a regra que
// não cabe num método da entidade vive aqui — conferência de pertencimento,
// cascata de inativação (por contrato, nunca direto ao irmão), geração de
// chave de API e auditoria de toda escrita.
type Service interface {
	Create(ctx context.Context, in orgmodel.CreateInput) (*orgmodel.Organization, error)
	Read(ctx context.Context, id uuid.UUID) (*orgmodel.Organization, error)
	List(ctx context.Context, f orgmodel.ListFilter) ([]orgmodel.Organization, int64, error)
	Update(ctx context.Context, id uuid.UUID, in orgmodel.UpdateInput) (*orgmodel.Organization, error)
	Reativar(ctx context.Context, id uuid.UUID) (*orgmodel.Organization, error)
	Delete(ctx context.Context, id uuid.UUID) error
	DefinirDominio(ctx context.Context, id uuid.UUID, valor string) (*orgmodel.Organization, error)
	RemoverDominio(ctx context.Context, id uuid.UUID) (*orgmodel.Organization, error)
	ListarDominiosAtivos(ctx context.Context) ([]DominioRegistrado, error)

	CriarApiKey(ctx context.Context, organizationUUID uuid.UUID, in ApiKeyEntrada) (*orgmodel.ApiKey, string, error)
	ListarApiKeys(ctx context.Context, organizationUUID uuid.UUID, p pagination.Pagination) ([]orgmodel.ApiKey, int64, error)
	RevogarApiKey(ctx context.Context, organizationUUID, chaveUUID uuid.UUID) error
}

// ApiKeyEntrada carrega o dado cru do DTO para o service — o hash da chave é
// gerado AQUI dentro (o segredo em claro nunca atravessa camadas).
type ApiKeyEntrada struct {
	Nome                 string
	EscopoOrganization   bool
	WorkspacesPermitidos []uuid.UUID
	Permissoes           []string
	ExpiresAt            *time.Time
}

// DominioRegistrado é a projeção pública de um domínio custom ativo — o
// bootstrap traduz para o contrato do middleware/CORS.
type DominioRegistrado struct {
	Valor            string
	OrganizationUUID uuid.UUID
}

// permissaoGlobal é a forma de 2 segmentos do super_admin — o único curinga
// que não cabe na gramática dominio:subdominio:acao conferida pelo Atende.
const permissaoGlobal = "*:*"

// LeitorBaseDomain fornece o base_domain da plataforma sem acoplar o service
// ao singleton de config — o singleton.go injeta a leitura real; testes
// passam stub.
type LeitorBaseDomain func() (string, error)

type serviceImpl struct {
	repo       Repository
	chaves     RepositorioApiKeys
	suspensore SuspendedorWorkspaces
	sessoes    EncerradorSessoesUsuarios
	baseDomain LeitorBaseDomain
	trilha     audit_log.Destino // trilha de auditoria assíncrona (#9); nil = slog legado
}

// OpcaoServico adiciona peça opcional ao service na montagem (padrão da F4/
// E1): hoje, a trilha de auditoria assíncrona ligada pelo bootstrap.
type OpcaoServico func(*serviceImpl)

// ComTrilha liga o destino assíncrono da auditoria (evolução #9).
func ComTrilha(t audit_log.Destino) OpcaoServico {
	return func(s *serviceImpl) { s.trilha = t }
}

func NewService(repo Repository, chaves RepositorioApiKeys, suspensore SuspendedorWorkspaces, sessoes EncerradorSessoesUsuarios, baseDomain LeitorBaseDomain, opcoes ...OpcaoServico) Service {
	s := &serviceImpl{repo: repo, chaves: chaves, suspensore: suspensore, sessoes: sessoes, baseDomain: baseDomain}
	for _, aplicar := range opcoes {
		aplicar(s)
	}
	return s
}

// Criar: input cru → entidade VÁLIDA pelo construtor → persistência → auditoria.
// A organization é a RAIZ: não recebe escopo de ninguém.
func (s *serviceImpl) Create(ctx context.Context, in orgmodel.CreateInput) (*orgmodel.Organization, error) {
	o, err := orgmodel.NewOrganization(in)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Criar(ctx, o); err != nil {
		return nil, err
	}
	s.auditar(ctx, "criar", o.UUID, true)
	return o, nil
}

func (s *serviceImpl) Read(ctx context.Context, id uuid.UUID) (*orgmodel.Organization, error) {
	if err := exigirOrganizacaoDoContexto(ctx, id); err != nil {
		return nil, err
	}
	return s.repo.BuscarPorUUID(ctx, id)
}

func (s *serviceImpl) List(ctx context.Context, f orgmodel.ListFilter) ([]orgmodel.Organization, int64, error) {
	// Listagem GLOBAL — restrita por permissão (só super_admin tem :ler;
	// exceção documentada no AGENTS.md do pacote).
	return s.repo.Listar(ctx, f)
}

// Editar traduz o input em chamadas aos métodos de comportamento — nunca
// atribui campo direto. Inativar via PATCH dispara a cascata; reativação é
// ação própria (POST .../acoes/reativar), não PATCH de campo.
func (s *serviceImpl) Update(ctx context.Context, id uuid.UUID, in orgmodel.UpdateInput) (*orgmodel.Organization, error) {
	o, err := s.Read(ctx, id)
	if err != nil {
		return nil, err
	}
	inativou := false
	if in.Nome != nil {
		if err := o.Renomear(*in.Nome); err != nil {
			return nil, err
		}
	}
	if in.Status != nil && *in.Status == orgmodel.StatusInativo {
		// Inativar já-inativa é recusado (422) — transição de estado tem método próprio.
		if err := o.Inativar(); err != nil {
			return nil, err
		}
		inativou = true
	}
	var sessoesEncerradas, chavesRevogadas int64
	if inativou {
		// Cascata ANTES da persistência (fail-closed): se os acessos não podem
		// ser encerrados, a organization continua ativa — nunca pai inativo
		// com filho vivo ou credencial sobrevivendo à dona.
		sessoesEncerradas, chavesRevogadas, err = s.executarCascata(ctx, o.UUID)
		if err != nil {
			return nil, err
		}
	}
	if err := s.repo.Atualizar(ctx, o); err != nil {
		return nil, err
	}
	if inativou {
		s.auditar(ctx, "editar", o.UUID, true,
			"status", string(orgmodel.StatusInativo),
			"sessoes_encerradas", sessoesEncerradas,
			"apikeys_revogadas", chavesRevogadas)
		return o, nil
	}
	s.auditar(ctx, "editar", o.UUID, true)
	return o, nil
}

// Reativar devolve a organization ao ar; o domínio custom volta a resolver e
// os workspaces continuam suspensos até ação explícita sobre cada um.
func (s *serviceImpl) Reativar(ctx context.Context, id uuid.UUID) (*orgmodel.Organization, error) {
	o, err := s.Read(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := o.Reativar(); err != nil {
		return nil, err
	}
	if err := s.repo.Atualizar(ctx, o); err != nil {
		return nil, err
	}
	s.auditar(ctx, "reativar", o.UUID, true)
	return o, nil
}

// Remover é remoção LÓGICA com cascata COMPLETA: suspende os filhos, revoga
// as chaves de API e encerra as sessões antes de apagar a raiz. O domínio
// custom REMOVIDO não se libera (índice único total — evita takeover), e o
// provedor de resolução para de listá-lo imediatamente.
func (s *serviceImpl) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := s.Read(ctx, id); err != nil {
		return err
	}
	sessoes, chaves, err := s.executarCascata(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repo.Remover(ctx, id); err != nil {
		return err
	}
	s.auditar(ctx, "remover", id, true,
		"sessoes_encerradas", sessoes, "apikeys_revogadas", chaves)
	return nil
}

// DefinirDominio aponta o white-label — permissão SEPARADA de editar porque
// muda onde a plataforma responde. O VO valida contra o base_domain da
// plataforma; unicidade global vem do índice único TOTAL (23505 traduzido).
func (s *serviceImpl) DefinirDominio(ctx context.Context, id uuid.UUID, valor string) (*orgmodel.Organization, error) {
	o, err := s.Read(ctx, id)
	if err != nil {
		return nil, err
	}
	baseDomain, err := s.baseDomain()
	if err != nil {
		return nil, err
	}
	vo, err := orgmodel.ParseDominio(valor, baseDomain)
	if err != nil {
		return nil, err
	}
	o.DefinirDominio(vo)
	if err := s.repo.Atualizar(ctx, o); err != nil {
		return nil, err
	}
	s.auditar(ctx, "definir_dominio", o.UUID, true)
	return o, nil
}

func (s *serviceImpl) RemoverDominio(ctx context.Context, id uuid.UUID) (*orgmodel.Organization, error) {
	o, err := s.Read(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := o.RemoverDominio(); err != nil {
		return nil, err
	}
	if err := s.repo.Atualizar(ctx, o); err != nil {
		return nil, err
	}
	s.auditar(ctx, "remover_dominio", o.UUID, true)
	return o, nil
}

func (s *serviceImpl) ListarDominiosAtivos(ctx context.Context) ([]DominioRegistrado, error) {
	linhas, err := s.repo.ListarDominiosAtivos(ctx)
	if err != nil {
		return nil, err
	}
	lista := make([]DominioRegistrado, 0, len(linhas))
	for _, l := range linhas {
		lista = append(lista, DominioRegistrado{Valor: l.Valor, OrganizationUUID: l.OrganizationUUID})
	}
	return lista, nil
}

// CriarApiKey opera o registro-filho pela RAIZ: confere pertencimento, gera o
// segredo, monta a entidade válida, confere que o criador POSSUI cada
// permissão pedida e devolve a chave em claro UMA ÚNICA vez.
// A chave em claro NUNCA entra em log nem auditoria.
func (s *serviceImpl) CriarApiKey(ctx context.Context, organizationUUID uuid.UUID, in ApiKeyEntrada) (*orgmodel.ApiKey, string, error) {
	if err := exigirOrganizacaoDoContexto(ctx, organizationUUID); err != nil {
		return nil, "", err
	}
	chave, hash, err := orgmodel.NovaChaveSegredo()
	if err != nil {
		return nil, "", err
	}
	k, err := orgmodel.NewApiKey(orgmodel.CreateApiKeyInput{
		OrganizationUUID:     organizationUUID,
		Nome:                 in.Nome,
		KeyHash:              hash,
		EscopoOrganization:   in.EscopoOrganization,
		WorkspacesPermitidos: in.WorkspacesPermitidos,
		Permissoes:           in.Permissoes,
		ExpiresAt:            in.ExpiresAt,
	})
	if err != nil {
		return nil, "", err
	}
	// Escalação de privilégio é recusada ANTES da persistência: a chave nunca
	// concede poder que quem a criou não tem — *:* só vale para quem possui *:*.
	if err := exigirPermissoesPossuidas(ctx, in.Permissoes); err != nil {
		return nil, "", err
	}
	if err := s.chaves.CriarApiKey(ctx, k); err != nil {
		return nil, "", err
	}
	s.auditar(ctx, "criar_apikey", organizationUUID, true,
		"apikey_uuid", k.UUID.String(), "escopo_organization", k.EscopoOrganization)
	return k, chave, nil
}

func (s *serviceImpl) ListarApiKeys(ctx context.Context, organizationUUID uuid.UUID, p pagination.Pagination) ([]orgmodel.ApiKey, int64, error) {
	if err := exigirOrganizacaoDoContexto(ctx, organizationUUID); err != nil {
		return nil, 0, err
	}
	return s.chaves.ListarApiKeys(ctx, organizationUUID, p)
}

func (s *serviceImpl) RevogarApiKey(ctx context.Context, organizationUUID, chaveUUID uuid.UUID) error {
	if err := exigirOrganizacaoDoContexto(ctx, organizationUUID); err != nil {
		return err
	}
	if _, err := s.chaves.BuscarApiKeyPorUUID(ctx, organizationUUID, chaveUUID); err != nil {
		return err
	}
	if err := s.chaves.RemoverApiKey(ctx, organizationUUID, chaveUUID); err != nil {
		return err
	}
	s.auditar(ctx, "revogar_apikey", organizationUUID, true, "apikey_uuid", chaveUUID.String())
	return nil
}

// --- Internos -----------------------------------------------------------------

// executarCascata roda TODA a cascata de inativação/remoção (R4), na ordem:
// 1) workspaces suspensos PELO CONTRATO (contratos.go) — nunca chamada direta
// ao irmão; 2) chaves de API revogadas (registro-filho do MESMO agregado);
// 3) sessões dos usuários encerradas PELO CONTRATO. Tudo ANTES da
// persistência do novo estado da raiz: se qualquer passo falha, ela continua
// viva — o pior caso é acessos encerrados com pai ainda ativo (direção
// segura). Reativar NÃO desfaz: workspaces, chaves e sessões voltam só por
// ação explícita sobre cada um.
func (s *serviceImpl) executarCascata(ctx context.Context, organizationUUID uuid.UUID) (sessoesEncerradas, chavesRevogadas int64, err error) {
	if err := s.suspenderFilhos(ctx, organizationUUID); err != nil {
		return 0, 0, err
	}
	chavesRevogadas, err = s.chaves.RevogarApiKeysDaOrganization(ctx, organizationUUID)
	if err != nil {
		slog.ErrorContext(ctx, "organization.cascata_apikeys_falhou",
			"dominio", orgmodel.Dominio, "subdominio", orgmodel.Subdominio,
			"organization_uuid", organizationUUID.String(),
			"ray_trace", orgctx.RayTrace(ctx), "causa", err.Error())
		return 0, 0, err
	}
	sessoesEncerradas, err = s.sessoes.RevogarTokensDaOrganization(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "organization.cascata_sessoes_falhou",
			"dominio", orgmodel.Dominio, "subdominio", orgmodel.Subdominio,
			"organization_uuid", organizationUUID.String(),
			"ray_trace", orgctx.RayTrace(ctx), "causa", err.Error())
		return sessoesEncerradas, chavesRevogadas, err
	}
	if chavesRevogadas > 0 || sessoesEncerradas > 0 {
		slog.InfoContext(ctx, "organization.cascata_acessos_encerrados",
			"dominio", orgmodel.Dominio, "subdominio", orgmodel.Subdominio,
			"acao", "cascata_organization_inativada",
			"organization_uuid", organizationUUID.String(),
			"apikeys_revogadas", chavesRevogadas,
			"sessoes_encerradas", sessoesEncerradas)
	}
	return sessoesEncerradas, chavesRevogadas, nil
}

// suspenderFilhos é o passo workspace da cascata — o adaptador do bootstrap
// resolve o singleton na chamada; enquanto o workspace não existe (antes da
// F3), ele devolve 0 sem erro — não há filho vivo para suspender.
func (s *serviceImpl) suspenderFilhos(ctx context.Context, organizationUUID uuid.UUID) error {
	suspensos, err := s.suspensore.SuspenderPorOrganization(ctx, organizationUUID)
	if err != nil {
		slog.ErrorContext(ctx, "organization.cascata_falhou",
			"dominio", orgmodel.Dominio, "subdominio", orgmodel.Subdominio,
			"organization_uuid", organizationUUID.String(),
			"ray_trace", orgctx.RayTrace(ctx), "causa", err.Error())
		return err
	}
	if suspensos > 0 {
		slog.InfoContext(ctx, "organization.cascata_workspaces_suspensos",
			"dominio", orgmodel.Dominio, "subdominio", orgmodel.Subdominio,
			"organization_uuid", organizationUUID.String(), "quantidade", suspensos)
	}
	return nil
}

// exigirOrganizacaoDoContexto confere que a rota {uuid} aponta para a MESMA
// organization resolvida na requisição. Divergente ou ctx sem escopo =
// ErrNotFound — não vaza existência de organization alheia (mesma regra do
// workspace alheio no domínio do parceiro, doc 03).
func exigirOrganizacaoDoContexto(ctx context.Context, alvo uuid.UUID) error {
	atual := orgctx.OrganizationUUID(ctx)
	if atual == uuid.Nil || atual != alvo {
		return ErrNotFound
	}
	return nil
}

// exigirPermissoesPossuidas confere CADA permissão pedida para a chave contra
// as EFETIVAS do criador no ctx, com o MESMO matcher do RequirePermission
// (middleware.Atende): igualdade exata, curinga por segmento ou global.
// Assim um admin_organization não fabrica poderes além do papel dele. A
// forma global `*:*` tem DOIS segmentos — fora da gramática que o Atende
// exige da pedida — e é conferida por pertencimento exato: só quem possui
// `*:*` a concede. Curinga de pedida (`identidade:*:ler`) exige que o
// efetivo tenha o `*` naquele segmento — nunca o contrário. ctx sem
// permissões injetadas recusa tudo (fail-closed).
func exigirPermissoesPossuidas(ctx context.Context, pedidas []string) error {
	efetivas := orgctx.Permissoes(ctx)
	for _, p := range pedidas {
		possui := false
		if p == permissaoGlobal {
			for _, e := range efetivas {
				if e == permissaoGlobal {
					possui = true
					break
				}
			}
		} else {
			possui = middleware.Atende(efetivas, p)
		}
		if possui {
			continue
		}
		slog.WarnContext(ctx, "organization.apikey_permissao_recusada",
			"dominio", orgmodel.Dominio, "subdominio", orgmodel.Subdominio,
			"permissao_pedida", p, "user_uuid", orgctx.UserUUID(ctx).String(),
			"organization_uuid", orgctx.OrganizationUUID(ctx).String(),
			"ray_trace", orgctx.RayTrace(ctx))
		return ErrPermissaoNaoPossuida
	}
	return nil
}

// auditar registra toda ESCRITA na trilha assíncrona (#9) com payload
// montado à mão (doc 04): identificadores e vocabulário fechado, nunca texto
// livre — e nunca o valor de segredos. Sem trilha ligada (montagem direta em
// teste), cai para o slog legado — mesmo payload, caminho síncrono.
func (s *serviceImpl) auditar(ctx context.Context, acao string, organizationUUID uuid.UUID, success bool, extras ...any) {
	validarAcaoCatalogada(acao) // ação fora do events.go reprova em teste/boot
	evento := audit_log.Evento{
		Instante:         time.Now().UTC(),
		Dominio:          orgmodel.Dominio,
		Subdominio:       orgmodel.Subdominio,
		Acao:             acao,
		Sucesso:          success,
		OrganizationUUID: organizationUUID.String(),
		UserUUID:         orgctx.UserUUID(ctx).String(),
		RayTrace:         orgctx.RayTrace(ctx),
		Detalhes:         audit_log.Detalhes(extras...),
	}
	if s.trilha != nil {
		s.trilha.Registrar(evento)
		return
	}
	args := []any{
		"dominio", orgmodel.Dominio, "subdominio", orgmodel.Subdominio, "acao", acao,
		"organization_uuid", organizationUUID.String(),
		"user_uuid", orgctx.UserUUID(ctx).String(),
		"ray_trace", orgctx.RayTrace(ctx),
		"success", success,
	}
	args = append(args, extras...)
	slog.InfoContext(ctx, "organization."+acao, args...)
}
