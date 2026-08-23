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
// os pares extras montados à mão na escrita. user_nome/user_email (UX2) são
// o enriquecimento EM LOTE pela tabela de usuários — linha sem usuário
// (sistema/anônimo) sai com os campos vazios, nunca quebra.
type AuditoriaItemDto struct {
	Instante         time.Time         `json:"instante"`
	Dominio          string            `json:"dominio"`
	Subdominio       string            `json:"subdominio"`
	Acao             string            `json:"acao"`
	Sucesso          bool              `json:"sucesso"`
	OrganizationUUID string            `json:"organization_uuid"`
	WorkspaceUUID    string            `json:"workspace_uuid"`
	UserUUID         string            `json:"user_uuid"`
	UserNome         string            `json:"user_nome"`
	UserEmail        string            `json:"user_email"`
	RayTrace         string            `json:"ray_trace"`
	Detalhes         map[string]string `json:"detalhes"`
}

func novoAuditoriaItem(ev audit_log.Evento, usuarios map[string]UsuarioLog) AuditoriaItemDto {
	detalhes := ev.Detalhes
	if detalhes == nil {
		detalhes = map[string]string{}
	}
	return AuditoriaItemDto{
		Instante: ev.Instante, Dominio: ev.Dominio, Subdominio: ev.Subdominio,
		Acao: ev.Acao, Sucesso: ev.Sucesso,
		OrganizationUUID: ev.OrganizationUUID, WorkspaceUUID: ev.WorkspaceUUID,
		UserUUID: ev.UserUUID, RayTrace: ev.RayTrace, Detalhes: detalhes,
	}.enriquecer(usuarios)
}

// enriquecer preenche user_nome/user_email do mapa resolvido em lote —
// uuid ausente no mapa (sem usuário ou não resolvido) mantém os campos vazios.
func (d AuditoriaItemDto) enriquecer(usuarios map[string]UsuarioLog) AuditoriaItemDto {
	if u, ok := usuarios[d.UserUUID]; ok {
		d.UserNome, d.UserEmail = u.Nome, u.Email
	}
	return d
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
	UserNome         string    `json:"user_nome"`
	UserEmail        string    `json:"user_email"`
}

func novoAcessoItem(ev access_log.Evento, usuarios map[string]UsuarioLog) AcessoItemDto {
	item := AcessoItemDto{
		Instante: ev.Instante, Metodo: ev.Metodo, Path: ev.Path, Rota: ev.Rota,
		Status: ev.Status, DuracaoMS: ev.DuracaoMS, IP: ev.IP, UserAgent: ev.UserAgent,
		RayTrace:         ev.RayTrace,
		OrganizationUUID: ev.OrganizationUUID, WorkspaceUUID: ev.WorkspaceUUID,
		UserUUID: ev.UserUUID,
	}
	if u, ok := usuarios[item.UserUUID]; ok {
		item.UserNome, item.UserEmail = u.Nome, u.Email
	}
	return item
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
	UserNome         string    `json:"user_nome"`
	UserEmail        string    `json:"user_email"`
	RayTrace         string    `json:"ray_trace"`
}

func novoErroItem(ev errobserve.Evento, usuarios map[string]UsuarioLog) ErroItemDto {
	item := ErroItemDto{
		Instante: ev.Instante, Dominio: ev.Dominio, Subdominio: ev.Subdominio,
		Codigo: ev.Codigo, Mensagem: ev.Mensagem, Severidade: string(ev.Severidade),
		Desconhecido:     ev.Desconhecido,
		OrganizationUUID: ev.OrganizationUUID, WorkspaceUUID: ev.WorkspaceUUID,
		UserUUID: ev.UserUUID, RayTrace: ev.RayTrace,
	}
	if u, ok := usuarios[item.UserUUID]; ok {
		item.UserNome, item.UserEmail = u.Nome, u.Email
	}
	return item
}

func NovoAuditoriaResponseDto(itens []audit_log.Evento, total int64, p pagination.Pagination, usuarios map[string]UsuarioLog) pagination.Response[AuditoriaItemDto] {
	if usuarios == nil {
		usuarios = map[string]UsuarioLog{}
	}
	dtos := make([]AuditoriaItemDto, 0, len(itens))
	for _, ev := range itens {
		dtos = append(dtos, novoAuditoriaItem(ev, usuarios))
	}
	return pagination.NovaResponse(dtos, total, p)
}

func NovoAcessoResponseDto(itens []access_log.Evento, total int64, p pagination.Pagination, usuarios map[string]UsuarioLog) pagination.Response[AcessoItemDto] {
	if usuarios == nil {
		usuarios = map[string]UsuarioLog{}
	}
	dtos := make([]AcessoItemDto, 0, len(itens))
	for _, ev := range itens {
		dtos = append(dtos, novoAcessoItem(ev, usuarios))
	}
	return pagination.NovaResponse(dtos, total, p)
}

func NovoErrosResponseDto(itens []errobserve.Evento, total int64, p pagination.Pagination, usuarios map[string]UsuarioLog) pagination.Response[ErroItemDto] {
	if usuarios == nil {
		usuarios = map[string]UsuarioLog{}
	}
	dtos := make([]ErroItemDto, 0, len(itens))
	for _, ev := range itens {
		dtos = append(dtos, novoErroItem(ev, usuarios))
	}
	return pagination.NovaResponse(dtos, total, p)
}

// OpcaoDto — UMA opção de Select do painel de filtros (UX3): uuid para
// preencher o filtro, nome para exibir.
type OpcaoDto struct {
	UUID string `json:"uuid"`
	Nome string `json:"nome"`
}

// OpcoesFiltroResponseDto — opções de filtro JÁ recortadas pelo escopo do
// chamador: plataforma lista tudo; organization, o próprio recorte;
// workspace, os usuários atribuídos ao próprio workspace. Listas vazias saem
// como [] (nunca null).
type OpcoesFiltroResponseDto struct {
	Organizacoes []OpcaoDto `json:"organizacoes"`
	Workspaces   []OpcaoDto `json:"workspaces"`
	Usuarios     []OpcaoDto `json:"usuarios"`
}

func novasOpcoes(itens []OpcaoFiltro) []OpcaoDto {
	dtos := make([]OpcaoDto, 0, len(itens))
	for _, item := range itens {
		dtos = append(dtos, OpcaoDto{UUID: item.UUID, Nome: item.Nome})
	}
	return dtos
}

func NovoOpcoesFiltroResponseDto(organizacoes, workspaces, usuarios []OpcaoFiltro) OpcoesFiltroResponseDto {
	return OpcoesFiltroResponseDto{
		Organizacoes: novasOpcoes(organizacoes),
		Workspaces:   novasOpcoes(workspaces),
		Usuarios:     novasOpcoes(usuarios),
	}
}
