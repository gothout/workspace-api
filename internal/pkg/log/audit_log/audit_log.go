// Package audit_log é a trilha de AUDITORIA das escritas do sistema (doc 04):
// um evento por escrita dos subdomínios, com o payload montado à mão que já
// era exigido do slog — identificadores e vocabulário fechado, nunca texto
// livre e nunca segredo.
//
// Pacote-FOLHA (regra 1 de agents/01): conhece só a forma do evento e a
// interface Destino. Quem entrega os eventos em algum lugar é o bootstrap:
// o writer assíncrono do ClickHouse (evolução #9) quando existe, ou o
// SlogPadrao (stdout degradado) quando não. Registrar NUNCA bloqueia nem
// erra — telemetria não pode mudar o resultado do negócio.
package audit_log

import (
	"fmt"
	"log/slog"
	"sort"
	"time"
)

// Evento é uma linha da trilha de auditoria. Os campos espelham exatamente
// o payload montado à mão nos auditar() dos subdomínios — nada foi perdido
// na migração para o destino assíncrono.
type Evento struct {
	Instante         time.Time
	Dominio          string
	Subdominio       string
	Acao             string
	Sucesso          bool
	OrganizationUUID string // "000...0" quando ausente no ctx (mesmo comportamento do slog legado)
	WorkspaceUUID    string
	UserUUID         string
	RayTrace         string
	// Detalhes são os pares extras montados à mão (k→v normalizado em
	// texto). Nil é válido — nem todo evento carrega detalhe.
	Detalhes map[string]string
}

// Destino é a cara de quem consome a trilha (ClickHouse, stdout de teste).
// Implementação estrutural: o infra implementa sem importar nada daqui além
// do tipo do evento; a ligação acontece no cmd/bootstrap.
type Destino interface {
	Registrar(Evento)
}

// Detalhes normaliza pares chave/valor avulsos ("chave", valor, ...) no mapa
// do Evento — mesma convenção k/v do slog usada nos payloads antigos. Chave
// sem par vira valor vazio (honesto: presente no payload, sem conteúdo).
func Detalhes(pares ...any) map[string]string {
	if len(pares) == 0 {
		return nil
	}
	detalhes := make(map[string]string, len(pares)/2)
	for i := 0; i < len(pares); i += 2 {
		chave := fmt.Sprint(pares[i])
		valor := ""
		if i+1 < len(pares) {
			valor = fmt.Sprint(pares[i+1])
		}
		detalhes[chave] = valor
	}
	return detalhes
}

// SlogPadrao devolve o destino DEGRADADO (stdout): uma linha estruturada por
// evento, na ordem fixa do payload legado. Falha = Warn, sucesso = Info.
// É o destino ligado pelo bootstrap quando o ClickHouse está ausente — o log
// [DEGRADADO] correspondente sai do Connect do infra, não daqui.
func SlogPadrao() Destino { return slogDestino{} }

type slogDestino struct{}

func (slogDestino) Registrar(ev Evento) {
	if ev.Instante.IsZero() {
		ev.Instante = time.Now().UTC()
	}
	args := []any{
		"instante", ev.Instante.Format(time.RFC3339Nano),
		"dominio", ev.Dominio, "subdominio", ev.Subdominio, "acao", ev.Acao,
	}
	if ev.OrganizationUUID != "" {
		args = append(args, "organization_uuid", ev.OrganizationUUID)
	}
	if ev.WorkspaceUUID != "" {
		args = append(args, "workspace_uuid", ev.WorkspaceUUID)
	}
	if ev.UserUUID != "" {
		args = append(args, "user_uuid", ev.UserUUID)
	}
	args = append(args, "ray_trace", ev.RayTrace, "success", ev.Sucesso)
	for _, chave := range chavesOrdenadas(ev.Detalhes) {
		args = append(args, chave, ev.Detalhes[chave])
	}
	registrador := slog.Info
	if !ev.Sucesso {
		registrador = slog.Warn // falha de auditoria é evento de segurança
	}
	registrador("auditoria."+ev.Dominio+"."+ev.Acao, args...)
}

// chavesOrdenadas garante saída determinística do destino stdout — mapa não
// tem ordem e log de auditoria precisa ser comparável linha a linha.
func chavesOrdenadas(detalhes map[string]string) []string {
	chaves := make([]string, 0, len(detalhes))
	for chave := range detalhes {
		chaves = append(chaves, chave)
	}
	sort.Strings(chaves)
	return chaves
}
