// Seed da plataforma: dados mínimos IDEMPOTENTES, rodados SÓ via
// `workspace-api seed` — nunca automáticos no boot (agents/02).
//
// Os 5 papéis globais e seus conjuntos exatos de permissões vêm do doc 03:
// strings estáveis que os subdomínios declaram como constantes PermX nas
// fases seguintes — divergência entre seed e PermX é bug de contrato.
package bootstrap

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"workspace-api/internal/infra/database/postgres"
)

// Nomes canônicos dos papéis seed (tabela identidade_user_papel.nome).
const (
	papelSuperAdmin        = "super_admin"
	papelAdminOrganization = "admin_organization"
	papelAdminWorkspace    = "admin_workspace"
	papelUsuarioWorkspace  = "usuario_workspace"
	papelSomenteLeitura    = "somente_leitura"
)

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
		descricao: "Dono do contrato: administra workspaces, usuários e as chaves de API da organization; suporte em qualquer workspace da própria.",
		permissoes: []string{
			"identidade:workspace:*",
			"identidade:user:*",
			"identidade:organization:gerenciar_apikeys",
		},
	},
	{
		nome:      papelAdminWorkspace,
		descricao: "Administrador do workspace: tudo dentro dos workspaces em que exerce o papel, menos mudar a estrutura do contrato.",
		permissoes: []string{
			"identidade:workspace:editar",
			"identidade:user:*",
		},
	},
	{
		nome:      papelUsuarioWorkspace,
		descricao: "Operador: trabalha no workspace sem administrar identidade; ações operacionais chegam com os subdomínios de negócio.",
		permissoes: []string{
			"identidade:workspace:ler",
		},
	},
	{
		nome:      papelSomenteLeitura,
		descricao: "Consulta sem escrita: só as ações :ler.",
		permissoes: []string{
			"identidade:organization:ler",
			"identidade:workspace:ler",
			"identidade:user:ler",
		},
	},
}

// Seed executa todos os seeds do template sobre o banco do processo.
func Seed(caminhoConfig string) error {
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
	return semearOrganizacaoRaiz(ctx, db)
}

// semearOrganizacaoRaiz cria a organization da PLATAFORMA (raiz do console
// master) com uuid determinístico — rodar duas vezes não duplica nem altera.
func semearOrganizacaoRaiz(ctx context.Context, db *gorm.DB) error {
	uuidRaiz := uuid.NewSHA1(uuid.NameSpaceURL, []byte("workspace-api://organizacao-raiz"))
	return db.WithContext(ctx).Exec(`
INSERT INTO identidade_organization_organization (uuid, nome, status)
VALUES (?, ?, 'ativo')
ON CONFLICT (uuid) DO NOTHING`, uuidRaiz, "Plataforma").Error
}

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
