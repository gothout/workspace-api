package provisionamento

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	modeluser "workspace-api/internal/identidade/model/user"
	modelworkspace "workspace-api/internal/identidade/model/workspace"
	"workspace-api/internal/pkg/log/audit_log"
	"workspace-api/internal/pkg/orgctx"
	"workspace-api/internal/pkg/pii"
)

// Papel atribuído ao admin inicial — nome canônico do seed (doc 03).
const papelAdminOrganization = "admin_organization"

// Service orquestra o provisionamento INICIAL de uma organization: admin
// (nome/e-mail/senha definidos pelo chamador) + workspace inicial (slug) +
// papel admin_organization atribuído. Regra de cada escrita mora no
// SUBDOMÍNIO; aqui mora a ordem, a idempotência e a recusa de quem já foi
// provisionado.
type Service interface {
	Provisionar(ctx context.Context, organizationUUID uuid.UUID, entrada ProvisionamentoRequestDto) (*ProvisionamentoResponseDto, error)
}

type serviceImpl struct {
	organizacoes Organizacoes
	workspaces   Workspaces
	usuarios     Usuarios
	papeis       Papeis
	trilha       audit_log.Destino // trilha assíncrona (#9); nil = slog legado
}

// Dependencias carrega os contratos ligados pelo cmd/bootstrap. Todos são
// obrigatórios: provisionamento pela metade não sobe.
type Dependencias struct {
	Organizacoes Organizacoes
	Workspaces   Workspaces
	Usuarios     Usuarios
	Papeis       Papeis
	Trilha       audit_log.Destino
}

// NewService devolve o service DECORADO (service_observado.go): todo erro
// que sobe ao chamador é observado — singleton e testes pelo mesmo caminho.
func NewService(deps Dependencias) Service {
	s := &serviceImpl{
		organizacoes: deps.Organizacoes,
		workspaces:   deps.Workspaces,
		usuarios:     deps.Usuarios,
		papeis:       deps.Papeis,
		trilha:       deps.Trilha,
	}
	return serviceObservado{Service: s, obs: observadorErros}
}

// Provisionar executa a orquestração, na ordem fail-closed:
//
//  1. entradas validadas CEDO pelos VOs dos modelos (falha de formato nunca
//     toca o banco);
//  2. organization alvo tem que existir e estar ativa;
//  3. organization COM workspace = ErrJaProvisionado (409 — resposta
//     idempotente para chamada repetida);
//  4. workspace inicial criado PELO subdomínio (autorização cross-tenant e
//     unicidade global do slug moram lá);
//  5. admin criado/reconhecido na organization alvo (ctx reescopado — a
//     unicidade de e-mail é POR organization); admin pré-existente de uma
//     tentativa anterior interrompida é RECONHECIDO, não duplicado;
//  6. papel admin_organization atribuído (duplicata = idempotente).
func (s *serviceImpl) Provisionar(ctx context.Context, organizationUUID uuid.UUID, entrada ProvisionamentoRequestDto) (*ProvisionamentoResponseDto, error) {
	if _, err := modelworkspace.ParseSlug(entrada.Slug); err != nil {
		return nil, ErrInvalidInput
	}
	email, err := modeluser.ParseEmail(entrada.Email)
	if err != nil {
		return nil, ErrInvalidInput
	}
	if err := modeluser.ValidarSenha(entrada.Senha); err != nil {
		return nil, ErrInvalidInput
	}

	existe, ativa, err := s.organizacoes.Estado(ctx, organizationUUID)
	if err != nil {
		return nil, err
	}
	if !existe {
		return nil, ErrOrganizacaoNaoEncontrada
	}
	if !ativa {
		return nil, ErrOrganizacaoInativa
	}
	jaTem, err := s.workspaces.TemWorkspaces(ctx, organizationUUID)
	if err != nil {
		return nil, err
	}
	if jaTem {
		return nil, ErrJaProvisionado
	}

	workspaceUUID, err := s.workspaces.Criar(ctx, organizationUUID, entrada.Slug)
	if err != nil {
		return nil, err
	}

	// O admin vive NA ORGANIZATION ALVO: o ctx segue reescopado para as
	// escritas do user (unicidade de e-mail por organization, queries
	// fail-closed). Mesma técnica do reescopo da criação cross-tenant (UX4).
	ctxAlvo := orgctx.WithOrganization(ctx, organizationUUID)
	adminUUID, err := s.adminDaOrganization(ctxAlvo, email.String(), entrada.Nome, entrada.Senha)
	if err != nil {
		return nil, err
	}

	papelUUID, err := s.papeis.PorNome(ctx, papelAdminOrganization)
	if err != nil {
		return nil, err
	}
	if err := s.usuarios.AtribuirPapel(ctxAlvo, adminUUID, workspaceUUID, papelUUID); err != nil && !errors.Is(err, ErrAtribuicaoExistente) {
		return nil, err
	}

	s.auditar(ctx, "provisionar", organizationUUID, true,
		"slug", entrada.Slug,
		"admin_uuid", adminUUID.String(),
		"workspace_uuid", workspaceUUID.String(),
		"email_mascarado", pii.MascaraEmail(email.String()))

	return &ProvisionamentoResponseDto{
		OrganizationUUID: organizationUUID,
		WorkspaceUUID:    workspaceUUID,
		AdminUUID:        adminUUID,
		Email:            email.String(),
		Slug:             entrada.Slug,
	}, nil
}

// adminDaOrganization reconhece o admin de tentativa anterior interrompida
// (criado antes de o fluxo falhar) ou cria um novo — nunca duplica.
func (s *serviceImpl) adminDaOrganization(ctx context.Context, email, nome, senha string) (uuid.UUID, error) {
	if existente, ok, err := s.usuarios.UUIDPorEmail(ctx, email); err != nil {
		return uuid.Nil, err
	} else if ok {
		return existente, nil
	}
	return s.usuarios.CriarAdmin(ctx, nome, email, senha)
}

// auditar registra a ORQUESTRAÇÃO na trilha assíncrona (#9) com payload
// montado à mão (doc 04): identificadores e vocabulário fechado, nunca texto
// livre — e nunca a senha. Sem trilha ligada (montagem direta em teste), cai
// para o slog legado — mesmo payload, caminho síncrono.
func (s *serviceImpl) auditar(ctx context.Context, acao string, organizationUUID uuid.UUID, success bool, extras ...any) {
	validarAcaoCatalogada(acao) // ação fora do events.go reprova em teste/boot
	evento := audit_log.Evento{
		Instante:         time.Now().UTC(),
		Dominio:          Dominio,
		Subdominio:       Subdominio,
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
		"dominio", Dominio, "subdominio", Subdominio, "acao", acao,
		"organization_uuid", organizationUUID.String(),
		"user_uuid", orgctx.UserUUID(ctx).String(),
		"ray_trace", orgctx.RayTrace(ctx),
		"success", success,
	}
	args = append(args, extras...)
	slog.InfoContext(ctx, Subdominio+"."+acao, args...)
}
