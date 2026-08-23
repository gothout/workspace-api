package bootstrap

import (
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	dominioOrganizacao "workspace-api/internal/identidade/domain/organization"
	dominioUsuario "workspace-api/internal/identidade/domain/user"
	dominioWorkspace "workspace-api/internal/identidade/domain/workspace"
	orgmodel "workspace-api/internal/identidade/model/organization"
	modeluser "workspace-api/internal/identidade/model/user"
	modelworkspace "workspace-api/internal/identidade/model/workspace"
	"workspace-api/internal/pkg/orgctx"
)

// --- Invariantes disputadas sob concorrência --------------------------------
//
// Os testes deste arquivo disputam os índices únicos do Postgres com N
// goroutines alinhadas numa barreira de disparo. São as invariantes de
// unicidade do template (slug global do workspace, e-mail por organization)
// — rodam no `go test ./...` com docker e são o alvo direto do
// `go test -race ./...` (doc 06, fase F6).

// aquecerPool abre as conexões do pool ANTES da largada: a disputa mede o
// índice único do Postgres, não o handshake TCP/auth do pgx — sem isso as
// primeiras goroutines pagariam a criação de conexão e a corrida seria
// enviesada para quem chega primeiro.
func aquecerPool(t *testing.T, db *gorm.DB, conexoes int) {
	t.Helper()
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(conexoes + 2)
	sqlDB.SetMaxIdleConns(conexoes + 2)

	falhas := make(chan error, conexoes+2)
	var wg sync.WaitGroup
	for i := 0; i < conexoes+2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			falhas <- db.Exec("SELECT 1").Error
		}()
	}
	wg.Wait()
	close(falhas)
	for err := range falhas {
		require.NoError(t, err, "aquecimento do pool falhou")
	}
}

// disputar alinha `concorrentes` goroutines numa barreira e dispara todas de
// uma vez executando `tentativa`; devolve os erros na ordem de chegada.
func disputar(t *testing.T, concorrentes int, tentativa func() error) []error {
	t.Helper()

	resultados := make(chan error, concorrentes)
	var prontos sync.WaitGroup
	prontos.Add(concorrentes)
	disparo := make(chan struct{})
	var done sync.WaitGroup
	done.Add(concorrentes)
	for i := 0; i < concorrentes; i++ {
		go func() {
			defer done.Done()
			prontos.Done() // sinaliza que está prestes a esperar o disparo
			<-disparo      // todos alinhados antes de disparar
			resultados <- tentativa()
		}()
	}
	prontos.Wait() // garante que todas as goroutines estão na barreira
	close(disparo)
	done.Wait()
	close(resultados)

	saida := make([]error, 0, concorrentes)
	for err := range resultados {
		saida = append(saida, err)
	}
	return saida
}

// contarVitorias separa vitórias (err == nil) dos conflitos esperados;
// qualquer outro erro reprova na hora.
func contarVitorias(t *testing.T, resultados []error, conflitoEsperado error) (vitorias, conflitos int) {
	t.Helper()
	for _, err := range resultados {
		switch {
		case err == nil:
			vitorias++
		case errors.Is(err, conflitoEsperado):
			conflitos++
		default:
			t.Fatalf("erro inesperado na disputa: %v", err)
		}
	}
	return vitorias, conflitos
}

// TestUnicidadeSlugSobConcorrencia exercita o índice único TOTAL do slug sob
// paralelismo: exatamente UMA goroutine cria o slug, as demais recebem
// ErrSlugEmUso — nunca erro genérico, nunca duas vitórias.
func TestUnicidadeSlugSobConcorrencia(t *testing.T) {
	if !dockerDisponivel(t) {
		t.Skip("docker indisponível — teste de integração pulado")
	}
	amb := subirAmbiente(t)

	disputante, err := orgmodel.NewOrganization(orgmodel.CreateInput{Nome: "Disputante"})
	require.NoError(t, err)
	require.NoError(t, dominioOrganizacao.NewRepository(amb.db).Criar(amb.ctx, disputante))

	svc := dominioWorkspace.NewService(dominioWorkspace.NewRepository(amb.db), nil)
	ctxDisputa := orgctx.WithOrganization(amb.ctx, disputante.UUID)

	const concorrentes = 12
	slug := "disputado"
	aquecerPool(t, amb.db, concorrentes)

	resultados := disputar(t, concorrentes, func() error {
		_, err := svc.Create(ctxDisputa, modelworkspace.CreateInput{Nome: "Disputa", Slug: slug})
		return err
	})

	vitorias, conflitos := contarVitorias(t, resultados, dominioWorkspace.ErrSlugEmUso)
	assert.Equal(t, 1, vitorias, "exatamente UM create vence a disputa pelo slug")
	assert.Equal(t, concorrentes-1, conflitos, "demais recebem ErrSlugEmUso (nunca erro genérico)")
}

// TestUnicidadeEmailSobConcorrencia exercita o índice único PARCIAL
// (organization_uuid, email) WHERE deleted_at IS NULL da migration 0006 sob
// paralelismo: exatamente UMA goroutine cadastra o e-mail na organization,
// as demais recebem ErrEmailEmUso.
func TestUnicidadeEmailSobConcorrencia(t *testing.T) {
	if !dockerDisponivel(t) {
		t.Skip("docker indisponível — teste de integração pulado")
	}
	amb := subirAmbiente(t)
	semearOrganization(t, amb, orgA)

	svc := dominioUsuario.NewService(
		dominioUsuario.NewRepository(amb.db),
		dominioUsuario.NewRepositorioAtribuicoes(amb.db),
		validadorSemprePertence{},
		dominioUsuario.NovasCredenciaisBcrypt(),
	)
	ctxDisputa := orgctx.WithOrganization(amb.ctx, orgA)

	const concorrentes = 12
	email := "disputado@exemplo.com"
	aquecerPool(t, amb.db, concorrentes)

	resultados := disputar(t, concorrentes, func() error {
		_, err := svc.Create(ctxDisputa, dominioUsuario.EntradaCriacao{
			Dados: modeluser.CreateInput{Nome: "Disputa", Email: email},
			Senha: "senha-segura-123",
		})
		return err
	})

	vitorias, conflitos := contarVitorias(t, resultados, dominioUsuario.ErrEmailEmUso)
	assert.Equal(t, 1, vitorias, "exatamente UM create vence a disputa pelo e-mail")
	assert.Equal(t, concorrentes-1, conflitos, "demais recebem ErrEmailEmUso (nunca erro genérico)")

	// A mesma disputa em OUTRA organization é permitida: o índice é parcial
	// por organization — o e-mail não é endereço público da plataforma.
	outraOrg, err := orgmodel.NewOrganization(orgmodel.CreateInput{Nome: "Outra"})
	require.NoError(t, err)
	require.NoError(t, dominioOrganizacao.NewRepository(amb.db).Criar(amb.ctx, outraOrg))
	_, err = svc.Create(orgctx.WithOrganization(amb.ctx, outraOrg.UUID), dominioUsuario.EntradaCriacao{
		Dados: modeluser.CreateInput{Nome: "Homônimo", Email: email},
		Senha: "senha-segura-123",
	})
	assert.NoError(t, err, "mesmo e-mail em organization diferente deve passar")
}
