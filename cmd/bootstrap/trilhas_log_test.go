package bootstrap

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dominioOrganizacao "workspace-api/internal/identidade/domain/organization"
	dominioUsuario "workspace-api/internal/identidade/domain/user"
	orgmodel "workspace-api/internal/identidade/model/organization"
	"workspace-api/internal/pkg/log/audit_log"
)

// trilhaFake captura os eventos de auditoria — dublê do destino que o
// bootstrap liga (writer do ClickHouse em produção).
type trilhaFake struct {
	eventos []audit_log.Evento
}

func (f *trilhaFake) Registrar(ev audit_log.Evento) { f.eventos = append(f.eventos, ev) }

// TestIniciarTrilhasDegradadaUsaStdout: sem clickhouse na config, os destinos
// existem (SlogPadrao) e o escritor é nil — consumidor trata a ausência.
func TestIniciarTrilhasDegradadaUsaStdout(t *testing.T) {
	configParaOrganization(t)
	trilhas, err := iniciarTrilhas()
	require.NoError(t, err)
	require.Nil(t, trilhas.escritor)
	require.NotNil(t, trilhas.auditoria)
	require.NotNil(t, trilhas.acesso)
}

// TestEscritasAuditadasNaTrilhaAssincrona: a escrita do subdomínio chega ao
// destino com o payload montado à mão intacto (domínio, subdomínio, ação,
// sucesso, identificadores) — a migração da trilha não perdeu nada.
func TestEscritasAuditadasNaTrilhaAssincrona(t *testing.T) {
	if !dockerDisponivel(t) {
		t.Skip("docker indisponível — teste de integração pulado")
	}
	amb := subirAmbiente(t)
	configParaOrganization(t)

	fake := &trilhaFake{}
	svcUser := dominioUsuario.NewService(
		dominioUsuario.NewRepository(amb.db),
		dominioUsuario.NewRepositorioAtribuicoes(amb.db),
		validadorSemprePertence{}, dominioUsuario.NovasCredenciaisBcrypt())
	svcOrg := dominioOrganizacao.NewService(
		dominioOrganizacao.NewRepository(amb.db),
		dominioOrganizacao.NewRepositorioApiKeys(amb.db),
		suspensorNulo{}, encerradorLocal{svcUser},
		func() (string, error) { return "plataforma.teste", nil },
		dominioOrganizacao.ComTrilha(fake))

	_, err := svcOrg.Create(amb.ctx, orgmodel.CreateInput{Nome: "Trilha"})
	require.NoError(t, err)

	require.Len(t, fake.eventos, 1, "toda escrita audita exatamente uma vez")
	ev := fake.eventos[0]
	assert.Equal(t, "identidade", ev.Dominio)
	assert.Equal(t, "organization", ev.Subdominio)
	assert.Equal(t, "criar", ev.Acao)
	assert.True(t, ev.Sucesso)
	assert.NotEmpty(t, ev.Instante)
	assert.NotZero(t, ev.OrganizationUUID, "uuid da organização criada no payload")
}
