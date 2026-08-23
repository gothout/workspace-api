// Package access_log é a trilha de ACESSO HTTP: um evento por requisição,
// emitido pelo middleware global (primeiro da cadeia, em cmd/server/routes)
// com o ray_trace que correlaciona a linha com os erros padronizados do
// rest_err e com a auditoria.
//
// Pacote-FOLHA (regra 1 de agents/01): conhece só a forma do evento e a
// interface Destino. A entrega é ligada no cmd/bootstrap — writer assíncrono
// do ClickHouse (evolução #9) quando existe, SlogPadrao (stdout degradado)
// quando não. Registrar NUNCA bloqueia o request: enfileirar não pode custar
// mais do que responder.
package access_log

import (
	"log/slog"
	"time"
)

// Evento é uma linha da trilha de acesso. Rota é o PADRÃO casado pelo gin
// (/api/domain/identidade/organizations/{uuid}) — agrupável no ClickHouse;
// Path é o caminho cru da requisição. Os identificadores de tenancy (E5)
// são preenchidos pelo middleware DEPOIS da cadeia rodar — vazios quando a
// requisição não passou por ela (404, /doc) ou não resolveu workspace.
type Evento struct {
	Instante         time.Time
	Metodo           string
	Path             string
	Rota             string
	Status           int
	DuracaoMS        int64
	IP               string
	UserAgent        string
	RayTrace         string
	OrganizationUUID string
	WorkspaceUUID    string
	UserUUID         string
}

// Destino é quem consome a trilha (ClickHouse, stdout de teste). Implementação
// estrutural pelo infra; a ligação acontece no cmd/bootstrap.
type Destino interface {
	Registrar(Evento)
}

// SlogPadrao devolve o destino DEGRADADO (stdout): uma linha estruturada por
// requisição, mesmo formato do middleware legado. É o destino ligado pelo
// bootstrap quando o ClickHouse está ausente.
func SlogPadrao() Destino { return slogDestino{} }

type slogDestino struct{}

func (slogDestino) Registrar(ev Evento) {
	if ev.Instante.IsZero() {
		ev.Instante = time.Now().UTC()
	}
	args := []any{
		"instante", ev.Instante.Format(time.RFC3339Nano),
		"metodo", ev.Metodo, "path", ev.Path, "rota", ev.Rota,
		"status", ev.Status, "duracao_ms", ev.DuracaoMS,
		"ip", ev.IP, "user_agent", ev.UserAgent,
		"ray_trace", ev.RayTrace}
	if ev.OrganizationUUID != "" {
		args = append(args, "organization_uuid", ev.OrganizationUUID)
	}
	if ev.WorkspaceUUID != "" {
		args = append(args, "workspace_uuid", ev.WorkspaceUUID)
	}
	if ev.UserUUID != "" {
		args = append(args, "user_uuid", ev.UserUUID)
	}
	slog.Info("acesso", args...)
}
