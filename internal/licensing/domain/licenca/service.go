package licenca

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	modellicenca "workspace-api/internal/licensing/model/licenca"
	"workspace-api/internal/middleware"
	"workspace-api/internal/pkg/log/audit_log"
	"workspace-api/internal/pkg/orgctx"
)

// Service é o domain service do agregado: atribuição (resolve o slug no
// catálogo pelo contrato BuscadorModulos), revogação e a fronteira de leitura
// (própria organization OU permissão ler_plataforma — matcher do middleware).
// Atribuir/revogar são EXCLUSIVOS do super_admin: a exigência vive rota a
// rota; aqui não se re-checa permissão de escrita.
type Service interface {
	Atribuir(ctx context.Context, organizationUUIDAlvo uuid.UUID, moduloSlug string) (*modellicenca.LicencaComModulo, error)
	Read(ctx context.Context, organizationUUIDAlvo, id uuid.UUID) (*modellicenca.LicencaComModulo, error)
	Listar(ctx context.Context, organizationUUIDAlvo uuid.UUID) ([]modellicenca.LicencaComModulo, error)
	Revogar(ctx context.Context, organizationUUIDAlvo, id uuid.UUID) error

	// ExisteViva expõe a pergunta da ativação (contrato irmão): a
	// organization tem concessão viva deste módulo? Leitura de existência —
	// nunca expõe a licença em si.
	ExisteViva(ctx context.Context, organizationUUID, moduloUUID uuid.UUID) (bool, error)
}

type serviceImpl struct {
	repo        Repository
	modulos     BuscadorModulos    // nil = fail-closed na atribuição (peça faltando recusa)
	invalidador InvalidadorAcessos // nil = sem cache de aplicações (operação normal)
	trilha      audit_log.Destino  // trilha assíncrona (#9); nil = slog legado
}

// OpcaoServico adiciona peça opcional ao service na montagem.
type OpcaoServico func(*serviceImpl)

// ComTrilha liga o destino assíncrono da auditoria (evolução #9).
func ComTrilha(t audit_log.Destino) OpcaoServico {
	return func(s *serviceImpl) { s.trilha = t }
}

// ComInvalidadorAcessos liga a invalidação do cache de módulos liberados
// (app:{org}:*) às escritas de licença — conceder/revogar vale na hora.
func ComInvalidadorAcessos(i InvalidadorAcessos) OpcaoServico {
	return func(s *serviceImpl) { s.invalidador = i }
}

func NewService(repo Repository, modulos BuscadorModulos, opcoes ...OpcaoServico) Service {
	s := &serviceImpl{repo: repo, modulos: modulos}
	for _, aplicar := range opcoes {
		aplicar(s)
	}
	return serviceObservado{Service: s, obs: observadorErros}
}

// Atribui a licença do módulo (resolvido por slug) à organization alvo.
// Idempotência amigável: par já licenciado = ErrJaConcedida (409) — o painel
// mostra o estado real em vez de fingir sucesso.
func (s *serviceImpl) Atribuir(ctx context.Context, organizationUUIDAlvo uuid.UUID, moduloSlug string) (*modellicenca.LicencaComModulo, error) {
	if organizationUUIDAlvo == uuid.Nil {
		return nil, ErrInvalidInput
	}
	if s.modulos == nil {
		// Peça faltando é boot quebrado: fail-closed — nunca conceder sem
		// confirmar que o módulo existe no catálogo.
		return nil, ErrModuloInvalido
	}
	m, err := s.modulos.BuscarPorSlug(ctx, moduloSlug)
	if err != nil {
		if errors.Is(err, ErrModuloNaoEncontrado) {
			return nil, ErrModuloInvalido
		}
		return nil, err
	}
	l := modellicenca.NewLicenca(modellicenca.CreateInput{
		OrganizationUUID: organizationUUIDAlvo,
		ModuloUUID:       m.UUID,
		ConcedidaPor:     orgctx.UserUUID(ctx),
	})
	if err := s.repo.Criar(ctx, l); err != nil {
		return nil, err
	}
	if s.invalidador != nil {
		s.invalidador.InvalidarOrganization(ctx, organizationUUIDAlvo.String())
	}
	s.auditar(ctx, "atribuir", true,
		"organization_uuid_alvo", organizationUUIDAlvo.String(),
		"organization_uuid", organizationUUIDAlvo.String(),
		"modulo_slug", m.Slug.String())
	return &modellicenca.LicencaComModulo{
		Licenca:    *l,
		ModuloSlug: m.Slug.String(),
		ModuloNome: m.Nome,
	}, nil
}

// Read devolve UMA licença; quem lê fora da própria organization precisa da
// permissão ler_plataforma (matcher curinga do middleware sobre as efetivas
// injetadas no ctx).
func (s *serviceImpl) Read(ctx context.Context, organizationUUIDAlvo, id uuid.UUID) (*modellicenca.LicencaComModulo, error) {
	if err := s.autorizarLeitura(ctx, organizationUUIDAlvo); err != nil {
		return nil, err
	}
	if organizationUUIDAlvo == orgctx.OrganizationUUID(ctx) {
		return s.repo.BuscarPorUUID(ctx, id)
	}
	return s.repo.BuscarPorUUIDNaOrganization(ctx, organizationUUIDAlvo, id)
}

// Listar devolve as licenças da organization alvo com a mesma fronteira do Read.
func (s *serviceImpl) Listar(ctx context.Context, organizationUUIDAlvo uuid.UUID) ([]modellicenca.LicencaComModulo, error) {
	if err := s.autorizarLeitura(ctx, organizationUUIDAlvo); err != nil {
		return nil, err
	}
	return s.repo.Listar(ctx, organizationUUIDAlvo)
}

// Revogar remove a licença (soft delete). A cascata de ACESSO é natural:
// sem licença viva, a resolução `Application` nega na próxima requisição —
// ativações órfãs ficam inertes até reativação da licença.
func (s *serviceImpl) Revogar(ctx context.Context, organizationUUIDAlvo, id uuid.UUID) error {
	l, err := s.repo.BuscarPorUUIDNaOrganization(ctx, organizationUUIDAlvo, id)
	if err != nil {
		return err
	}
	if err := s.repo.Revogar(ctx, organizationUUIDAlvo, id); err != nil {
		return err
	}
	if s.invalidador != nil {
		// Revogar afeta TODOS os workspaces da organization: invalidação
		// grosseira app:{org}:* — a resolução nega na próxima requisição.
		s.invalidador.InvalidarOrganization(ctx, organizationUUIDAlvo.String())
	}
	s.auditar(ctx, "revogar", true,
		"organization_uuid_alvo", organizationUUIDAlvo.String(),
		"organization_uuid", organizationUUIDAlvo.String(),
		"modulo_slug", l.ModuloSlug)
	return nil
}

// ExisteViva responde ao contrato VerificadorLicencas da ativação.
func (s *serviceImpl) ExisteViva(ctx context.Context, organizationUUID, moduloUUID uuid.UUID) (bool, error) {
	if organizationUUID == uuid.Nil || moduloUUID == uuid.Nil {
		return false, ErrInvalidInput
	}
	return s.repo.ExisteNaOrganization(ctx, organizationUUID, moduloUUID)
}

// autorizarLeitura: mesma organization do ctx → permissão :ler já exigida na
// rota basta; organization ALHEIA → exige ler_plataforma nas efetivas.
func (s *serviceImpl) autorizarLeitura(ctx context.Context, organizationUUIDAlvo uuid.UUID) error {
	if organizationUUIDAlvo == uuid.Nil {
		return ErrInvalidInput
	}
	if organizationUUIDAlvo == orgctx.OrganizationUUID(ctx) {
		return nil
	}
	// Matcher curinga do middleware sobre as efetivas injetadas no ctx:
	// super_admin (*:*) passa; papéis de tenant, não.
	if !middleware.Atende(orgctx.Permissoes(ctx), PermLerPlataforma) {
		return ErrSemAcesso
	}
	return nil
}

// auditar registra toda ESCRITA na trilha assíncrona (#9): organization_uuid
// do evento é a organization ALVO (é nela que a concessão acontece).
func (s *serviceImpl) auditar(ctx context.Context, acao string, success bool, extras ...any) {
	validarAcaoCatalogada(acao)
	evento := audit_log.Evento{
		Instante:   time.Now().UTC(),
		Dominio:    modellicenca.Dominio,
		Subdominio: modellicenca.Subdominio,
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
		"dominio", modellicenca.Dominio, "subdominio", modellicenca.Subdominio, "acao", acao,
		"user_uuid", orgctx.UserUUID(ctx).String(),
		"ray_trace", orgctx.RayTrace(ctx),
		"success", success,
	}
	args = append(args, extras...)
	slog.InfoContext(ctx, "licenca."+acao, args...)
}
