package logs

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"workspace-api/internal/infra/clickhouse"
	"workspace-api/internal/pkg/errobserve"
	"workspace-api/internal/pkg/log/access_log"
	"workspace-api/internal/pkg/log/audit_log"
)

// --- Dublês do enriquecimento (UX2, issue #29) -------------------------------

type trilhaFake struct {
	auditoria []audit_log.Evento
	acesso    []access_log.Evento
	erros     []errobserve.Evento
}

func (f *trilhaFake) Auditoria(context.Context, clickhouse.FiltroTrilha) ([]audit_log.Evento, int64, error) {
	return f.auditoria, int64(len(f.auditoria)), nil
}

func (f *trilhaFake) Acesso(context.Context, clickhouse.FiltroTrilha) ([]access_log.Evento, int64, error) {
	return f.acesso, int64(len(f.acesso)), nil
}

func (f *trilhaFake) Erros(context.Context, clickhouse.FiltroTrilha) ([]errobserve.Evento, int64, error) {
	return f.erros, int64(len(f.erros)), nil
}

type resolvedorFake struct {
	pedidos [][]string
	mapa    map[string]UsuarioLog
	err     error
}

func (r *resolvedorFake) Resolver(_ context.Context, uuids []string) (map[string]UsuarioLog, error) {
	copia := append([]string(nil), uuids...)
	r.pedidos = append(r.pedidos, copia)
	if r.err != nil {
		return nil, r.err
	}
	return r.mapa, nil
}

// Identidades do cenário: duas resolvidas e uma fantasma (sistema/anônimo).
var (
	uana     = "dddddddd-0000-4000-8000-00000000da01"
	ubruno   = "dddddddd-0000-4000-8000-00000000da02"
	fantasma = "dddddddd-0000-4000-8000-00000000da09"
)

func novoResolvedor() *resolvedorFake {
	return &resolvedorFake{mapa: map[string]UsuarioLog{
		uana:   {UUID: uana, Nome: "Ana Lima", Email: "ana@exemplo.com"},
		ubruno: {UUID: ubruno, Nome: "Bruno Reis", Email: "bruno@exemplo.com"},
	}}
}

var ctxLeitura = ctxComRecorte(permsDePlataforma)

// Enriquecimento aplicado nas TRÊS trilhas: linha com usuário conhecido leva
// nome/e-mail; fantasma ou vazia sai com os campos VAZIOS — nunca quebra.
func TestEnriquecimentoUsuariosNasTresTrilhas(t *testing.T) {
	resolvedor := novoResolvedor()
	trilhas := &trilhaFake{
		auditoria: []audit_log.Evento{
			{Acao: "criar", UserUUID: uana},
			{Acao: "remover", UserUUID: fantasma},
			{Acao: "criar"}, // sem usuário (evento de sistema)
		},
		acesso: []access_log.Evento{
			{Metodo: "GET", Status: 200, UserUUID: ubruno},
			{Metodo: "GET", Status: 401},
		},
		erros: []errobserve.Evento{
			{Codigo: "identidade.workspace.slug_em_uso", UserUUID: uana},
		},
	}
	svc := NewService(trilhas, ComResolvedorUsuarios(resolvedor))

	respAud, err := svc.Auditoria(ctxLeitura, clickhouse.FiltroTrilha{}, paginaPadrao)
	require.NoError(t, err)
	require.Len(t, respAud.Items, 3)
	assert.Equal(t, "Ana Lima", respAud.Items[0].UserNome)
	assert.Equal(t, "ana@exemplo.com", respAud.Items[0].UserEmail)
	assert.Empty(t, respAud.Items[1].UserNome, "fantasma fica vazio")
	assert.Empty(t, respAud.Items[1].UserEmail)
	assert.Empty(t, respAud.Items[2].UserUUID)
	assert.Empty(t, respAud.Items[2].UserNome, "linha de sistema fica vazia")

	respAcesso, err := svc.Acesso(ctxLeitura, clickhouse.FiltroTrilha{}, paginaPadrao)
	require.NoError(t, err)
	require.Len(t, respAcesso.Items, 2)
	assert.Equal(t, "Bruno Reis", respAcesso.Items[0].UserNome)
	assert.Equal(t, "bruno@exemplo.com", respAcesso.Items[0].UserEmail)
	assert.Empty(t, respAcesso.Items[1].UserEmail)

	respErros, err := svc.Erros(ctxLeitura, clickhouse.FiltroTrilha{}, paginaPadrao)
	require.NoError(t, err)
	require.Len(t, respErros.Items, 1)
	assert.Equal(t, "Ana Lima", respErros.Items[0].UserNome)
}

// Resolução é EM LOTE com uuids DISTINTOS: duas linhas da mesma pessoa geram
// UMA chamada com um único uuid (não N consultas por página).
func TestResolucaoEmLoteComUUIDsDistintos(t *testing.T) {
	resolvedor := novoResolvedor()
	trilhas := &trilhaFake{
		auditoria: []audit_log.Evento{
			{Acao: "criar", UserUUID: uana},
			{Acao: "editar", UserUUID: uana},
			{Acao: "remover", UserUUID: ubruno},
		},
	}
	svc := NewService(trilhas, ComResolvedorUsuarios(resolvedor))

	_, err := svc.Auditoria(ctxLeitura, clickhouse.FiltroTrilha{}, paginaPadrao)
	require.NoError(t, err)
	require.Len(t, resolvedor.pedidos, 1, "uma resolução por consulta")
	assert.ElementsMatch(t, []string{uana, ubruno}, resolvedor.pedidos[0])
}

// Página sem usuário referenciado NÃO chama o resolvedor.
func TestSemUsuarioNaPaginaNaoResolve(t *testing.T) {
	resolvedor := novoResolvedor()
	trilhas := &trilhaFake{auditoria: []audit_log.Evento{{Acao: "criar"}}}
	svc := NewService(trilhas, ComResolvedorUsuarios(resolvedor))

	_, err := svc.Auditoria(ctxLeitura, clickhouse.FiltroTrilha{}, paginaPadrao)
	require.NoError(t, err)
	assert.Empty(t, resolvedor.pedidos)
}

// Enriquecimento é COMPLEMENTAR: falha do resolvedor (e ausência dele) nunca
// derruba a consulta — as linhas seguem com os campos vazios.
func TestEnriquecimentoDegradadoNuncaDerrubaConsulta(t *testing.T) {
	trilhas := &trilhaFake{auditoria: []audit_log.Evento{{Acao: "criar", UserUUID: uana}}}

	comFalha := &resolvedorFake{err: errors.New("postgres indisponível")}
	svc := NewService(trilhas, ComResolvedorUsuarios(comFalha))
	resp, err := svc.Auditoria(ctxLeitura, clickhouse.FiltroTrilha{}, paginaPadrao)
	require.NoError(t, err, "falha do enriquecimento não sobe ao chamador")
	require.Len(t, resp.Items, 1)
	assert.Equal(t, uana, resp.Items[0].UserUUID, "uuid original preservado")
	assert.Empty(t, resp.Items[0].UserNome)
	assert.Empty(t, resp.Items[0].UserEmail)

	semResolvedor := NewService(trilhas) // Dependencias.Usuarios == nil
	resp, err = semResolvedor.Auditoria(ctxLeitura, clickhouse.FiltroTrilha{}, paginaPadrao)
	require.NoError(t, err)
	require.Len(t, resp.Items, 1)
	assert.Empty(t, resp.Items[0].UserNome)
}
