// Package orgctx carrega o escopo da hierarquia organization → workspace →
// user no contexto da requisição e aplica esse escopo às queries do banco.
//
// Regra central (fail-closed): TODO repository de negócio passa por Scope ou
// ScopeOrganization; contexto sem escopo FAZ A QUERY FALHAR
// (ErrEscopoAusente) — nunca roda aberta varrendo a tabela inteira.
package orgctx

import (
	"context"
	"errors"
	"sort"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ErrEscopoAusente é devolvido pela query quando o contexto não traz o
// escopo mínimo exigido — a falha é proposital (fail-closed).
var ErrEscopoAusente = errors.New("escopo de tenancy ausente no contexto: query recusada (fail-closed)")

type chave int

const (
	chaveOrganizationUUID chave = iota
	chaveWorkspaceUUID
	chaveUserUUID
	chaveRayTrace
	chavePermissoes
	chaveAplicacao
)

// WithOrganization injeta a organization resolvida da requisição.
func WithOrganization(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, chaveOrganizationUUID, id)
}

// WithWorkspace injeta o workspace ativo da requisição.
func WithWorkspace(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, chaveWorkspaceUUID, id)
}

// WithUser injeta o usuário autenticado da requisição.
func WithUser(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, chaveUserUUID, id)
}

// WithRayTrace injeta o identificador de correlação da requisição.
func WithRayTrace(ctx context.Context, ray string) context.Context {
	return context.WithValue(ctx, chaveRayTrace, ray)
}

// WithPermissoes injeta as permissões efetivas resolvidas para a requisição.
func WithPermissoes(ctx context.Context, permissoes []string) context.Context {
	conjunto := make(map[string]struct{}, len(permissoes))
	for _, p := range permissoes {
		conjunto[p] = struct{}{}
	}
	return context.WithValue(ctx, chavePermissoes, conjunto)
}

// WithAplicacao injeta o slug do módulo selecionado na requisição — validado
// e injetado pelo RequireAplicacao do middleware; ausente = requisição sem
// contexto de aplicação (rotas core da plataforma).
func WithAplicacao(ctx context.Context, slug string) context.Context {
	return context.WithValue(ctx, chaveAplicacao, slug)
}

// Aplicacao devolve o slug do módulo ativo na requisição (vazio se ausente).
func Aplicacao(ctx context.Context) string {
	v, _ := ctx.Value(chaveAplicacao).(string)
	return v
}

func valorUUID(ctx context.Context, k chave) uuid.UUID {
	v, _ := ctx.Value(k).(uuid.UUID)
	return v
}

// OrganizationUUID devolve a organization do ctx (uuid.Nil se ausente).
func OrganizationUUID(ctx context.Context) uuid.UUID {
	return valorUUID(ctx, chaveOrganizationUUID)
}

// WorkspaceUUID devolve o workspace do ctx (uuid.Nil se ausente).
func WorkspaceUUID(ctx context.Context) uuid.UUID {
	return valorUUID(ctx, chaveWorkspaceUUID)
}

// UserUUID devolve o usuário do ctx (uuid.Nil se ausente).
func UserUUID(ctx context.Context) uuid.UUID {
	return valorUUID(ctx, chaveUserUUID)
}

// RayTrace devolve o correlacionador de log da requisição (vazio se ausente).
func RayTrace(ctx context.Context) string {
	v, _ := ctx.Value(chaveRayTrace).(string)
	return v
}

// TemPermissao confere se a permissão granular está nas efetivas do ctx —
// casamento EXATO; curingas (ex.: identidade:workspace:*) precisam do
// matcher do middleware, que usa Permissoes().
func TemPermissao(ctx context.Context, permissao string) bool {
	conjunto, ok := ctx.Value(chavePermissoes).(map[string]struct{})
	if !ok {
		return false
	}
	_, presente := conjunto[permissao]
	return presente
}

// Permissoes devolve TODAS as permissões efetivas injetadas no ctx, em ordem
// determinística — inclusive curingas; quem interpreta curinga é o matcher
// da autorização, não o repositório.
func Permissoes(ctx context.Context) []string {
	conjunto, ok := ctx.Value(chavePermissoes).(map[string]struct{})
	if !ok {
		return nil
	}
	lista := make([]string, 0, len(conjunto))
	for p := range conjunto {
		lista = append(lista, p)
	}
	sort.Strings(lista)
	return lista
}

// Scope aplica o filtro de tenancy COMPLETO (organization E workspace) à
// query — para tabelas da vida dentro do workspace. Contexto sem os dois
// UUIDs devolve uma query que FALHA com ErrEscopoAusente ao ser executada.
func Scope(db *gorm.DB, ctx context.Context) *gorm.DB {
	org := OrganizationUUID(ctx)
	ws := WorkspaceUUID(ctx)
	if org == uuid.Nil || ws == uuid.Nil {
		return comErroAcumulado(db, ErrEscopoAusente)
	}
	return db.Where("organization_uuid = ?", org).Where("workspace_uuid = ?", ws)
}

// ScopeOrganization aplica só o filtro de organization — variante para
// tabelas que vivem ACIMA do workspace (ex.: workspace, user). Usar fora
// desse caso exige exceção documentada no AGENTS.md do subdomínio.
func ScopeOrganization(db *gorm.DB, ctx context.Context) *gorm.DB {
	org := OrganizationUUID(ctx)
	if org == uuid.Nil {
		return comErroAcumulado(db, ErrEscopoAusente)
	}
	return db.Where("organization_uuid = ?", org)
}

// comErroAcumulado marca a query com um erro que será devolvido na execução —
// o mecanismo fail-closed: gorm acumula o erro e nenhum finisher roda SQL.
func comErroAcumulado(db *gorm.DB, err error) *gorm.DB {
	tx := db.Session(&gorm.Session{})
	_ = tx.AddError(err)
	return tx
}
