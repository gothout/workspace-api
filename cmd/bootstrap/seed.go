// Seed da plataforma: dados mínimos IDEMPOTENTES, rodados SÓ via
// `workspace-api seed` — nunca automáticos no boot (agents/02).
//
// Os 5 papéis globais e seus conjuntos exatos de permissões vêm do doc 03:
// strings estáveis que os subdomínios declaram como constantes PermX nas
// fases seguintes — divergência entre seed e PermX é bug de contrato.
//
// identidade:catalogo:ler (constante PermLer da aplicação catalogo) está nos
// QUATRO papéis humanos: ver a própria árvore de permissões é pré-requisito
// de usar qualquer outra — super_admin passa pelo curinga *:*.
//
// Leitura de logs (E5): admin_organization lê a organization inteira
// (identidade:logs:ler + :ler_organization); admin_workspace e
// somente_leitura leem o recorte do próprio workspace (identidade:logs:ler);
// usuario_workspace não vê trilhas — auditoria é função de administração.
//
// O PROVISIONAMENTO opcional (--super-admin-email/--super-admin-senha) cria o
// primeiro super_admin e o workspace inicial na organization raiz — o par que
// falta para o template subir ponta a ponta sem insert manual. Toda regra de
// negócio roda PELOS SERVICES dos subdomínios (construtores puros, nunca os
// singletons do processo): formato de slug/e-mail/senha, slugs reservados,
// unicidade, bcrypt e validação da atribuição continuam morando onde sempre
// viveram. Idempotente: reconhece o que já existe e não altera nada.
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	dominioUsuario "workspace-api/internal/identidade/domain/user"
	dominioWorkspace "workspace-api/internal/identidade/domain/workspace"
	modeluser "workspace-api/internal/identidade/model/user"
	modelworkspace "workspace-api/internal/identidade/model/workspace"
	"workspace-api/internal/infra/database/postgres"
	"workspace-api/internal/pkg/orgctx"
)

// Nomes canônicos dos papéis seed (tabela identidade_user_papel.nome).
const (
	papelSuperAdmin        = "super_admin"
	papelAdminOrganization = "admin_organization"
	papelAdminWorkspace    = "admin_workspace"
	papelUsuarioWorkspace  = "usuario_workspace"
	papelSomenteLeitura    = "somente_leitura"
)

// Constantes do provisionamento opcional (R3): vocabulário fechado do
// bootstrap — o nome do primeiro super_admin é fixo (renomeável depois pela
// própria API) e o slug do workspace inicial tem padrão, ambos documentados
// no README.
const (
	// SlugPadraoProvisionamento é o slug sugerido do workspace inicial quando
	// o operador não informa outro — rótulo livre (fora dos fixos da plataforma).
	SlugPadraoProvisionamento = "principal"
	// nomeSuperAdminSeed é o nome do primeiro super_admin.
	nomeSuperAdminSeed = "Administrador da Plataforma"
	// rayTraceSeed identifica a trilha de auditoria das escritas do seed.
	rayTraceSeed = "seed"
)

// Provisionamento carrega o bootstrap OPCIONAL do template: o primeiro
// super_admin e o workspace inicial da organization raiz. Nunca é automático
// no boot — roda só via `workspace-api seed` com e-mail E senha informados.
type Provisionamento struct {
	EmailSuperAdmin string
	SenhaSuperAdmin string
	SlugWorkspace   string
}

// NovoProvisionamento monta o pedido a partir das entradas da CLI: qualquer
// campo marcado liga o provisionamento e EXIGE e-mail + senha juntos (o slug
// tem padrão). Sem campos, devolve nil — seed simples, sem provisionamento.
func NovoProvisionamento(email, senha, slug string) (*Provisionamento, error) {
	if email == "" && senha == "" && slug == "" {
		return nil, nil
	}
	if email == "" || senha == "" {
		return nil, errors.New("seed: o provisionamento pede --super-admin-email E --super-admin-senha juntos")
	}
	if strings.TrimSpace(slug) == "" {
		slug = SlugPadraoProvisionamento
	}
	return &Provisionamento{EmailSuperAdmin: email, SenhaSuperAdmin: senha, SlugWorkspace: slug}, nil
}

// papelSeed é a definição declarativa de um papel: nome, descrição PT-BR e as
// permissões granulares exatas (string dominio:subdominio:acao; curinga só
// para papel admin).
type papelSeed struct {
	nome       string
	descricao  string
	permissoes []string
}

var papeisSeed = []papelSeed{
	{
		nome:       papelSuperAdmin,
		descricao:  "Admin da plataforma: atravessa qualquer exigência e entra em qualquer workspace de qualquer organization.",
		permissoes: []string{"*:*"},
	},
	{
		nome:      papelAdminOrganization,
		descricao: "Dono do contrato: administra workspaces, usuários e as chaves de API da organization; suporte em qualquer workspace da própria; lê os logs da organization inteira.",
		permissoes: []string{
			"identidade:workspace:*",
			"identidade:user:*",
			"identidade:organization:gerenciar_apikeys",
			"identidade:catalogo:ler",
			"identidade:logs:ler",
			"identidade:logs:ler_organization",
		},
	},
	{
		nome:      papelAdminWorkspace,
		descricao: "Administrador do workspace: tudo dentro dos workspaces em que exerce o papel, menos mudar a estrutura do contrato.",
		permissoes: []string{
			"identidade:workspace:editar",
			"identidade:user:*",
			"identidade:catalogo:ler",
			"identidade:logs:ler",
		},
	},
	{
		nome:      papelUsuarioWorkspace,
		descricao: "Operador: trabalha no workspace sem administrar identidade; ações operacionais chegam com os subdomínios de negócio.",
		permissoes: []string{
			"identidade:workspace:ler",
			"identidade:catalogo:ler",
		},
	},
	{
		nome:      papelSomenteLeitura,
		descricao: "Consulta sem escrita: só as ações :ler.",
		permissoes: []string{
			"identidade:organization:ler",
			"identidade:workspace:ler",
			"identidade:user:ler",
			"identidade:catalogo:ler",
			"identidade:logs:ler",
		},
	},
}

// Seed executa todos os seeds do template sobre o banco do processo. Com
// provisionamento não nulo, cria TAMBÉM o primeiro super_admin e o workspace
// inicial da organization raiz — sempre idempotente.
func Seed(caminhoConfig string, provisionamento *Provisionamento) error {
	op, err := abrirOperacao(caminhoConfig, true)
	if err != nil {
		return err
	}
	defer op.fechar()

	db, err := postgres.GetDB()
	if err != nil {
		return err
	}
	ctx := context.Background()
	if err := semearPapeis(ctx, db); err != nil {
		return err
	}
	if err := semearOrganizacaoRaiz(ctx, db); err != nil {
		return err
	}
	if provisionamento == nil {
		return nil
	}
	return provisionarBootstrap(ctx, db, *provisionamento)
}

// uuidOrganizacaoRaiz devolve o uuid determinístico da organization raiz —
// mesma chave usada por quem consulta o seed depois (provisionamento,
// resolução do console master).
func uuidOrganizacaoRaiz() uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("workspace-api://organizacao-raiz"))
}

// semearOrganizacaoRaiz cria a organization da PLATAFORMA (raiz do console
// master) com uuid determinístico — rodar duas vezes não duplica nem altera.
func semearOrganizacaoRaiz(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).Exec(`
INSERT INTO identidade_organization_organization (uuid, nome, status)
VALUES (?, ?, 'ativo')
ON CONFLICT (uuid) DO NOTHING`, uuidOrganizacaoRaiz(), "Plataforma").Error
}

// provisionarBootstrap cria o PRIMEIRO super_admin e o workspace inicial na
// organization raiz — a porta de entrada do template. Ordem: workspace →
// usuário → atribuição (a atribuição valida o tripé pelos services). As
// entradas são validadas CEDO pelos VOs do modelo: falha de formato morre
// antes de tocar o banco, com mensagem acionável no CLI.
func provisionarBootstrap(ctx context.Context, db *gorm.DB, prov Provisionamento) error {
	if prov.SlugWorkspace == "" {
		prov.SlugWorkspace = SlugPadraoProvisionamento
	}
	slugInicial, err := modelworkspace.ParseSlug(prov.SlugWorkspace)
	if err != nil {
		return fmt.Errorf("provisionamento: slug do workspace inicial inválido: %w", err)
	}
	emailSuperAdmin, err := modeluser.ParseEmail(prov.EmailSuperAdmin)
	if err != nil {
		return fmt.Errorf("provisionamento: e-mail do super_admin inválido: %w", err)
	}
	if err := modeluser.ValidarSenha(prov.SenhaSuperAdmin); err != nil {
		return fmt.Errorf("provisionamento: senha fora da política (%d–%d bytes): %w",
			modeluser.TamanhoMinimoSenha, modeluser.TamanhoMaximoSenha, err)
	}

	ctx = orgctx.WithRayTrace(orgctx.WithOrganization(ctx, uuidOrganizacaoRaiz()), rayTraceSeed)

	servicoWorkspace := dominioWorkspace.NewService(dominioWorkspace.NewRepository(db), nil)
	workspaceUUID, err := garantirWorkspaceInicial(ctx, servicoWorkspace, slugInicial)
	if err != nil {
		return err
	}

	repoUsuario := dominioUsuario.NewRepository(db)
	servicoUsuario := dominioUsuario.NewService(
		repoUsuario,
		dominioUsuario.NewRepositorioAtribuicoes(db),
		validadorWorkspacesSeed{repo: dominioWorkspace.NewRepository(db)},
		dominioUsuario.NovasCredenciaisBcrypt())
	usuarioUUID, err := garantirSuperAdmin(ctx, servicoUsuario, repoUsuario, emailSuperAdmin, prov.SenhaSuperAdmin)
	if err != nil {
		return err
	}
	return garantirAtribuicaoSuperAdmin(ctx, servicoUsuario, db, usuarioUUID, workspaceUUID)
}

// garantirWorkspaceInicial resolve o slug GLOBALMENTE (mesmo finder do Host):
// já existindo na organization raiz é idempotência; pertencendo a OUTRA
// organization é erro — o provisionamento nunca toma endereço alheio.
func garantirWorkspaceInicial(ctx context.Context, servico dominioWorkspace.Service, slug modelworkspace.Slug) (uuid.UUID, error) {
	resolvido, err := servico.ResolverPorSlug(ctx, slug.String())
	switch {
	case err == nil:
		if resolvido.OrganizationUUID != orgctx.OrganizationUUID(ctx) {
			return uuid.Nil, fmt.Errorf("provisionamento: slug %q já pertence a outra organization", slug.String())
		}
		slog.Info("[SEED] workspace inicial já existe", "slug", slug.String(), "workspace_uuid", resolvido.UUID.String())
		return resolvido.UUID, nil
	case errors.Is(err, dominioWorkspace.ErrNotFound):
		w, err := servico.Create(ctx, modelworkspace.CreateInput{Nome: slug.String(), Slug: slug.String()})
		if err != nil {
			return uuid.Nil, fmt.Errorf("provisionamento: workspace inicial recusado: %w", err)
		}
		slog.Info("[SEED] workspace inicial criado", "slug", w.Slug.String(), "workspace_uuid", w.UUID.String())
		return w.UUID, nil
	default:
		return uuid.Nil, err
	}
}

// garantirSuperAdmin procura POR E-MAIL na raiz (unicidade por organization):
// encontrado = idempotência; ausente = criação pelo service do subdomínio
// (política de senha + bcrypt ficam dentro dele).
func garantirSuperAdmin(ctx context.Context, servico dominioUsuario.Service, repo dominioUsuario.Repository, email modeluser.Email, senha string) (uuid.UUID, error) {
	u, err := repo.BuscarPorEmail(ctx, email)
	switch {
	case err == nil:
		slog.Info("[SEED] super_admin já existe", "user_uuid", u.UUID.String())
		return u.UUID, nil
	case errors.Is(err, dominioUsuario.ErrNotFound):
		u, err := servico.Create(ctx, dominioUsuario.EntradaCriacao{
			Dados: modeluser.CreateInput{Nome: nomeSuperAdminSeed, Email: email.String()},
			Senha: senha,
		})
		if err != nil {
			return uuid.Nil, fmt.Errorf("provisionamento: super_admin recusado: %w", err)
		}
		slog.Info("[SEED] super_admin criado", "user_uuid", u.UUID.String())
		return u.UUID, nil
	default:
		return uuid.Nil, err
	}
}

// garantirAtribuicaoSuperAdmin liga super_admin × workspace inicial pelo
// service (validação do tripé + auditoria); já existente = idempotência.
func garantirAtribuicaoSuperAdmin(ctx context.Context, servico dominioUsuario.Service, db *gorm.DB, usuarioUUID, workspaceUUID uuid.UUID) error {
	atribuicoes, err := servico.Atribuicoes(ctx, usuarioUUID)
	if err != nil {
		return err
	}
	for _, a := range atribuicoes {
		if a.PapelNome == papelSuperAdmin && a.WorkspaceUUID == workspaceUUID {
			slog.Info("[SEED] atribuição do super_admin já existe",
				"user_uuid", usuarioUUID.String(), "workspace_uuid", workspaceUUID.String())
			return nil
		}
	}
	var papel struct {
		UUID uuid.UUID
	}
	if err := db.WithContext(ctx).Table("identidade_user_papel").Select("uuid").
		Where("nome = ?", papelSuperAdmin).Scan(&papel).Error; err != nil {
		return err
	}
	if papel.UUID == uuid.Nil {
		return errors.New("provisionamento: papel super_admin ausente — rode o seed antes do provisionamento")
	}
	if _, err := servico.AtribuirPapel(ctx, usuarioUUID, workspaceUUID, papel.UUID); err != nil {
		return fmt.Errorf("provisionamento: atribuição do super_admin recusada: %w", err)
	}
	slog.Info("[SEED] super_admin atribuído ao workspace inicial",
		"user_uuid", usuarioUUID.String(), "workspace_uuid", workspaceUUID.String())
	return nil
}

// validadorWorkspacesSeed liga o contrato ValidadorWorkspaces do subdomínio
// user ao REPOSITÓRIO PURO do irmão workspace — no caminho da CLI de seed o
// singleton do processo não existe (sync.Once é do serve). A regra é a MESMA
// do adaptador validadorWorkspaces{} do boot: BuscarPorUUID escopa pela
// organization do ctx, então "não encontrado" cobre inexistente/alheio; falta
// só conferir a vitalidade.
type validadorWorkspacesSeed struct{ repo dominioWorkspace.Repository }

func (v validadorWorkspacesSeed) Pertence(ctx context.Context, organizationUUID, workspaceUUID uuid.UUID) (bool, error) {
	w, err := v.repo.BuscarPorUUID(orgctx.WithOrganization(ctx, organizationUUID), workspaceUUID)
	if err != nil {
		if errors.Is(err, dominioWorkspace.ErrNotFound) {
			return false, nil // inexistente/alheio/inativo: mesma recusa
		}
		return false, err
	}
	return w.Status == modelworkspace.StatusAtivo, nil
}

var _ dominioUsuario.ValidadorWorkspaces = validadorWorkspacesSeed{}

// semearPapeis insere papéis e permissões com ON CONFLICT DO NOTHING — rodar
// duas vezes não duplica nem altera papel existente.
func semearPapeis(ctx context.Context, db *gorm.DB) error {
	for _, papel := range papeisSeed {
		uuidPapel := uuid.New()
		res := db.WithContext(ctx).Exec(`
INSERT INTO identidade_user_papel (uuid, nome, descricao)
VALUES (?, ?, ?)
ON CONFLICT (nome) DO NOTHING`, uuidPapel, papel.nome, papel.descricao)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			// Papel já existe: garante uuid dele para as permissões abaixo.
			var existente struct{ UUID uuid.UUID }
			if err := db.WithContext(ctx).
				Table("identidade_user_papel").
				Select("uuid").
				Where("nome = ?", papel.nome).
				Scan(&existente).Error; err != nil {
				return err
			}
			uuidPapel = existente.UUID
		}
		for _, permissao := range papel.permissoes {
			if err := db.WithContext(ctx).Exec(`
INSERT INTO identidade_user_papel_permissao (papel_uuid, permissao)
VALUES (?, ?)
ON CONFLICT (papel_uuid, permissao) DO NOTHING`, uuidPapel, permissao).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
