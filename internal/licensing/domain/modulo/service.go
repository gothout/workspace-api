package modulo

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	modelmodulo "workspace-api/internal/licensing/model/modulo"
	"workspace-api/internal/pkg/log/audit_log"
	"workspace-api/internal/pkg/orgctx"
)

// Service é o domain service do agregado: tradução de input em métodos de
// comportamento, guarda de remoção (sem licenças vivas) e auditoria de toda
// escrita. O catálogo é GLOBAL — não há escopo de tenancy aqui; a fronteira
// é a permissão licensing:modulo:* exigida rota a rota.
type Service interface {
	Create(ctx context.Context, in modelmodulo.CreateInput) (*modelmodulo.Modulo, error)
	Read(ctx context.Context, id uuid.UUID) (*modelmodulo.Modulo, error)
	// BuscarPorSlug expõe o finder do catálogo para os contratos irmãos
	// (licenca/ativacao resolvem o slug informado) — leitura global por
	// natureza, resultado só atravessa por uuid/projeção.
	BuscarPorSlug(ctx context.Context, slug string) (*modelmodulo.Modulo, error)
	List(ctx context.Context, f modelmodulo.ListFilter) ([]modelmodulo.Modulo, int64, error)
	Update(ctx context.Context, id uuid.UUID, in modelmodulo.UpdateInput) (*modelmodulo.Modulo, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type serviceImpl struct {
	repo        Repository
	licencas    VerificadorLicencas // nil = fail-closed na remoção (peça faltando recusa)
	invalidador InvalidadorAcessos  // nil = sem cache de aplicações (operação normal)
	trilha      audit_log.Destino   // trilha assíncrona (#9); nil = slog legado
}

// OpcaoServico adiciona peça opcional ao service na montagem.
type OpcaoServico func(*serviceImpl)

// ComTrilha liga o destino assíncrono da auditoria (evolução #9).
func ComTrilha(t audit_log.Destino) OpcaoServico {
	return func(s *serviceImpl) { s.trilha = t }
}

// ComInvalidadorAcessos liga a invalidação do cache de módulos liberados
// (app:*) às escritas do catálogo — desativar/remover vale na hora.
func ComInvalidadorAcessos(i InvalidadorAcessos) OpcaoServico {
	return func(s *serviceImpl) { s.invalidador = i }
}

func NewService(repo Repository, licencas VerificadorLicencas, opcoes ...OpcaoServico) Service {
	s := &serviceImpl{repo: repo, licencas: licencas}
	for _, aplicar := range opcoes {
		aplicar(s)
	}
	// Observação de erros (evolução errobserve): todo retorno de erro do
	// service passa pelo observador do subdomínio — erro sai intacto.
	return serviceObservado{Service: s, obs: observadorErros}
}

// Create: input cru → entidade VÁLIDA pelo construtor → persistência →
// auditoria. Unicidade do slug é do índice TOTAL (23505 traduzido no repo).
func (s *serviceImpl) Create(ctx context.Context, in modelmodulo.CreateInput) (*modelmodulo.Modulo, error) {
	m, err := modelmodulo.NewModulo(in)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Criar(ctx, m); err != nil {
		return nil, err
	}
	s.auditar(ctx, "criar", true, "slug", m.Slug.String())
	return m, nil
}

func (s *serviceImpl) Read(ctx context.Context, id uuid.UUID) (*modelmodulo.Modulo, error) {
	return s.repo.BuscarPorUUID(ctx, id)
}

// BuscarPorSlug valida o formato cedo: rótulo malformado vira ErrNotFound —
// não distingue existência para quem só tem o slug.
func (s *serviceImpl) BuscarPorSlug(ctx context.Context, slug string) (*modelmodulo.Modulo, error) {
	vo, err := modelmodulo.ParseSlug(slug)
	if err != nil {
		return nil, ErrNotFound
	}
	return s.repo.BuscarPorSlug(ctx, vo)
}

func (s *serviceImpl) List(ctx context.Context, f modelmodulo.ListFilter) ([]modelmodulo.Modulo, int64, error) {
	return s.repo.Listar(ctx, f)
}

// Update traduz o input em chamadas aos métodos de comportamento — nunca
// atribui campo direto.
func (s *serviceImpl) Update(ctx context.Context, id uuid.UUID, in modelmodulo.UpdateInput) (*modelmodulo.Modulo, error) {
	m, err := s.repo.BuscarPorUUID(ctx, id)
	if err != nil {
		return nil, err
	}
	inativou, ativou := false, false
	if in.Nome != nil {
		if err := m.Renomear(*in.Nome); err != nil {
			return nil, err
		}
	}
	if in.Descricao != nil {
		m.EditarDescricao(*in.Descricao)
	}
	if in.Ativo != nil {
		if *in.Ativo {
			// Transição é única: repeti-la viola a invariante (422) — mesmo
			// desenho do Inativar/Reativar do workspace.
			if err := m.Ativar(); err != nil {
				return nil, err
			}
			ativou = true
		} else {
			if err := m.Desativar(); err != nil {
				return nil, err
			}
			inativou = true
		}
	}
	if err := s.repo.Atualizar(ctx, m); err != nil {
		return nil, err
	}
	if (inativou || ativou) && s.invalidador != nil {
		// Vitalidade do catálogo muda a resolução de TODO par (org, ws):
		// invalidação global — evento raro, o TTL é o cinto de segurança.
		s.invalidador.InvalidarTudo(ctx)
	}
	s.auditar(ctx, "editar", true,
		"slug", m.Slug.String(), "inativo", inativou, "ativado", ativou)
	return m, nil
}

// Delete só aceita módulo SEM licenças vivas em organization nenhuma —
// catálogo referenciado por concessões não sai por baixo delas; o caminho de
// retirada é Desativar (bloqueia novo acesso imediatamente). Módulo removido
// NÃO libera o slug (índice único TOTAL).
func (s *serviceImpl) Delete(ctx context.Context, id uuid.UUID) error {
	m, err := s.repo.BuscarPorUUID(ctx, id)
	if err != nil {
		return err
	}
	if s.licencas == nil {
		// Peça faltando é boot quebrado: fail-closed — remoção nunca corre sem
		// saber se há concessões vivas.
		return ErrModuloEmUso
	}
	emUso, err := s.licencas.ExisteParaModulo(ctx, id)
	if err != nil {
		return err
	}
	if emUso {
		return ErrModuloEmUso
	}
	if err := s.repo.Remover(ctx, id); err != nil {
		return err
	}
	if s.invalidador != nil {
		s.invalidador.InvalidarTudo(ctx)
	}
	s.auditar(ctx, "remover", true, "slug", m.Slug.String())
	return nil
}

// auditar registra toda ESCRITA na trilha assíncrona (#9) com payload
// montado à mão (doc 04). Tabela global: organization/workspace vazios por
// natureza. Sem trilha ligada (montagem direta em teste), cai para o slog
// legado — mesmo payload, caminho síncrono.
func (s *serviceImpl) auditar(ctx context.Context, acao string, success bool, extras ...any) {
	validarAcaoCatalogada(acao) // ação fora do events.go reprova em teste/boot
	evento := audit_log.Evento{
		Instante:   time.Now().UTC(),
		Dominio:    modelmodulo.Dominio,
		Subdominio: modelmodulo.Subdominio,
		Acao:       acao,
		Sucesso:    success,
		UserUUID:   orgctx.UserUUID(ctx).String(),
		RayTrace:   orgctx.RayTrace(ctx),
		Detalhes:   audit_log.Detalhes(extras...),
	}
	if s.trilha != nil {
		s.trilha.Registrar(evento)
		return
	}
	args := []any{
		"dominio", modelmodulo.Dominio, "subdominio", modelmodulo.Subdominio, "acao", acao,
		"user_uuid", orgctx.UserUUID(ctx).String(),
		"ray_trace", orgctx.RayTrace(ctx),
		"success", success,
	}
	args = append(args, extras...)
	slog.InfoContext(ctx, "modulo."+acao, args...)
}
