package logs

import (
	"time"

	"workspace-api/internal/pkg/errobserve"
	"workspace-api/internal/pkg/log/access_log"
	"workspace-api/internal/pkg/log/audit_log"
	"workspace-api/internal/pkg/pagination"
)

// AuditoriaItemDto — uma linha da trilha de auditoria. Campos alinhados ao
// catálogo de eventos (E3): acao é a chave estável do events.go; detalhes são
// os pares extras montados à mão na escrita.
type AuditoriaItemDto struct {
	Instante         time.Time         `json:"instante"`
	Dominio          string            `json:"dominio"`
	Subdominio       string            `json:"subdominio"`
	Acao             string            `json:"acao"`
	Sucesso          bool              `json:"sucesso"`
	OrganizationUUID string            `json:"organization_uuid"`
	WorkspaceUUID    string            `json:"workspace_uuid"`
	UserUUID         string            `json:"user_uuid"`
	RayTrace         string            `json:"ray_trace"`
	Detalhes         map[string]string `json:"detalhes"`
}

func novoAuditoriaItem(ev audit_log.Evento) AuditoriaItemDto {
	detalhes := ev.Detalhes
	if detalhes == nil {
		detalhes = map[string]string{}
	}
	return AuditoriaItemDto{
		Instante: ev.Instante, Dominio: ev.Dominio, Subdominio: ev.Subdominio,
		Acao: ev.Acao, Sucesso: ev.Sucesso,
		OrganizationUUID: ev.OrganizationUUID, WorkspaceUUID: ev.WorkspaceUUID,
		UserUUID: ev.UserUUID, RayTrace: ev.RayTrace, Detalhes: detalhes,
	}
}

// AcessoItemDto — uma linha da trilha de acesso HTTP. Rota é o padrão do gin
// (agrupável); Path o caminho cru.
type AcessoItemDto struct {
	Instante         time.Time `json:"instante"`
	Metodo           string    `json:"metodo"`
	Path             string    `json:"path"`
	Rota             string    `json:"rota"`
	Status           int       `json:"status"`
	DuracaoMS        int64     `json:"duracao_ms"`
	IP               string    `json:"ip"`
	UserAgent        string    `json:"user_agent"`
	RayTrace         string    `json:"ray_trace"`
	OrganizationUUID string    `json:"organization_uuid"`
	WorkspaceUUID    string    `json:"workspace_uuid"`
	UserUUID         string    `json:"user_uuid"`
}

func novoAcessoItem(ev access_log.Evento) AcessoItemDto {
	return AcessoItemDto{
		Instante: ev.Instante, Metodo: ev.Metodo, Path: ev.Path, Rota: ev.Rota,
		Status: ev.Status, DuracaoMS: ev.DuracaoMS, IP: ev.IP, UserAgent: ev.UserAgent,
		RayTrace: ev.RayTrace,
		OrganizationUUID: ev.OrganizationUUID, WorkspaceUUID: ev.WorkspaceUUID,
		UserUUID: ev.UserUUID,
	}
}

// ErroItemDto — uma linha da trilha de erros observados (errobserve): código
// estável do catálogo + severidade declarada no singleton. Causa NÃO sai
// aqui — texto interno de log, nunca conteúdo de API.
type ErroItemDto struct {
	Instante         time.Time `json:"instante"`
	Dominio          string    `json:"dominio"`
	Subdominio       string    `json:"subdominio"`
	Codigo           string    `json:"codigo"`
	Mensagem         string    `json:"mensagem"`
	Severidade       string    `json:"severidade"`
	Desconhecido     bool      `json:"desconhecido"`
	OrganizationUUID string    `json:"organization_uuid"`
	WorkspaceUUID    string    `json:"workspace_uuid"`
	UserUUID         string    `json:"user_uuid"`
	RayTrace         string    `json:"ray_trace"`
}

func novoErroItem(ev errobserve.Evento) ErroItemDto {
	return ErroItemDto{
		Instante: ev.Instante, Dominio: ev.Dominio, Subdominio: ev.Subdominio,
		Codigo: ev.Codigo, Mensagem: ev.Mensagem, Severidade: string(ev.Severidade),
		Desconhecido:     ev.Desconhecido,
		OrganizationUUID: ev.OrganizationUUID, WorkspaceUUID: ev.WorkspaceUUID,
		UserUUID: ev.UserUUID, RayTrace: ev.RayTrace,
	}
}

func NovoAuditoriaResponseDto(itens []audit_log.Evento, total int64, p pagination.Pagination) pagination.Response[AuditoriaItemDto] {
	dtos := make([]AuditoriaItemDto, 0, len(itens))
	for _, ev := range itens {
		dtos = append(dtos, novoAuditoriaItem(ev))
	}
	return pagination.NovaResponse(dtos, total, p)
}

func NovoAcessoResponseDto(itens []access_log.Evento, total int64, p pagination.Pagination) pagination.Response[AcessoItemDto] {
	dtos := make([]AcessoItemDto, 0, len(itens))
	for _, ev := range itens {
		dtos = append(dtos, novoAcessoItem(ev))
	}
	return pagination.NovaResponse(dtos, total, p)
}

func NovoErrosResponseDto(itens []errobserve.Evento, total int64, p pagination.Pagination) pagination.Response[ErroItemDto] {
	dtos := make([]ErroItemDto, 0, len(itens))
	for _, ev := range itens {
		dtos = append(dtos, novoErroItem(ev))
	}
	return pagination.NovaResponse(dtos, total, p)
}
