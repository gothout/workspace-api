package tarefa

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	modeltarefa "workspace-api/internal/todolist/model/tarefa"
	"workspace-api/internal/pkg/log/audit_log"
	"workspace-api/internal/pkg/orgctx"
)

// Service é o domain service do agregado: tradução de input em métodos de
// comportamento, tenancy vindo do ctx (nunca do corpo) e auditoria de toda
// escrita.
type Service interface {
	Create(ctx context.Context, in modeltarefa.CreateInput) (*modeltarefa.Tarefa, error)
	Read(ctx context.Context, id uuid.UUID) (*modeltarefa.Tarefa, error)
	List(ctx context.Context, f modeltarefa.ListFilter) ([]modeltarefa.Tarefa, int64, error)
	Update(ctx context.Context, id uuid.UUID, in modeltarefa.UpdateInput) (*modeltarefa.Tarefa, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type serviceImpl struct {
	repo   Repository
	trilha audit_log.Destino // trilha assíncrona (#9); nil = slog legado
}

// OpcaoServico adiciona peça opcional ao service na montagem.
type OpcaoServico func(*serviceImpl)

// ComTrilha liga o destino assíncrono da auditoria (evolução #9).
func ComTrilha(t audit_log.Destino) OpcaoServico {
	return func(s *serviceImpl) { s.trilha = t }
}

func NewService(repo Repository, opcoes ...OpcaoServico) Service {
	s := &serviceImpl{repo: repo}
	for _, aplicar := range opcoes {
		aplicar(s)
	}
	return serviceObservado{Service: s, obs: observadorErros}
}

// Create: input cru → escopo do ctx → entidade VÁLIDA pelo construtor →
// persistência → auditoria.
func (s *serviceImpl) Create(ctx context.Context, in modeltarefa.CreateInput) (*modeltarefa.Tarefa, error) {
	in.OrganizationUUID = orgctx.OrganizationUUID(ctx) // escopo vem do ctx, NUNCA do corpo
	in.WorkspaceUUID = orgctx.WorkspaceUUID(ctx)
	t, err := modeltarefa.NewTarefa(in)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Criar(ctx, t); err != nil {
		return nil, err
	}
	s.auditar(ctx, "criar", true, "titulo", t.Titulo)
	return t, nil
}

func (s *serviceImpl) Read(ctx context.Context, id uuid.UUID) (*modeltarefa.Tarefa, error) {
	return s.repo.BuscarPorUUID(ctx, id)
}

func (s *serviceImpl) List(ctx context.Context, f modeltarefa.ListFilter) ([]modeltarefa.Tarefa, int64, error) {
	return s.repo.Listar(ctx, f)
}

// Update traduz o input em chamadas aos métodos de comportamento — nunca
// atribui campo direto.
func (s *serviceImpl) Update(ctx context.Context, id uuid.UUID, in modeltarefa.UpdateInput) (*modeltarefa.Tarefa, error) {
	t, err := s.repo.BuscarPorUUID(ctx, id)
	if err != nil {
		return nil, err
	}
	concluiu, reabriu := false, false
	if in.Titulo != nil {
		if err := t.Renomear(*in.Titulo); err != nil {
			return nil, err
		}
	}
	if in.Descricao != nil {
		t.EditarDescricao(*in.Descricao)
	}
	if in.Concluida != nil {
		if *in.Concluida {
			if err := t.Concluir(); err != nil {
				return nil, err
			}
			concluiu = true
		} else {
			if err := t.Reabrir(); err != nil {
				return nil, err
			}
			reabriu = true
		}
	}
	if err := s.repo.Atualizar(ctx, t); err != nil {
		return nil, err
	}
	s.auditar(ctx, "editar", true,
		"titulo", t.Titulo, "concluida", concluiu, "reaberta", reabriu)
	return t, nil
}

func (s *serviceImpl) Delete(ctx context.Context, id uuid.UUID) error {
	t, err := s.repo.BuscarPorUUID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repo.Remover(ctx, id); err != nil {
		return err
	}
	s.auditar(ctx, "remover", true, "titulo", t.Titulo)
	return nil
}

// auditar registra toda ESCRITA na trilha assíncrona (#9) com payload
// montado à mão (doc 04). Sem trilha ligada (teste), cai para o slog legado.
func (s *serviceImpl) auditar(ctx context.Context, acao string, success bool, extras ...any) {
	validarAcaoCatalogada(acao)
	evento := audit_log.Evento{
		Instante:         time.Now().UTC(),
		Dominio:          modeltarefa.Dominio,
		Subdominio:       modeltarefa.Subdominio,
		Acao:             acao,
		Sucesso:          success,
		WorkspaceUUID:    orgctx.WorkspaceUUID(ctx).String(),
		OrganizationUUID: orgctx.OrganizationUUID(ctx).String(),
		UserUUID:         orgctx.UserUUID(ctx).String(),
		RayTrace:         orgctx.RayTrace(ctx),
		Detalhes:         audit_log.Detalhes(extras...),
	}
	if s.trilha != nil {
		s.trilha.Registrar(evento)
		return
	}
	args := []any{
		"dominio", modeltarefa.Dominio, "subdominio", modeltarefa.Subdominio, "acao", acao,
		"workspace_uuid", orgctx.WorkspaceUUID(ctx).String(),
		"organization_uuid", orgctx.OrganizationUUID(ctx).String(),
		"user_uuid", orgctx.UserUUID(ctx).String(),
		"ray_trace", orgctx.RayTrace(ctx),
		"success", success,
	}
	args = append(args, extras...)
	slog.InfoContext(ctx, "tarefa."+acao, args...)
}
