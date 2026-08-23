package workspace

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	modelworkspace "workspace-api/internal/identidade/model/workspace"
	"workspace-api/internal/pkg/log/audit_log"
	"workspace-api/internal/pkg/orgctx"
)

// Service é o domain service do agregado: TODA a regra que não cabe num
// método da entidade vive aqui — lista de slugs reservados, unicidade
// amigável (além do 23505 traduzido no repository), resolução global por
// slug/uuid (exceções documentadas), cascata de suspensão e auditoria de
// toda escrita. Formato de slug NÃO mora aqui: é invariante do VO.
type Service interface {
	Create(ctx context.Context, in modelworkspace.CreateInput) (*modelworkspace.Workspace, error)
	Read(ctx context.Context, id uuid.UUID) (*modelworkspace.Workspace, error)
	List(ctx context.Context, f modelworkspace.ListFilter) ([]modelworkspace.Workspace, int64, error)
	Update(ctx context.Context, id uuid.UUID, in modelworkspace.UpdateInput) (*modelworkspace.Workspace, error)
	Reativar(ctx context.Context, id uuid.UUID) (*modelworkspace.Workspace, error)
	Delete(ctx context.Context, id uuid.UUID) error

	// SuspenderPorOrganization é o lado workspace da cascata organization →
	// workspace (contrato declarado no irmão, ligado no cmd/bootstrap):
	// inativar/remover a organization suspende os workspaces dela — filho
	// nunca fica mais vivo que o pai. Devolve quantos foram suspensos.
	SuspenderPorOrganization(ctx context.Context, organizationUUID uuid.UUID) (int, error)

	// ResolverPorSlug e ResolverPorUUID alimentam o ResolveWorkspace do
	// middleware (Host e fallback X-Workspace-Id): consultas GLOBAIS por
	// natureza — acontecem antes de existir escopo; resultado nunca exposto
	// em rota de administração.
	ResolverPorSlug(ctx context.Context, slug string) (*Resolvido, error)
	ResolverPorUUID(ctx context.Context, id uuid.UUID) (*Resolvido, error)

	// SlugsFixos expõe os rótulos reservados — endereços fixos da plataforma;
	// o middleware pergunta a este subdomínio, nunca o contrário.
	SlugsFixos() []string
}

// Resolvido é a projeção da resolução {slug}/{uuid} → workspace: só o que o
// middleware precisa (identificadores + vitalidade). Cache carrega cópia,
// não verdade — nada da entidade inteira atravessa aqui.
type Resolvido struct {
	UUID             uuid.UUID
	OrganizationUUID uuid.UUID
	Slug             string
	Status           string
}

func (r Resolvido) Ativo() bool { return r.Status == string(modelworkspace.StatusAtivo) }

type serviceImpl struct {
	repo   Repository
	cache  CacheResolucao    // nil é operação normal: sem cache, só mais caro
	trilha audit_log.Destino // trilha de auditoria assíncrona (#9); nil = slog legado
}

// OpcaoServico adiciona peça opcional ao service na montagem (padrão F4/E1):
// hoje, a trilha de auditoria assíncrona ligada pelo bootstrap.
type OpcaoServico func(*serviceImpl)

// ComTrilha liga o destino assíncrono da auditoria (evolução #9).
func ComTrilha(t audit_log.Destino) OpcaoServico {
	return func(s *serviceImpl) { s.trilha = t }
}

func NewService(repo Repository, cache CacheResolucao, opcoes ...OpcaoServico) Service {
	s := &serviceImpl{repo: repo, cache: cache}
	for _, aplicar := range opcoes {
		aplicar(s)
	}
	// Observação de erros (evolução errobserve): todo retorno de erro do
	// service passa pelo observador do subdomínio — erro sai intacto.
	return serviceObservado{Service: s, obs: observadorErros}
}

// slugsFixos — endereços fixos da plataforma; Host com um deles NUNCA resolve
// workspace (doc 03, regra 8). A lista mora neste subdomínio.
var slugsFixos = []string{"www", "api", "app", "admin", "docs", "status", "mail", "suporte", "painel"}

var conjuntosFixos = func() map[string]bool {
	conjunto := make(map[string]bool, len(slugsFixos))
	for _, s := range slugsFixos {
		conjunto[s] = true
	}
	return conjunto
}()

// Create: input cru → escopo do ctx → entidade VÁLIDA pelo construtor →
// regras → persistência → invalidação de cache → auditoria.
func (s *serviceImpl) Create(ctx context.Context, in modelworkspace.CreateInput) (*modelworkspace.Workspace, error) {
	in.OrganizationUUID = orgctx.OrganizationUUID(ctx) // escopo vem do ctx, NUNCA do corpo
	w, err := modelworkspace.NewWorkspace(in)
	if err != nil {
		return nil, err
	}
	if conjuntosFixos[w.Slug.String()] {
		return nil, ErrSlugReservado
	}
	if err := s.repo.Criar(ctx, w); err != nil {
		return nil, err
	}
	s.auditar(ctx, "criar", w.UUID, w.OrganizationUUID, true,
		"slug", w.Slug.String())
	return w, nil
}

func (s *serviceImpl) Read(ctx context.Context, id uuid.UUID) (*modelworkspace.Workspace, error) {
	return s.repo.BuscarPorUUID(ctx, id)
}

func (s *serviceImpl) List(ctx context.Context, f modelworkspace.ListFilter) ([]modelworkspace.Workspace, int64, error) {
	return s.repo.Listar(ctx, f)
}

// Update traduz o input em chamadas aos métodos de comportamento — nunca
// atribui campo direto. Inativar via PATCH suspende a resolução pelo Host na
// hora (cache invalidado); reativação é ação própria, não PATCH de campo.
func (s *serviceImpl) Update(ctx context.Context, id uuid.UUID, in modelworkspace.UpdateInput) (*modelworkspace.Workspace, error) {
	w, err := s.repo.BuscarPorUUID(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.Nome != nil {
		if err := w.Renomear(*in.Nome); err != nil {
			return nil, err
		}
	}
	inativou := false
	if in.Status != nil && *in.Status == modelworkspace.StatusInativo {
		if err := w.Inativar(); err != nil {
			return nil, err
		}
		inativou = true
	}
	if err := s.repo.Atualizar(ctx, w); err != nil {
		return nil, err
	}
	if inativou && s.cache != nil {
		s.cache.Invalidar(ctx, w.Slug.String())
	}
	s.auditar(ctx, "editar", w.UUID, w.OrganizationUUID, true,
		"slug", w.Slug.String(), "inativo", inativou)
	return w, nil
}

// Reativar devolve o workspace ao ar; o cache é invalidado para a resolução
// voltar imediatamente (nunca esperar TTL).
func (s *serviceImpl) Reativar(ctx context.Context, id uuid.UUID) (*modelworkspace.Workspace, error) {
	w, err := s.repo.BuscarPorUUID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := w.Reativar(); err != nil {
		return nil, err
	}
	if err := s.repo.Atualizar(ctx, w); err != nil {
		return nil, err
	}
	if s.cache != nil {
		s.cache.Invalidar(ctx, w.Slug.String())
	}
	s.auditar(ctx, "reativar", w.UUID, w.OrganizationUUID, true,
		"slug", w.Slug.String())
	return w, nil
}

// Remover é remoção LÓGICA; o slug removido NÃO se libera (índice único
// TOTAL — evita takeover de endereço por outro tenant).
func (s *serviceImpl) Delete(ctx context.Context, id uuid.UUID) error {
	w, err := s.repo.BuscarPorUUID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repo.Remover(ctx, id); err != nil {
		return err
	}
	if s.cache != nil {
		s.cache.Invalidar(ctx, w.Slug.String())
	}
	s.auditar(ctx, "remover", w.UUID, w.OrganizationUUID, true,
		"slug", w.Slug.String())
	return nil
}

// SuspenderPorOrganization roda a cascata PELO CONTRATO — o adaptador do
// cmd/bootstrap resolve o singleton deste subdomínio NA CHAMADA. Idempotente:
// sem workspaces ativos devolve 0 sem erro.
func (s *serviceImpl) SuspenderPorOrganization(ctx context.Context, organizationUUID uuid.UUID) (int, error) {
	suspensos, err := s.repo.SuspenderPorOrganization(ctx, organizationUUID)
	if err != nil {
		return 0, err
	}
	if suspensos > 0 {
		if s.cache != nil {
			s.cache.InvalidarOrganization(ctx, organizationUUID.String())
		}
		slog.InfoContext(ctx, "workspace.cascata_suspensos",
			"dominio", modelworkspace.Dominio, "subdominio", modelworkspace.Subdominio,
			"acao", "cascata_organization_inativada",
			"organization_uuid", organizationUUID.String(), "quantidade", suspensos)
	}
	return int(suspensos), nil
}

// ResolverPorSlug é a exceção GLOBAL documentada: valida o formato cedo
// (rótulo malformado vira ErrNotFound — não distingue existência), consulta
// o cache quando presente e projeta só o mínimo. Inativo NÃO é erro aqui:
// o consumidor decide o 404 pela vitalidade (não vaza existência).
func (s *serviceImpl) ResolverPorSlug(ctx context.Context, rotulo string) (*Resolvido, error) {
	slug, err := modelworkspace.ParseSlug(rotulo)
	if err != nil {
		return nil, ErrNotFound
	}
	if s.cache != nil {
		if entrada, ok := s.cache.Buscar(ctx, slug.String()); ok {
			if resolvido := entradaParaResolvido(entrada); resolvido != nil {
				return resolvido, nil
			}
			// Entrada corrompida: cai para a consulta real (nunca propaga lixo).
		}
	}
	w, err := s.repo.BuscarPorSlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	resolvido := paraResolvido(w)
	if s.cache != nil {
		s.cache.Guardar(ctx, slug.String(), resolvidoParaEntrada(resolvido))
	}
	return resolvido, nil
}

func (s *serviceImpl) ResolverPorUUID(ctx context.Context, id uuid.UUID) (*Resolvido, error) {
	w, err := s.repo.BuscarPorUUIDGlobal(ctx, id)
	if err != nil {
		return nil, err
	}
	return paraResolvido(w), nil
}

func (s *serviceImpl) SlugsFixos() []string {
	fixos := make([]string, len(slugsFixos))
	copy(fixos, slugsFixos)
	return fixos
}

// --- Internos -------------------------------------------------------------------

func paraResolvido(w *modelworkspace.Workspace) *Resolvido {
	return &Resolvido{
		UUID:             w.UUID,
		OrganizationUUID: w.OrganizationUUID,
		Slug:             w.Slug.String(),
		Status:           string(w.Status),
	}
}

func resolvidoParaEntrada(r *Resolvido) EntradaResolucao {
	return EntradaResolucao{
		WorkspaceUUID:    r.UUID.String(),
		OrganizationUUID: r.OrganizationUUID.String(),
		Status:           r.Status,
	}
}

func entradaParaResolvido(e *EntradaResolucao) *Resolvido {
	id, err := uuid.Parse(e.WorkspaceUUID)
	org, orgErr := uuid.Parse(e.OrganizationUUID)
	if err != nil || orgErr != nil {
		// Entrada corrompida no cache: trata como ausente (o chamador refaz a
		// consulta); nunca propaga lixo como verdade.
		return nil
	}
	return &Resolvido{UUID: id, OrganizationUUID: org, Status: e.Status}
}

// auditar registra toda ESCRITA na trilha assíncrona (#9) com payload
// montado à mão (doc 04): identificadores e vocabulário fechado, nunca texto
// livre. Sem trilha ligada (montagem direta em teste), cai para o slog
// legado — mesmo payload, caminho síncrono.
func (s *serviceImpl) auditar(ctx context.Context, acao string, workspaceUUID, organizationUUID uuid.UUID, success bool, extras ...any) {
	validarAcaoCatalogada(acao) // ação fora do events.go reprova em teste/boot
	evento := audit_log.Evento{
		Instante:         time.Now().UTC(),
		Dominio:          modelworkspace.Dominio,
		Subdominio:       modelworkspace.Subdominio,
		Acao:             acao,
		Sucesso:          success,
		WorkspaceUUID:    workspaceUUID.String(),
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
		"dominio", modelworkspace.Dominio, "subdominio", modelworkspace.Subdominio, "acao", acao,
		"workspace_uuid", workspaceUUID.String(),
		"organization_uuid", organizationUUID.String(),
		"user_uuid", orgctx.UserUUID(ctx).String(),
		"ray_trace", orgctx.RayTrace(ctx),
		"success", success,
	}
	args = append(args, extras...)
	slog.InfoContext(ctx, "workspace."+acao, args...)
}
