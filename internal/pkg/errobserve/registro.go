package errobserve

import (
	"sort"
	"sync"
)

// Estado global do pacote (padrão singleton documentado no AGENTS.md raiz):
//
//   - observadores REGISTRADOS (For de cada subdomínio, na importação/boot) —
//     fonte do catálogo agregado consumido pelo bootstrap e pela CLI;
//   - SINKS ligados — o slog padrão é SEMPRE o primeiro da lista (garantia
//     estrutural de DefinirSinks); ClickHouse e alerta entram por ele.
//
// Um único RWMutex protege os dois mapas: Observe tira snapshot em read lock
// (caminho quente), boot/testes escrevem em write lock.
var (
	estadoMu     sync.RWMutex
	observadores = map[string]*Observador{} // "dominio.subdominio" → observador registrado
	sinks        = []Sink{SlogPadrao()}     // slog sempre ativo — nunca removido
	falhasSink   uint64                     // pânico/erro recuperado num sink (telemetria perdida, contada)
)

// registrar inscreve o observador no catálogo global. Registro duplicado com
// OUTRA instância PANICA (nunca sobrescrita silenciosa — mesmo espírito do
// RegistrarCatalogo do rest_err); a MESMA instância é idempotente (vars de
// pacote inicializam uma vez por processo).
func registrar(obs *Observador) {
	estadoMu.Lock()
	defer estadoMu.Unlock()
	chave := obs.dominio + "." + obs.subdominio
	if existente, ok := observadores[chave]; ok && existente != obs {
		panic("errobserve: observador duplicado para " + chave)
	}
	observadores[chave] = obs
}

// DefinirSinks liga os sinks EXTRA da plataforma (ClickHouse da E2, alerta
// agregado) — chamado UMA vez pelo cmd/bootstrap. O sink slog padrão é
// SEMPRE mantido como primeiro da lista: telemetria degradada nunca fica sem
// destino. Nil ou entrada repetida nos extras são ignorados.
func DefinirSinks(extras ...Sink) {
	estadoMu.Lock()
	defer estadoMu.Unlock()
	lista := make([]Sink, 0, len(extras)+1)
	vistos := map[Sink]bool{nil: true}
	for _, extra := range extras {
		if vistos[extra] {
			continue
		}
		vistos[extra] = true
		lista = append(lista, extra)
	}
	sinks = append([]Sink{SlogPadrao()}, lista...)
}

// despachar entrega o evento a todos os sinks do snapshot corrente. Cada
// chamada roda com recuperação de pânico própria: um sink quebrado perde o
// evento dele (contado), jamais o negócio nem os outros sinks.
func despachar(evento Evento) {
	estadoMu.RLock()
	destinos := make([]Sink, len(sinks))
	copy(destinos, sinks)
	estadoMu.RUnlock()
	for _, destino := range destinos {
		entregar(destino, evento)
	}
}

func entregar(destino Sink, evento Evento) {
	defer func() {
		if recuperado := recover(); recuperado != nil {
			estadoMu.Lock()
			falhasSink++
			estadoMu.Unlock()
		}
	}()
	destino.Registrar(evento)
}

// CatalogoGlobal devolve o vocabulário agregado dos observadores registrados,
// agrupado por dominio/subdominio e ORDENADO deterministicamente — é o que o
// bootstrap converte para GET /api/system/eventos e o CLI imprime.
func CatalogoGlobal() []GrupoErrosObservados {
	estadoMu.RLock()
	grupos := make([]GrupoErrosObservados, 0, len(observadores))
	for _, obs := range observadores {
		grupos = append(grupos, GrupoErrosObservados{
			Dominio:    obs.dominio,
			Subdominio: obs.subdominio,
			Erros:      obs.Catalogo(),
		})
	}
	estadoMu.RUnlock()
	sort.Slice(grupos, func(i, j int) bool {
		if grupos[i].Dominio != grupos[j].Dominio {
			return grupos[i].Dominio < grupos[j].Dominio
		}
		return grupos[i].Subdominio < grupos[j].Subdominio
	})
	return grupos
}

// GrupoErrosObservados reúne o vocabulário de UM subdomínio no agregado.
type GrupoErrosObservados struct {
	Dominio    string
	Subdominio string
	Erros      []EventoMeta
}

// SeveridadeDoCodigo procura a severidade declarada para um código em todo o
// registro (catálogos e CLI); código fora dos observadores devolve "".
func SeveridadeDoCodigo(codigo string) Severidade {
	estadoMu.RLock()
	defer estadoMu.RUnlock()
	for _, obs := range observadores {
		for _, meta := range obs.Catalogo() {
			if meta.Codigo == codigo {
				return Severidade(meta.Severidade)
			}
		}
	}
	return ""
}

// FalhasSink devolve quantas entregas se perderam por pânico de sink —
// contadas, nunca silenciadas.
func FalhasSink() uint64 {
	estadoMu.RLock()
	defer estadoMu.RUnlock()
	return falhasSink
}

// ResetarParaTeste restaura o estado global do processo — uso EXCLUSIVO dos
// testes; nunca chamado pelo processo real.
func ResetarParaTeste() {
	estadoMu.Lock()
	defer estadoMu.Unlock()
	observadores = map[string]*Observador{}
	sinks = []Sink{SlogPadrao()}
	falhasSink = 0
}
