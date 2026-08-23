package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dominioWorkspace "workspace-api/internal/identidade/domain/workspace"
	modelworkspace "workspace-api/internal/identidade/model/workspace"
	"workspace-api/internal/pkg/errobserve"
	"workspace-api/internal/pkg/orgctx"
)

// Teste de COBERTURA DO OBSERVADOR DE ERROS (evolução errobserve): prova
// PONTA A PONTA que todo retorno de erro do service passa pelo observador do
// subdomínio com o código estável do catálogo e a severidade declarada no
// singleton — sem banco (repo dublê) e sem tocar o singleton do processo.
// O erro chega INTACTO ao chamador: telemetria nunca muda a resposta.
//
// Fakes locais mínimos do subdomínio workspace (o menor agregado): só os
// caminhos exercitados aqui precisam de comportamento.

type repoWorkspaceObservavel struct {
	erroAoBuscar error
}

func (r *repoWorkspaceObservavel) Criar(_ context.Context, _ *modelworkspace.Workspace) error {
	return nil
}

func (r *repoWorkspaceObservavel) BuscarPorUUID(_ context.Context, _ uuid.UUID) (*modelworkspace.Workspace, error) {
	return nil, r.erroAoBuscar
}

func (r *repoWorkspaceObservavel) BuscarPorSlug(_ context.Context, _ modelworkspace.Slug) (*modelworkspace.Workspace, error) {
	return nil, r.erroAoBuscar
}

func (r *repoWorkspaceObservavel) BuscarPorUUIDGlobal(_ context.Context, _ uuid.UUID) (*modelworkspace.Workspace, error) {
	return nil, r.erroAoBuscar
}

func (r *repoWorkspaceObservavel) Listar(_ context.Context, _ modelworkspace.ListFilter) ([]modelworkspace.Workspace, int64, error) {
	return nil, 0, nil
}

func (r *repoWorkspaceObservavel) Atualizar(_ context.Context, _ *modelworkspace.Workspace) error {
	return nil
}

func (r *repoWorkspaceObservavel) Remover(_ context.Context, _ uuid.UUID) error {
	return nil
}

func (r *repoWorkspaceObservavel) SuspenderPorOrganization(_ context.Context, _ uuid.UUID) (int64, error) {
	return 0, nil
}

func TestObservadorDeErrosCobreRetornosDoService(t *testing.T) {
	silenciarSlog(t)
	// O REGISTRO global (observadores dos subdomínios, criados na importação)
	// fica intacto — só os SINKS são trocados pelo coletor do teste e
	// devolvidos ao padrão no fim (DefinirSinks sem extras = só o slog).
	t.Cleanup(func() { errobserve.DefinirSinks() })

	coletor := &sinkColetorLocal{}
	errobserve.DefinirSinks(coletor)

	ctx := orgctx.WithOrganization(context.Background(), uuid.New())

	// NewService devolve o service DECORADO: singleton, seed e testes
	// observam igualmente pelo mesmo caminho.
	repo := &repoWorkspaceObservavel{erroAoBuscar: dominioWorkspace.ErrNotFound}
	service := dominioWorkspace.NewService(repo, nil)

	// 1) Sentinela catalogada → evento com código/severidade do catálogo,
	//    erro INTACTO no retorno.
	_, errLeitura := service.Read(ctx, uuid.New())
	require.ErrorIs(t, errLeitura, dominioWorkspace.ErrNotFound)
	require.Len(t, coletor.eventos, 1)
	evento := coletor.eventos[0]
	assert.Equal(t, "identidade.workspace.nao_encontrado", evento.Codigo)
	assert.Equal(t, string(errobserve.SeveridadeWarn), string(evento.Severidade))
	assert.False(t, evento.Desconhecido)
	assert.Equal(t, "identidade", evento.Dominio)
	assert.Equal(t, "workspace", evento.Subdominio)

	// 2) Erro fora do catálogo do subdomínio → DESCONHECIDO + critical.
	falhaEstranha := errors.New("falha de driver sem nome")
	repo.erroAoBuscar = falhaEstranha
	_, errLeitura = service.Read(ctx, uuid.New())
	require.ErrorIs(t, errLeitura, falhaEstranha)
	require.Len(t, coletor.eventos, 2)
	evento = coletor.eventos[1]
	assert.True(t, evento.Desconhecido)
	assert.Equal(t, errobserve.CodigoDesconhecido, evento.Codigo)
	assert.Equal(t, string(errobserve.SeveridadeCritical), string(evento.Severidade))

	// 3) Sucesso NÃO emite evento.
	_, _, err := service.List(ctx, modelworkspace.ListFilter{})
	require.NoError(t, err)
	assert.Len(t, coletor.eventos, 2)

	// 4) O vocabulário do subdomínio está no catálogo global — é o que sai em
	//    GET /api/system/eventos e na CLI.
	encontrou := false
	for _, grupo := range errobserve.CatalogoGlobal() {
		if grupo.Dominio == "identidade" && grupo.Subdominio == "workspace" {
			for _, meta := range grupo.Erros {
				if meta.Codigo == "identidade.workspace.slug_em_uso" {
					encontrou = true
				}
			}
		}
	}
	assert.True(t, encontrou, "catálogo global deve expor os códigos do workspace")
}

// TestObservadorPlataformaClassificaSentinelasProprias prova a face plataforma
// do namespace reservado: degradação = warn com o código dela; migrations =
// critical; qualquer outro erro cai como desconhecido/critical.
func TestObservadorPlataformaClassificaSentinelasProprias(t *testing.T) {
	silenciarSlog(t)
	t.Cleanup(func() { errobserve.DefinirSinks() })

	coletor := &sinkColetorLocal{}
	errobserve.DefinirSinks(coletor)

	observadorPlataforma().Observe(context.Background(),
		fmt.Errorf("redis inacessível: %w", errobserve.ErrDegradacao))
	observadorPlataforma().Observe(context.Background(),
		fmt.Errorf("versão suja: %w", errobserve.ErrMigracao))

	require.Len(t, coletor.eventos, 2)
	assert.Equal(t, "sistema.degradacao_dependencia", coletor.eventos[0].Codigo)
	assert.Equal(t, string(errobserve.SeveridadeWarn), string(coletor.eventos[0].Severidade))
	assert.Equal(t, "sistema.migrations.up", coletor.eventos[1].Codigo)
	assert.Equal(t, string(errobserve.SeveridadeCritical), string(coletor.eventos[1].Severidade))

	// O vocabulário fixo da PLATAFORMA fica visível nos catálogos mesmo sem
	// emissão (boot/shutdown são mapping, não telemetria hoje).
	grupos := errobserve.CatalogoSistema()
	require.NotEmpty(t, grupos)
	vistos := map[string]bool{}
	for _, meta := range grupos {
		vistos[meta.Codigo] = true
	}
	for _, codigo := range []string{"sistema.boot", "sistema.shutdown"} {
		assert.True(t, vistos[codigo], "evento de plataforma %q ausente do catálogo fixo", codigo)
	}
}

type sinkColetorLocal struct{ eventos []errobserve.Evento }

func (s *sinkColetorLocal) Registrar(ev errobserve.Evento) { s.eventos = append(s.eventos, ev) }

// silenciarSlog evita poluir a saída dos testes com os eventos observados.
func silenciarSlog(t *testing.T) {
	t.Helper()
	anterior := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { slog.SetDefault(anterior) })
}
