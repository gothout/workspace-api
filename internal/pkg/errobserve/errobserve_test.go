package errobserve

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"

	"workspace-api/internal/pkg/orgctx"
)

// uuidFixo gera UUID determinístico para os metadados do ctx.
func uuidFixo(n int) uuid.UUID {
	return uuid.MustParse(fmt.Sprintf("00000000-0000-0000-0000-%012d", n))
}

// Sentinelas de teste — locais do pacote, sem colisão com os subdomínios.
var (
	errSlugEmUso     = errors.New("slug em uso")
	errNaoEncontrado = errors.New("não encontrado")
)

func entradasDeExemplo() []Entrada {
	return []Entrada{
		{Erro: errSlugEmUso, Codigo: "identidade.workspace.slug_em_uso", Mensagem: "Slug em uso.", Severidade: SeveridadeWarn},
		{Erro: errNaoEncontrado, Codigo: "identidade.workspace.nao_encontrado", Mensagem: "Não encontrado.", Severidade: SeveridadeWarn},
	}
}

type sinkColetor struct {
	mutex   sync.Mutex
	eventos []Evento
	pânico  bool
}

func (s *sinkColetor) Registrar(ev Evento) {
	if s.pânico {
		panic("sink quebrado de propósito")
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.eventos = append(s.eventos, ev)
}

func novoObservadorTeste(t *testing.T) (*Observador, *sinkColetor) {
	t.Helper()
	ResetarParaTeste()
	coletor := &sinkColetor{}
	DefinirSinks(coletor)
	obs := For("identidade", "workspace", entradasDeExemplo())
	return obs, coletor
}

func TestNovoValidaEntradas(t *testing.T) {
	casos := []struct {
		nome     string
		dominio  string
		sub      string
		entradas []Entrada
	}{
		{"dominio vazio", "", "workspace", entradasDeExemplo()},
		{"subdominio vazio", "identidade", " ", entradasDeExemplo()},
		{"sentinela nil", "identidade", "workspace", []Entrada{{Codigo: "x.y", Severidade: SeveridadeWarn}}},
		{"código vazio", "identidade", "workspace", []Entrada{{Erro: errSlugEmUso, Severidade: SeveridadeWarn}}},
		{"severidade inválida", "identidade", "workspace", []Entrada{{Erro: errSlugEmUso, Codigo: "a.b", Severidade: "fatal"}}},
		{"código duplicado", "identidade", "workspace", []Entrada{
			{Erro: errSlugEmUso, Codigo: "a.b", Severidade: SeveridadeWarn},
			{Erro: errNaoEncontrado, Codigo: "a.b", Severidade: SeveridadeError},
		}},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			if _, err := Novo(caso.dominio, caso.sub, caso.entradas); err == nil {
				t.Fatalf("esperava erro de validação em %q", caso.nome)
			}
		})
	}
}

// O namespace reservado é INVIOLÁVEL para negócio: dominio sistema e códigos
// com prefixo sistema. reprova em boot/teste (critério da issue errobserve).
func TestNamespaceReservadoReprovado(t *testing.T) {
	ResetarParaTeste()

	if _, err := Novo(NamespaceReservado, "workspace", entradasDeExemplo()); err == nil {
		t.Fatal("dominio sistema deveria ser recusado a subdomínio de negócio")
	}
	if _, err := Novo("identidade", "workspace", []Entrada{
		{Erro: errSlugEmUso, Codigo: "sistema.boot", Severidade: SeveridadeWarn},
	}); err == nil {
		t.Fatal("código com prefixo sistema. deveria ser recusado fora da plataforma")
	}
	assertPanica(t, func() { For(NamespaceReservado, "workspace", entradasDeExemplo()) })

	// A plataforma legítima usa ObservadorPlataforma e EXIGE o prefixo.
	obs := ObservadorPlataforma([]Entrada{
		{Erro: ErrDegradacao, Codigo: "sistema.degradacao_dependencia", Mensagem: "Dependência degradada.", Severidade: SeveridadeWarn},
	})
	if obs.Dominio() != NamespaceReservado || obs.Subdominio() != "plataforma" {
		t.Fatalf("observador de plataforma montado errado: %s/%s", obs.Dominio(), obs.Subdominio())
	}
	assertPanica(t, func() {
		ObservadorPlataforma([]Entrada{{Erro: errors.New("x"), Codigo: "fora.do.namespace", Severidade: SeveridadeError}})
	})

	// E o vocabulário fixo existe mesmo sem emissão (visível nos catálogos).
	if len(CatalogoSistema()) < 4 {
		t.Fatalf("vocabulário de plataforma incompleto: %d entradas", len(CatalogoSistema()))
	}
}

func assertPanica(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("esperava pânico de validação")
		}
	}()
	fn()
}

// Observe devolve o erro INTACTO e emite o evento com código/severidade do
// catálogo — inclusive com wrapping (errors.Is atravessa fmt.Errorf %w).
func TestObserveCasamentoSentinela(t *testing.T) {
	obs, coletor := novoObservadorTeste(t)
	ctx := orgctx.WithRayTrace(
		orgctx.WithUser(
			orgctx.WithWorkspace(
				orgctx.WithOrganization(context.Background(), uuidFixo(1)), uuidFixo(2)),
			uuidFixo(3)),
		"ray-123")

	envolvido := fmt.Errorf("persistindo workspace: %w", errSlugEmUso)
	devolvido := obs.Observe(ctx, envolvido)
	if !errors.Is(devolvido, errSlugEmUso) || devolvido != envolvido {
		t.Fatalf("erro alterado pelo observador: %v", devolvido)
	}
	if len(coletor.eventos) != 1 {
		t.Fatalf("esperava 1 evento, recebi %d", len(coletor.eventos))
	}
	ev := coletor.eventos[0]
	if ev.Codigo != "identidade.workspace.slug_em_uso" || ev.Severidade != SeveridadeWarn ||
		ev.Desconhecido || ev.Mensagem != "Slug em uso." {
		t.Fatalf("evento mal classificado: %+v", ev)
	}
	if ev.Dominio != "identidade" || ev.Subdominio != "workspace" {
		t.Fatalf("evento sem identidade do emissor: %+v", ev)
	}
	if ev.RayTrace != "ray-123" ||
		ev.OrganizationUUID != uuidFixo(1).String() ||
		ev.WorkspaceUUID != uuidFixo(2).String() ||
		ev.UserUUID != uuidFixo(3).String() {
		t.Fatalf("metadados do ctx perdidos: %+v", ev)
	}
	if ev.Causa != envolvido.Error() {
		t.Fatalf("causa não preservada: %q", ev.Causa)
	}
}

// Sentinela FORA do catálogo do subdomínio vira desconhecido + critical — o
// pior caso até prova em contrário (critério da issue).
func TestObserveDesconhecidoCritical(t *testing.T) {
	obs, coletor := novoObservadorTeste(t)

	desconhecido := errors.New("falha de driver sem nome")
	if devolvido := obs.Observe(context.Background(), desconhecido); devolvido != desconhecido {
		t.Fatalf("erro alterado: %v", devolvido)
	}
	if len(coletor.eventos) != 1 {
		t.Fatalf("esperava 1 evento, recebi %d", len(coletor.eventos))
	}
	ev := coletor.eventos[0]
	if !ev.Desconhecido || ev.Severidade != SeveridadeCritical || ev.Codigo != CodigoDesconhecido {
		t.Fatalf("desconhecido mal classificado: %+v", ev)
	}
}

func TestObserveNilNaoEmite(t *testing.T) {
	obs, coletor := novoObservadorTeste(t)
	if err := obs.Observe(context.Background(), nil); err != nil {
		t.Fatalf("nil virou erro: %v", err)
	}
	if len(coletor.eventos) != 0 {
		t.Fatalf("nil emitiu evento: %+v", coletor.eventos)
	}
}

// Telemetria quebrada NUNCA quebra o negócio: sink que entra em pânico perde
// o evento dele (contado), os outros sinks seguem recebendo.
func TestSinkComPanicoNaoPropaga(t *testing.T) {
	ResetarParaTeste()
	quebrado := &sinkColetor{pânico: true}
	sadio := &sinkColetor{}
	DefinirSinks(quebrado, sadio)
	obs := For("identidade", "workspace", entradasDeExemplo())

	if devolvido := obs.Observe(context.Background(), errSlugEmUso); devolvido != errSlugEmUso {
		t.Fatalf("pânico de sink vazou para o chamador: %v", devolvido)
	}
	if FalhasSink() != 1 {
		t.Fatalf("falha de sink não contada: %d", FalhasSink())
	}
	if len(sadio.eventos) != 1 {
		t.Fatalf("sink sadio deixou de receber: %d eventos", len(sadio.eventos))
	}
}

// DefinirSinks mantém o slog SEMPRE como primeiro destino (garantia
// estrutural), ignora nil e repetidos, e o catálogo global sai ordenado.
func TestDefinirSinksECatalogoGlobal(t *testing.T) {
	ResetarParaTeste()
	fake := &sinkColetor{}
	DefinirSinks(nil, fake, fake)

	For("identidade", "workspace", entradasDeExemplo())
	For("identidade", "organization", []Entrada{
		{Erro: errNaoEncontrado, Codigo: "identidade.organization.nao_encontrado", Mensagem: "Org ausente.", Severidade: SeveridadeError},
	})

	grupos := CatalogoGlobal()
	if len(grupos) != 2 {
		t.Fatalf("esperava 2 grupos, recebi %d", len(grupos))
	}
	if grupos[0].Dominio != "identidade" || grupos[0].Subdominio != "organization" {
		t.Fatalf("ordem determinística quebrada: %+v", grupos[0])
	}
	if grupos[0].Erros[0].Severidade != string(SeveridadeError) {
		t.Fatalf("severidade perdida no agregado: %+v", grupos[0].Erros[0])
	}
	if sev := SeveridadeDoCodigo("identidade.workspace.slug_em_uso"); sev != SeveridadeWarn {
		t.Fatalf("consulta de severidade falhou: %q", sev)
	}
	if sev := SeveridadeDoCodigo("codigo.fora.do.registro"); sev != "" {
		t.Fatalf("código inexistente devolveu severidade: %q", sev)
	}

	// Registro duplicado com OUTRA instância panica (nunca sobrescrita).
	assertPanica(t, func() { For("identidade", "workspace", entradasDeExemplo()) })
}
