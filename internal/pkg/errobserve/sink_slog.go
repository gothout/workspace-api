package errobserve

import (
	"log/slog"
)

// SlogPadrao é o destino SEMPRE ativo (stdout): uma linha estruturada por
// evento de erro, campos em ordem fixa. Nível pela severidade — warn desce
// como WARN; error e critical sobem como ERROR. É também o destino
// DEGRADADO: com o ClickHouse fora, é ele quem fica (o log [DEGRADADO]
// correspondente sai do Connect do infra, não daqui).
func SlogPadrao() Sink { return slogSink{} }

type slogSink struct{}

func (slogSink) Registrar(ev Evento) {
	args := []any{
		"instante", ev.Instante.Format("2006-01-02T15:04:05.000Z07:00"),
		"dominio", ev.Dominio, "subdominio", ev.Subdominio,
		"codigo", ev.Codigo, "severidade", string(ev.Severidade),
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
	if ev.RayTrace != "" {
		args = append(args, "ray_trace", ev.RayTrace)
	}
	if ev.Mensagem != "" {
		args = append(args, "mensagem", ev.Mensagem)
	}
	if ev.Causa != "" {
		args = append(args, "causa", ev.Causa)
	}
	registrador := slog.Error
	if ev.Severidade == SeveridadeWarn && !ev.Desconhecido {
		registrador = slog.Warn
	}
	registrador(prefixoSlog(ev), args...)
}

// prefixoSlog monta a chave da linha: o código estável JÁ carrega
// dominio/subdominio (identidade.workspace.slug_em_uso) — a linha ganha só o
// prefixo "erro."; desconhecidos agrupam em erro.desconhecido.
func prefixoSlog(ev Evento) string {
	if ev.Desconhecido || ev.Codigo == CodigoDesconhecido {
		return "erro.desconhecido"
	}
	return "erro." + ev.Codigo
}
