package errobserve

import (
	"log/slog"
	"sync"
	"time"
)

// janelaPadraoAlerta é a agregação quando nada é configurado
// (logs.alerta_janela_seg): críticos repetidos viram UM [ALERTA] por janela.
const janelaPadraoAlerta = 60 * time.Second

// Alerta devolve o sink de ALERTA: só eventos CRITICAL o interessam. O
// primeiro critical de um código dentro da janela emite a linha "[ALERTA]"
// imediata; os seguintes do MESMO código na mesma janela são AGREGADOS (a
// contagem sai no próximo alerta) — incidente grita uma vez, não cem.
// Não-critical passa direto (no-op). Janela <= 0 cai no padrão de 60s.
func Alerta(janela time.Duration) Sink {
	if janela <= 0 {
		janela = janelaPadraoAlerta
	}
	return &alertaSink{janela: janela, ultimo: map[string]time.Time{}, suprimidos: map[string]int{}}
}

type alertaSink struct {
	mutex      sync.Mutex
	janela     time.Duration
	ultimo     map[string]time.Time // último [ALERTA] emitido por código
	suprimidos map[string]int       // eventos agregados desde o último alerta
	emitidos   uint64               // total de linhas [ALERTA] — observabilidade do próprio sink
}

func (a *alertaSink) Registrar(ev Evento) {
	if ev.Severidade != SeveridadeCritical {
		return
	}
	a.mutex.Lock()
	defer a.mutex.Unlock()
	agora := ev.Instante
	if agora.IsZero() {
		agora = time.Now().UTC()
	}
	if ultima, ok := a.ultimo[ev.Codigo]; ok && agora.Sub(ultima) < a.janela {
		a.suprimidos[ev.Codigo]++
		return
	}
	a.emitidos++
	a.ultimo[ev.Codigo] = agora
	args := []any{
		"codigo", ev.Codigo,
		"dominio", ev.Dominio, "subdominio", ev.Subdominio,
		"severidade", string(ev.Severidade),
	}
	if ev.Desconhecido || ev.Codigo == CodigoDesconhecido {
		args = append(args, "desconhecido", true)
	}
	if ev.RayTrace != "" {
		args = append(args, "ray_trace", ev.RayTrace)
	}
	if suprimidos := a.suprimidos[ev.Codigo]; suprimidos > 0 {
		args = append(args, "agregados_na_janela_anterior", suprimidos)
	}
	a.suprimidos[ev.Codigo] = 0
	slog.Error("[ALERTA]", args...)
}

// AlertasEmitidos expõe quantas linhas [ALERTA] saíram (teste/observação).
func (a *alertaSink) AlertasEmitidos() uint64 {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	return a.emitidos
}

// SuprimidosNaJanela devolve quantos eventos de um código foram agregados
// desde o último alerta dele (teste).
func (a *alertaSink) SuprimidosNaJanela(codigo string) int {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	return a.suprimidos[codigo]
}
