package ativacao

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	modelativacao "workspace-api/internal/licensing/model/ativacao"
	"workspace-api/internal/pkg/log/audit_log"
	"workspace-api/internal/pkg/orgctx"
)

// Service é o domain service do agregado: ativação valida O TRIPÉ completo —
// workspace vivo na organization (ValidadorWorkspaces), módulo ATIVO no
// catálogo (BuscadorModulos) e licença viva na organization
// (VerificadorLicencas) — e SlugsLiberados alimenta a resolução do acesso.
type Service interface {
	Ativar(ctx context.Context, workspaceUUIDAlvo uuid.UUID, moduloSlug string) (*modelativacao.AtivacaoComModulo, error)
	Read(ctx context.Context, workspaceUUIDAlvo, id uuid.UUID) (*modelativacao.AtivacaoComModulo, error)
	Listar(ctx context.Context, workspaceUUIDAlvo uuid.UUID) ([]modelativacao.AtivacaoComModulo, error)
	Desativar(ctx context.Context, workspaceUUIDAlvo, id uuid.UUID) error

	// SlugsLiberados devolve as aplicações usáveis no par (organization,
	// workspace) — licença ∩ ativação ∩ módulo ativo. Consumido pela cadeia
	// de acesso (F8) e pelo login/seletor de aplicações (F9).
	SlugsLiberados(ctx context.Context, organizationUUID, workspaceUUID uuid.UUID) ([]AplicacaoDisponivelDto, error)
}

type serviceImpl struct {
	repo        Repository
	modulos     BuscadorModulos     // nil = fail-closed na ativação
	licencas    VerificadorLicencas // nil = fail-closed na ativação
	workspaces  ValidadorWorkspaces // nil = fail-closed na ativação
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
// (app:{org}:{ws}) às escritas de ativação — ativar/desativar vale na hora.
func ComInvalidadorAcessos(i InvalidadorAcessos) OpcaoServico {
	return func(s *serviceImpl) { s.invalidador = i }
}

func NewService(repo Repository, modulos BuscadorModulos, licencas VerificadorLicencas, workspaces ValidadorWorkspaces, opcoes ...OpcaoServico) Service {
	s := &serviceImpl{repo: repo, modulos: modulos, licencas: licencas, workspaces: workspaces}
	for _, aplicar := range opcoes {
		aplicar(s)
	}
	return serviceObservado{Service: s, obs: observadorErros}
}

// Ativar liga o módulo (resolvido por slug) ao workspace alvo — SEMPRE dentro
// da organization do ctx: workspace alheio/inativo = ErrWorkspaceInvalido;
// módulo ausente/inativo = ErrModuloNaoEncontrado; sem licença viva =
// ErrSemLicenca. Unicidade viva é do índice parcial (23505 → ErrJaAtivada).
func (s *serviceImpl) Ativar(ctx context.Context, workspaceUUIDAlvo uuid.UUID, moduloSlug string) (*modelativacao.AtivacaoComModulo, error) {
	organizationUUID := orgctx.OrganizationUUID(ctx)
	if organizationUUID == uuid.Nil || workspaceUUIDAlvo == uuid.Nil {
		return nil, ErrInvalidInput
	}
	if err := s.validarTripé(ctx, organizationUUID, workspaceUUIDAlvo, moduloSlug); err != nil {
		return nil, err
	}
	m, _ := s.modulos.BuscarPorSlug(ctx, moduloSlug)
	a := modelativacao.NewAtivacao(modelativacao.CreateInput{
		OrganizationUUID: organizationUUID,
		WorkspaceUUID:    workspaceUUIDAlvo,
		ModuloUUID:       m.UUID,
		AtivadoPor:       orgctx.UserUUID(ctx),
	})
	if err := s.repo.Criar(ctx, a); err != nil {
		return nil, err
	}
	if s.invalidador != nil {
		s.invalidador.InvalidarPar(ctx, organizationUUID.String(), workspaceUUIDAlvo.String())
	}
	s.auditar(ctx, "ativar", true,
		"workspace_uuid_alvo", workspaceUUIDAlvo.String(),
		"workspace_uuid", workspaceUUIDAlvo.String(),
		"organization_uuid", organizationUUID.String(),
		"modulo_slug", m.Slug.String())
	return &modelativacao.AtivacaoComModulo{
		Ativacao:   *a,
		ModuloSlug: m.Slug.String(),
		ModuloNome: m.Nome,
	}, nil
}

func (s *serviceImpl) Read(ctx context.Context, workspaceUUIDAlvo, id uuid.UUID) (*modelativacao.AtivacaoComModulo, error) {
	return s.repo.BuscarPorUUID(ctx, orgctx.OrganizationUUID(ctx), workspaceUUIDAlvo, id)
}

func (s *serviceImpl) Listar(ctx context.Context, workspaceUUIDAlvo uuid.UUID) ([]modelativacao.AtivacaoComModulo, error) {
	return s.repo.Listar(ctx, orgctx.OrganizationUUID(ctx), workspaceUUIDAlvo)
}

// Desativar remove a ativação (soft delete); o acesso cai NA HORA — a
// resolução consulta apenas linhas vivas.
func (s *serviceImpl) Desativar(ctx context.Context, workspaceUUIDAlvo, id uuid.UUID) error {
	a, err := s.repo.BuscarPorUUID(ctx, orgctx.OrganizationUUID(ctx), workspaceUUIDAlvo, id)
	if err != nil {
		return err
	}
	if err := s.repo.Remover(ctx, a.OrganizationUUID, a.WorkspaceUUID, id); err != nil {
		return err
	}
	if s.invalidador != nil {
		s.invalidador.InvalidarPar(ctx, a.OrganizationUUID.String(), a.WorkspaceUUID.String())
	}
	s.auditar(ctx, "desativar", true,
		"workspace_uuid_alvo", a.WorkspaceUUID.String(),
		"workspace_uuid", a.WorkspaceUUID.String(),
		"organization_uuid", a.OrganizationUUID.String(),
		"modulo_slug", a.ModuloSlug)
	return nil
}

func (s *serviceImpl) SlugsLiberados(ctx context.Context, organizationUUID, workspaceUUID uuid.UUID) ([]AplicacaoDisponivelDto, error) {
	if organizationUUID == uuid.Nil || workspaceUUID == uuid.Nil {
		return nil, ErrInvalidInput
	}
	return s.repo.ListarSlugsLiberados(ctx, organizationUUID, workspaceUUID)
}

// validarTripé roda as três conferências na ordem do custo e do sigilo:
// workspace primeiro (não vaza catálogo), módulo depois, licença por último.
// Peça de contrato faltando = boot quebrado: recusa com o erro correspondente
// (fail-closed) — nunca ativa sem saber o estado real.
func (s *serviceImpl) validarTripé(ctx context.Context, organizationUUID, workspaceUUID uuid.UUID, moduloSlug string) error {
	if s.workspaces == nil || s.modulos == nil || s.licencas == nil {
		return ErrWorkspaceInvalido
	}
	pertence, err := s.workspaces.Pertence(ctx, organizationUUID, workspaceUUID)
	if err != nil {
		return err
	}
	if !pertence {
		return ErrWorkspaceInvalido
	}
	m, err := s.modulos.BuscarPorSlug(ctx, moduloSlug)
	if err != nil {
		if errors.Is(err, ErrModuloNaoEncontrado) {
			return ErrModuloInvalido
		}
		return err
	}
	if !m.Ativo {
		return ErrModuloInvalido
	}
	comLicenca, err := s.licencas.Existe(ctx, organizationUUID, m.UUID)
	if err != nil {
		return err
	}
	if !comLicenca {
		return ErrSemLicenca
	}
	return nil
}

// auditar registra toda ESCRITA na trilha assíncrona (#9): os campos *_alvo
// carregam o workspace/organization do PATH quando divergentes do ctx.
func (s *serviceImpl) auditar(ctx context.Context, acao string, success bool, extras ...any) {
	validarAcaoCatalogada(acao)
	evento := audit_log.Evento{
		Instante:         time.Now().UTC(),
		Dominio:          modelativacao.Dominio,
		Subdominio:       modelativacao.Subdominio,
		Acao:             acao,
		Sucesso:          success,
		UserUUID:         orgctx.UserUUID(ctx).String(),
		RayTrace:         orgctx.RayTrace(ctx),
		Detalhes:         audit_log.Detalhes(extras...),
	}
	if s.trilha != nil {
		s.trilha.Registrar(evento)
		return
	}
	args := []any{
		"dominio", modelativacao.Dominio, "subdominio", modelativacao.Subdominio, "acao", acao,
		"user_uuid", orgctx.UserUUID(ctx).String(),
		"ray_trace", orgctx.RayTrace(ctx),
		"success", success,
	}
	args = append(args, extras...)
	slog.InfoContext(ctx, "ativacao."+acao, args...)
}
