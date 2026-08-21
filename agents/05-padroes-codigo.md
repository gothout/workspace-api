# 05 — Padrões de Código (singleton, templates, testes)

Todos os templates usam o subdomínio `workspace` como exemplo. Copiar e adaptar — o formato é **canônico**, não uma sugestão. Módulo Go: `workspace-api`.

## Padrão singleton por subdomínio (obrigatório)

Cada subdomínio expõe `New(deps...)`, `Use()`, `MustUse()` em `singleton.go`. O mesmo par **função pura + singleton do processo** vale para todo pacote de `infra` (`Connect` puro + `InitX`/`Get...`/`Close` com `sync.Once`).

```go
package workspace

import (
    "errors"
    "sync"

    "gorm.io/gorm"
)

var (
    controllerInstance Controller
    serviceInstance    Service
    repositoryInstance Repository
    once               sync.Once
    initErr            error
    ErrNotInitialized  = errors.New("controller workspace não inicializado")
)

// UseWorkspace agrupa todas as camadas (Repository, Service, Controller).
type UseWorkspace struct {
    Repository Repository
    Service    Service
    Controller Controller
}

// New inicializa o singleton do subdomínio montando repo → service → controller.
// Chamado UMA vez pelo cmd/bootstrap; chamadas seguintes devolvem a mesma instância.
func New(db *gorm.DB) (Controller, error) {
    once.Do(func() {
        if db == nil {
            initErr = errors.New("conexão com o banco não pode ser nula")
            return
        }
        repositoryInstance = NewRepository(db)
        serviceInstance = NewService(repositoryInstance)
        controllerInstance = NewController(serviceInstance)
    })
    return controllerInstance, initErr
}

// Use devolve o controller singleton; erro se não inicializado.
// Usado pelo registro de rotas: sem boot, a rota não é registrada e o motivo sai no log.
func Use() (Controller, error) {
    if controllerInstance == nil {
        return nil, ErrNotInitialized
    }
    return controllerInstance, nil
}

// MustUse devolve todas as camadas; entra em pânico se não inicializado.
// Restrito ao cmd/bootstrap — panic fora do boot é proibido.
func MustUse() *UseWorkspace {
    if controllerInstance == nil || serviceInstance == nil || repositoryInstance == nil {
        panic(ErrNotInitialized)
    }
    return &UseWorkspace{Repository: repositoryInstance, Service: serviceInstance, Controller: controllerInstance}
}
```

Variação permitida: subdomínio que depende de um vizinho recebe a **interface do contrato** como parâmetro extra do `New` (ligada no bootstrap) — nunca o pacote do vizinho.

O singleton **não é seguro para inicialização concorrente nem re-inicialização** — a garantia é a ordem sequencial do `cmd/bootstrap` (um `New` por processo, na ordem de dependência). Testes de service **nunca** passam por `New`: montam `NewService(repoFake)` direto, sem singleton.

## Templates de arquivo

### model.go

```go
// Package workspace implementa o subdomínio workspace do domínio identidade:
// unidade de trabalho de uma organization, endereçada por slug DNS único global.
// Entidades: Workspace. Dependências: pkg (orgctx, rest_err, pagination) + libs
// (gorm/pgx); a conexão *gorm.DB é injetada pelo bootstrap (infra/postgres) —
// o subdomínio não importa o pacote infra.
package workspace

import (
    "regexp"
    "strings"
    "time"

    "github.com/google/uuid"
    "gorm.io/gorm"

    "workspace-api/internal/pkg/pagination"
)

const (
    // Dominio e Subdominio identificam este pacote nos catálogos (erros, permissões) e logs.
    Dominio    = "identidade"
    Subdominio = "workspace"
)

// StatusWorkspace — ciclo de vida do workspace.
type StatusWorkspace string

const (
    StatusAtivo   StatusWorkspace = "ativo"
    StatusInativo StatusWorkspace = "inativo"
)

// Valido confere se o valor está no conjunto fechado — todo tipo nomeado tem um.
func (s StatusWorkspace) Valido() bool {
    switch s {
    case StatusAtivo, StatusInativo:
        return true
    }
    return false
}

// Slug é um VALUE OBJECT: imutável, definido pelo valor, válido desde o nascimento.
// VO em Go = tipo nomeado + construtor que valida + métodos de comportamento —
// nunca uma string solta viajando pelos inputs.
type Slug string

var slugRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{1,61}[a-z0-9])$`)

// ParseSlug valida o formato DNS e devolve o VO; inválido = ErrSlugInvalido.
// (A tag `slugdns` do DTO é a primeira linha de defesa; aqui é a garantia de domínio.)
func ParseSlug(s string) (Slug, error) {
    if !slugRe.MatchString(s) {
        return "", ErrSlugInvalido
    }
    return Slug(s), nil
}

func (s Slug) String() string { return string(s) }

// Workspace é a ENTIDADE raiz do agregado do subdomínio: tem identidade (uuid)
// e ciclo de vida. Campos exportados são concessão ao GORM — MUTAÇÃO DIRETA
// fora dos métodos de comportamento é proibida: toda transição passa por eles.
type Workspace struct {
    UUID             uuid.UUID       `gorm:"column:uuid;type:uuid;primaryKey" json:"uuid"`
    OrganizationUUID uuid.UUID       `gorm:"column:organization_uuid;type:uuid;not null;index:idx_workspace_scope,priority:1" json:"organization_uuid"`
    Nome             string          `gorm:"column:nome;not null" json:"nome"`
    Slug             Slug            `gorm:"column:slug;type:text;not null" json:"slug"`
    Status           StatusWorkspace `gorm:"column:status;not null;default:'ativo'" json:"status"`
    CreatedAt        time.Time       `gorm:"column:created_at;autoCreateTime" json:"created_at"`
    UpdatedAt        time.Time       `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
    DeletedAt        gorm.DeletedAt  `gorm:"column:deleted_at;index" json:"-"`
}

func (Workspace) TableName() string { return "identidade_workspace_workspace" }

// NewWorkspace é o CONSTRUTOR do agregado: valida as invariantes antes de
// devolver a entidade. Entidade ≠ DTO ≠ input: o input carrega dado cru, o
// construtor devolve entidade VÁLIDA (ou a sentinela do invariante violado).
// Controller e service nunca montam entidade campo a campo.
func NewWorkspace(in CreateInput) (*Workspace, error) {
    slug, err := ParseSlug(in.Slug)
    if err != nil {
        return nil, err
    }
    nome := strings.TrimSpace(in.Nome)
    if len(nome) < 2 {
        return nil, ErrInvalidInput
    }
    return &Workspace{
        UUID: uuid.New(), OrganizationUUID: in.OrganizationUUID,
        Nome: nome, Slug: slug, Status: StatusAtivo,
    }, nil
}

// Inativar — transição de estado com invariante: workspace inativo não inativa de novo.
func (w *Workspace) Inativar() error {
    if w.Status == StatusInativo {
        return ErrJaInativo
    }
    w.Status = StatusInativo
    return nil
}

// Renomear — comportamento com invariante de tamanho; o service chama este
// método em vez de atribuir w.Nome direto.
func (w *Workspace) Renomear(nome string) error {
    nome = strings.TrimSpace(nome)
    if len(nome) < 2 {
        return ErrInvalidInput
    }
    w.Nome = nome
    return nil
}

// CreateInput e UpdateInput carregam só o que a regra permite escrever.
// OrganizationUUID é preenchido pelo SERVICE a partir do ctx, NUNCA do corpo.
// O service traduz UpdateInput em chamadas aos métodos de comportamento —
// nunca atribui w.Status direto.
type CreateInput struct {
    OrganizationUUID uuid.UUID
    Nome             string
    Slug             string
}

type UpdateInput struct {
    Nome   *string
    Status *StatusWorkspace
}

// ListFilter — filtros de listagem; o escopo vem do ctx, nunca do filtro.
type ListFilter struct {
    Nome   string
    Status *StatusWorkspace
    pagination.Pagination
}
```

### dto_request.go

```go
package workspace

// CreateWorkspaceRequestDto — entrada de POST /api/domain/identidade/workspaces.
type CreateWorkspaceRequestDto struct {
    Nome string `json:"nome" binding:"required,min=2,max=120"`
    Slug string `json:"slug" binding:"required,min=3,max=63,slugdns"` // slugdns: tag do pkg/validator
}

// ParaEntrada converte o DTO no input do service — controller nunca monta entidade.
func (d CreateWorkspaceRequestDto) ParaEntrada() CreateInput {
    return CreateInput{Nome: d.Nome, Slug: d.Slug}
}

// UpdateWorkspaceRequestDto — entrada de PATCH; ponteiros distinguem "ausente" de "vazio".
type UpdateWorkspaceRequestDto struct {
    Nome   *string `json:"nome" binding:"omitempty,min=2,max=120"`
    Status *string `json:"status" binding:"omitempty,oneof=ativo inativo"`
}

// ParaEntrada valida e converte para UpdateInput; status fora do conjunto = ErrInvalidInput.
func (d UpdateWorkspaceRequestDto) ParaEntrada() (UpdateInput, error) {
    in := UpdateInput{Nome: d.Nome}
    if d.Status != nil {
        s := StatusWorkspace(*d.Status)
        if !s.Valido() {
            return UpdateInput{}, ErrInvalidInput
        }
        in.Status = &s
    }
    return in, nil
}
```

### dto_response.go

```go
package workspace

import (
    "time"

    "github.com/google/uuid"

    "workspace-api/internal/pkg/pagination"
)

// WorkspaceResponseDto — saída única do subdomínio; nunca expõe hash nem campos internos.
type WorkspaceResponseDto struct {
    UUID             uuid.UUID `json:"uuid"`
    OrganizationUUID uuid.UUID `json:"organization_uuid"`
    Nome             string    `json:"nome"`
    Slug             string    `json:"slug"`
    Status           string    `json:"status"`
    CreatedAt        time.Time `json:"created_at"`
    UpdatedAt        time.Time `json:"updated_at"`
}

func NovoWorkspaceResponseDto(w *Workspace) WorkspaceResponseDto {
    return WorkspaceResponseDto{
        UUID: w.UUID, OrganizationUUID: w.OrganizationUUID, Nome: w.Nome,
        Slug: w.Slug.String(), Status: string(w.Status),
        CreatedAt: w.CreatedAt, UpdatedAt: w.UpdatedAt,
    }
}

func NovoWorkspaceListaResponseDto(items []Workspace, total int64, p pagination.Pagination) pagination.Response[WorkspaceResponseDto] {
    dtos := make([]WorkspaceResponseDto, 0, len(items))
    for i := range items {
        dtos = append(dtos, NovoWorkspaceResponseDto(&items[i]))
    }
    return pagination.NovaResponse(dtos, total, p)
}
```

### errors.go

```go
package workspace

import (
    "errors"
    "net/http"

    "workspace-api/internal/pkg/rest_err"
)

// Sentinelas do subdomínio — comparadas com errors.Is, nunca por texto.
// Regra de mapeamento sentinela → status: entrada inválida = 400; não encontrado = 404;
// conflito de UNICIDADE = 409; INVARIANTE de domínio violada = 422; desconhecido = 500.
var (
    ErrNotFound      = errors.New("workspace não encontrado")
    ErrInvalidInput  = errors.New("dados de entrada inválidos")
    ErrSlugEmUso     = errors.New("slug já está em uso por outro workspace")
    ErrSlugReservado = errors.New("slug reservado pela plataforma")
    ErrSlugInvalido  = errors.New("slug fora do formato DNS")
    ErrJaInativo     = errors.New("workspace já está inativo")
)

// errorCatalog é o contrato público de cada sentinela: código estável, mensagem PT-BR, status.
// Registrado no mapa global do rest_err pelo init() — é o que alimenta GET /api/system/errors.
// Sentinela nova SEM entrada aqui não fecha o checklist do subdomínio.
var errorCatalog = map[error]rest_err.ErroCatalogado{
    ErrNotFound:      {Codigo: "identidade.workspace.nao_encontrado", Mensagem: "Workspace não encontrado.", Status: http.StatusNotFound},
    ErrInvalidInput:  {Codigo: "identidade.workspace.entrada_invalida", Mensagem: "Dados de entrada inválidos.", Status: http.StatusBadRequest},
    ErrSlugEmUso:     {Codigo: "identidade.workspace.slug_em_uso", Mensagem: "Slug já está em uso por outro workspace.", Status: http.StatusConflict},
    ErrSlugReservado: {Codigo: "identidade.workspace.slug_reservado", Mensagem: "Slug reservado pela plataforma.", Status: http.StatusUnprocessableEntity},
    ErrSlugInvalido:  {Codigo: "identidade.workspace.slug_invalido", Mensagem: "Slug fora do formato DNS.", Status: http.StatusBadRequest},
    ErrJaInativo:     {Codigo: "identidade.workspace.ja_inativo", Mensagem: "Workspace já está inativo.", Status: http.StatusUnprocessableEntity},
}

func init() {
    rest_err.RegistrarCatalogo(Dominio, Subdominio, errorCatalog)
}
```

### permissions.go

```go
package workspace

import "net/http"

// Permissões granulares do subdomínio — string dominio:subdominio:acao.
const (
    PermCriar   = "identidade:workspace:criar"
    PermLer     = "identidade:workspace:ler"
    PermEditar  = "identidade:workspace:editar"
    PermRemover = "identidade:workspace:remover"
)

// PermissaoMeta — metadados da permissão para o catálogo consultável (doc 03).
// Mesma forma em todos os subdomínios; o bootstrap agrega os Catalogo() no registro
// único do identidade/application/catalogo (domain não importa application — regra 3).
type PermissaoMeta struct {
    Permissao string     // valor exato exigido pela rota
    Descricao string     // PT-BR: o que a permissão libera
    Rotas     []RotaMeta // pares rota+método que exigem esta permissão
    GrupoMenu string     // agrupamento sugerido para o menu do front-end
}

// RotaMeta — UM par rota+método; a árvore do endpoint emite uma ação por par.
type RotaMeta struct {
    Rota   string // path com {uuid} onde couber
    Metodo string // método HTTP
}

// Catalogo devolve TODAS as permissões do subdomínio com metadados.
// Sem ele o subdomínio não aparece no endpoint de permissões e o checklist não fecha.
func Catalogo() []PermissaoMeta {
    return []PermissaoMeta{
        {Permissao: PermCriar, Descricao: "Criar workspace na organization", Rotas: []RotaMeta{{Rota: "/api/domain/identidade/workspaces", Metodo: http.MethodPost}}, GrupoMenu: "Identidade · Workspaces"},
        {Permissao: PermLer, Descricao: "Listar e consultar workspaces da organization", Rotas: []RotaMeta{{Rota: "/api/domain/identidade/workspaces", Metodo: http.MethodGet}, {Rota: "/api/domain/identidade/workspaces/{uuid}", Metodo: http.MethodGet}}, GrupoMenu: "Identidade · Workspaces"},
        {Permissao: PermEditar, Descricao: "Editar dados do workspace", Rotas: []RotaMeta{{Rota: "/api/domain/identidade/workspaces/{uuid}", Metodo: http.MethodPatch}}, GrupoMenu: "Identidade · Workspaces"},
        {Permissao: PermRemover, Descricao: "Remover workspace", Rotas: []RotaMeta{{Rota: "/api/domain/identidade/workspaces/{uuid}", Metodo: http.MethodDelete}}, GrupoMenu: "Identidade · Workspaces"},
    }
}
```

### repository.go

```go
package workspace

import (
    "context"
    "errors"

    "github.com/google/uuid"
    "github.com/jackc/pgx/v5/pgconn"
    "gorm.io/gorm"

    "workspace-api/internal/pkg/orgctx"
)

// Repository é UM POR AGREGADO: declara, em linguagem de negócio, o que o
// subdomínio precisa perguntar/gravar — nunca um "CRUD genérico". Finders
// carregam o vocabulário do negócio (FindBySlug, SlugOcupado); Create/Update/
// Delete são as primitivas de persistência da raiz.
type Repository interface {
    Create(ctx context.Context, w *Workspace) error
    FindByUUID(ctx context.Context, id uuid.UUID) (*Workspace, error)
    FindBySlug(ctx context.Context, slug Slug) (*Workspace, error)
    List(ctx context.Context, f ListFilter) ([]Workspace, int64, error)
    Update(ctx context.Context, w *Workspace) error
    Delete(ctx context.Context, id uuid.UUID) error
}

type repositoryImpl struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) Repository { return &repositoryImpl{db: db} }

// Toda query de administração passa por ScopeOrganization — esta tabela está ACIMA
// do workspace que ela define; sem organization no ctx, o escopo FALHA (fail-closed).
func (r *repositoryImpl) FindByUUID(ctx context.Context, id uuid.UUID) (*Workspace, error) {
    var w Workspace
    err := orgctx.ScopeOrganization(r.db.WithContext(ctx), ctx).
        Where("uuid = ?", id).First(&w).Error
    if errors.Is(err, gorm.ErrRecordNotFound) {
        return nil, ErrNotFound
    }
    if err != nil {
        return nil, err
    }
    return &w, nil
}

// FindBySlug é a EXCEÇÃO de escopo documentada: query GLOBAL usada na resolução
// pelo Host (antes de existir escopo). Resultado nunca exposto em rota de administração.
func (r *repositoryImpl) FindBySlug(ctx context.Context, slug Slug) (*Workspace, error) {
    var w Workspace
    err := r.db.WithContext(ctx).Where("slug = ?", slug).First(&w).Error
    if errors.Is(err, gorm.ErrRecordNotFound) {
        return nil, ErrNotFound
    }
    if err != nil {
        return nil, err
    }
    return &w, nil
}

func (r *repositoryImpl) List(ctx context.Context, f ListFilter) ([]Workspace, int64, error) {
    var items []Workspace
    var total int64
    q := orgctx.ScopeOrganization(r.db.WithContext(ctx), ctx).Model(&Workspace{})
    if f.Nome != "" {
        q = q.Where("nome ILIKE ?", "%"+f.Nome+"%")
    }
    if f.Status != nil {
        q = q.Where("status = ?", *f.Status)
    }
    if err := q.Count(&total).Error; err != nil {
        return nil, 0, err
    }
    err := q.Order("created_at DESC").
        Offset(f.Pagination.Offset()).Limit(f.Pagination.Limit()). // helpers do pkg/pagination (teto 100)
        Find(&items).Error
    return items, total, err
}

func (r *repositoryImpl) Create(ctx context.Context, w *Workspace) error {
    return traduzirErroDriver(orgctx.ScopeOrganization(r.db.WithContext(ctx), ctx).Create(w).Error)
}

func (r *repositoryImpl) Update(ctx context.Context, w *Workspace) error {
    return orgctx.ScopeOrganization(r.db.WithContext(ctx), ctx).Save(w).Error
}

// Update e Delete conferem RowsAffected: 0 linhas = registro inexistente ou fora do escopo.
func (r *repositoryImpl) Delete(ctx context.Context, id uuid.UUID) error {
    res := orgctx.ScopeOrganization(r.db.WithContext(ctx), ctx).
        Where("uuid = ?", id).Delete(&Workspace{})
    if res.Error != nil {
        return res.Error
    }
    if res.RowsAffected == 0 {
        return ErrNotFound
    }
    return nil
}

// traduzirErroDriver: conflito de unicidade do Postgres (SQLSTATE 23505) vira a sentinela 409.
func traduzirErroDriver(err error) error {
    var pgErr *pgconn.PgError
    if errors.As(err, &pgErr) && pgErr.Code == "23505" {
        return ErrSlugEmUso
    }
    return err
}
```

### service.go

```go
package workspace

import (
    "context"
    "log/slog"

    "github.com/google/uuid"

    "workspace-api/internal/pkg/orgctx"
)

type Service interface {
    Create(ctx context.Context, in CreateInput) (*Workspace, error)
    Read(ctx context.Context, id uuid.UUID) (*Workspace, error)
    List(ctx context.Context, f ListFilter) ([]Workspace, int64, error)
    Update(ctx context.Context, id uuid.UUID, in UpdateInput) (*Workspace, error)
    Delete(ctx context.Context, id uuid.UUID) error
}

type serviceImpl struct{ repo Repository }

func NewService(repo Repository) Service { return &serviceImpl{repo: repo} }

// slugsReservados — endereços fixos da plataforma; a lista mora no subdomínio
// (o middleware pergunta a ele, nunca o contrário).
var slugsReservados = map[Slug]bool{
    "www": true, "api": true, "app": true, "admin": true, "docs": true,
    "status": true, "mail": true, "suporte": true, "painel": true,
}

// Create: input cru → escopo do ctx → entidade VÁLIDA pelo construtor → regras → persistência.
func (s *serviceImpl) Create(ctx context.Context, in CreateInput) (*Workspace, error) {
    in.OrganizationUUID = orgctx.OrganizationUUID(ctx) // escopo vem do ctx, NUNCA do corpo
    w, err := NewWorkspace(in) // invariantes validadas aqui — nunca struct literal à mão
    if err != nil {
        return nil, err
    }
    if slugsReservados[w.Slug] {
        return nil, ErrSlugReservado
    }
    // unicidade amigável: checagem prévia além do 23505 traduzido pelo repository
    if err := s.repo.Create(ctx, w); err != nil {
        return nil, err
    }
    // Toda escrita audita (doc 04): payload fechado, montado à mão — nunca texto livre.
    slog.InfoContext(ctx, "workspace.criar",
        "dominio", Dominio, "subdominio", Subdominio, "acao", "criar",
        "workspace_uuid", w.UUID.String(), "organization_uuid", w.OrganizationUUID.String(),
        "user_uuid", orgctx.UserUUID(ctx).String(), "ray_trace", orgctx.RayTrace(ctx),
        "success", true)
    return w, nil
}

// Update traduz o input em chamadas aos métodos de comportamento — nunca atribui campo direto.
func (s *serviceImpl) Update(ctx context.Context, id uuid.UUID, in UpdateInput) (*Workspace, error) {
    w, err := s.repo.FindByUUID(ctx, id)
    if err != nil {
        return nil, err
    }
    if in.Nome != nil {
        if err := w.Renomear(*in.Nome); err != nil {
            return nil, err
        }
    }
    if in.Status != nil && *in.Status == StatusInativo {
        if err := w.Inativar(); err != nil {
            return nil, err
        }
    }
    // reativação é ação de negócio própria (POST .../acoes/reativar), não PATCH de campo
    if err := s.repo.Update(ctx, w); err != nil {
        return nil, err
    }
    return w, nil
}
```

O `Service` do subdomínio é o **domain service** do agregado: TODA a regra de negócio que não cabe num método da entidade vive aqui — lista de reservados, unicidade amigável, cascatas. Formato de slug NÃO mora aqui: é invariante do VO (`ParseSlug`). O controller nunca tem regra. **Referência a outro agregado só por uuid ou por interface** ligada no bootstrap (regra 4) — domain service nunca importa subdomínio irmão. Orquestração que cruza subdomínios não é domain service: é **service de aplicação** e mora em `internal/{dominio}/application/`. **Toda escrita audita** (doc 04) com payload montado à mão — identificadores e vocabulário fechado, nunca texto livre.

### controller.go

```go
package workspace

import (
    "errors"
    "net/http"

    "github.com/gin-gonic/gin"
    "github.com/google/uuid"

    "workspace-api/internal/middleware"
    "workspace-api/internal/pkg/pagination"
    "workspace-api/internal/pkg/rest_err"
)

// PrefixoRotas é o path do subdomínio dentro do grupo /api/domain.
const PrefixoRotas = "/identidade/workspaces"

type Controller interface {
    Routes(routes gin.IRouter)
    Create(c *gin.Context)
    Read(c *gin.Context)
    List(c *gin.Context)
    Update(c *gin.Context)
    Delete(c *gin.Context)
}

type controllerImpl struct{ service Service }

func NewController(service Service) Controller { return &controllerImpl{service: service} }

// Routes declara a cadeia ROTA A ROTA — nunca no grupo (doc 03).
// As funções de PACOTE do middleware falham FECHADAS (403) quando a cadeia não
// foi inicializada no boot — Routes() nunca panica; MustUse() é do bootstrap.
func (ctrl *controllerImpl) Routes(routes gin.IRouter) {
    g := routes.Group(PrefixoRotas)
    g.POST("", middleware.SetContextAuthorization(), middleware.ResolveWorkspace(), middleware.RequirePermission(PermCriar), ctrl.Create)
    g.GET("", middleware.SetContextAuthorization(), middleware.ResolveWorkspace(), middleware.RequirePermission(PermLer), ctrl.List)
    g.GET("/:uuid", middleware.SetContextAuthorization(), middleware.ResolveWorkspace(), middleware.RequirePermission(PermLer), ctrl.Read)
    g.PATCH("/:uuid", middleware.SetContextAuthorization(), middleware.ResolveWorkspace(), middleware.RequirePermission(PermEditar), ctrl.Update)
    g.DELETE("/:uuid", middleware.SetContextAuthorization(), middleware.ResolveWorkspace(), middleware.RequirePermission(PermRemover), ctrl.Delete)
}

// Handlers finos: bind → service → c.JSON. Erro sai SÓ por rest_err.WriteError.

// @Summary      Cria um workspace
// @Description  Cria workspace na organization autenticada, validando slug único global e reservados
// @Tags         Identidade · Workspace
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Param        request body CreateWorkspaceRequestDto true "Dados do workspace"
// @Success      201 {object} WorkspaceResponseDto
// @Failure      400 {object} rest_err.RestErr
// @Failure      403 {object} rest_err.RestErr
// @Failure      409 {object} rest_err.RestErr "Slug em uso"
// @Router       /api/domain/identidade/workspaces [post]
func (ctrl *controllerImpl) Create(c *gin.Context) {
    var dto CreateWorkspaceRequestDto
    if err := c.ShouldBindJSON(&dto); err != nil {
        rest_err.WriteError(c, traduzir(ErrInvalidInput))
        return
    }
    w, err := ctrl.service.Create(c.Request.Context(), dto.ParaEntrada())
    if err != nil {
        rest_err.WriteError(c, traduzir(err))
        return
    }
    c.JSON(http.StatusCreated, NovoWorkspaceResponseDto(w))
}

// @Summary      Consulta um workspace
// @Description  Devolve o workspace no escopo resolvido
// @Tags         Identidade · Workspace
// @Produce      json
// @Security     BearerAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Param        uuid path string true "UUID do workspace"
// @Success      200 {object} WorkspaceResponseDto
// @Failure      400 {object} rest_err.RestErr "UUID malformado"
// @Failure      403 {object} rest_err.RestErr
// @Failure      404 {object} rest_err.RestErr
// @Router       /api/domain/identidade/workspaces/{uuid} [get]
func (ctrl *controllerImpl) Read(c *gin.Context) {
    id, err := uuid.Parse(c.Param("uuid"))
    if err != nil {
        rest_err.WriteError(c, traduzir(ErrInvalidInput))
        return
    }
    w, err := ctrl.service.Read(c.Request.Context(), id)
    if err != nil {
        rest_err.WriteError(c, traduzir(err))
        return
    }
    c.JSON(http.StatusOK, NovoWorkspaceResponseDto(w))
}

// @Summary      Lista workspaces
// @Description  Lista paginada dos workspaces da organization, com filtros
// @Tags         Identidade · Workspace
// @Produce      json
// @Security     BearerAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Param        page query int false "Página (default 1)"
// @Param        pageSize query int false "Itens por página (teto 100)"
// @Param        nome query string false "Filtro por nome"
// @Success      200 {object} pagination.Response[WorkspaceResponseDto]
// @Failure      403 {object} rest_err.RestErr
// @Router       /api/domain/identidade/workspaces [get]
func (ctrl *controllerImpl) List(c *gin.Context) {
    var f ListFilter
    if err := c.ShouldBindQuery(&f); err != nil {
        rest_err.WriteError(c, traduzir(ErrInvalidInput))
        return
    }
    f.Pagination = pagination.DoQuery(c) // lê page/pageSize aplicando o teto de 100 (doc 04)
    items, total, err := ctrl.service.List(c.Request.Context(), f)
    if err != nil {
        rest_err.WriteError(c, traduzir(err))
        return
    }
    c.JSON(http.StatusOK, NovoWorkspaceListaResponseDto(items, total, f.Pagination))
}

// @Summary      Atualiza um workspace
// @Description  Atualização parcial; transição de status passa pelos métodos de comportamento do agregado
// @Tags         Identidade · Workspace
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Param        uuid path string true "UUID do workspace"
// @Param        request body UpdateWorkspaceRequestDto true "Campos a atualizar"
// @Success      200 {object} WorkspaceResponseDto
// @Failure      400 {object} rest_err.RestErr
// @Failure      403 {object} rest_err.RestErr
// @Failure      404 {object} rest_err.RestErr
// @Failure      422 {object} rest_err.RestErr "Transição de estado proibida"
// @Router       /api/domain/identidade/workspaces/{uuid} [patch]
func (ctrl *controllerImpl) Update(c *gin.Context) {
    id, err := uuid.Parse(c.Param("uuid"))
    if err != nil {
        rest_err.WriteError(c, traduzir(ErrInvalidInput))
        return
    }
    var dto UpdateWorkspaceRequestDto
    if err := c.ShouldBindJSON(&dto); err != nil {
        rest_err.WriteError(c, traduzir(ErrInvalidInput))
        return
    }
    in, err := dto.ParaEntrada()
    if err != nil {
        rest_err.WriteError(c, traduzir(err))
        return
    }
    w, err := ctrl.service.Update(c.Request.Context(), id, in)
    if err != nil {
        rest_err.WriteError(c, traduzir(err))
        return
    }
    c.JSON(http.StatusOK, NovoWorkspaceResponseDto(w))
}

// @Summary      Remove um workspace
// @Description  Remoção lógica do workspace no escopo resolvido
// @Tags         Identidade · Workspace
// @Produce      json
// @Security     BearerAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Param        uuid path string true "UUID do workspace"
// @Success      204
// @Failure      400 {object} rest_err.RestErr "UUID malformado"
// @Failure      403 {object} rest_err.RestErr
// @Failure      404 {object} rest_err.RestErr
// @Router       /api/domain/identidade/workspaces/{uuid} [delete]
func (ctrl *controllerImpl) Delete(c *gin.Context) {
    id, err := uuid.Parse(c.Param("uuid"))
    if err != nil {
        rest_err.WriteError(c, traduzir(ErrInvalidInput))
        return
    }
    if err := ctrl.service.Delete(c.Request.Context(), id); err != nil {
        rest_err.WriteError(c, traduzir(err))
        return
    }
    c.Status(http.StatusNoContent)
}

// traduzir converte a sentinela no rest_err do catálogo do subdomínio; desconhecido = 500.
func traduzir(err error) *rest_err.RestErr {
    switch {
    case errors.Is(err, ErrNotFound):
        return rest_err.DoCatalogo(ErrNotFound)
    case errors.Is(err, ErrInvalidInput):
        return rest_err.DoCatalogo(ErrInvalidInput)
    case errors.Is(err, ErrSlugEmUso):
        return rest_err.DoCatalogo(ErrSlugEmUso)
    case errors.Is(err, ErrSlugReservado):
        return rest_err.DoCatalogo(ErrSlugReservado)
    case errors.Is(err, ErrSlugInvalido):
        return rest_err.DoCatalogo(ErrSlugInvalido)
    case errors.Is(err, ErrJaInativo):
        return rest_err.DoCatalogo(ErrJaInativo)
    default:
        return rest_err.Interno(err)
    }
}
```

(`rest_err.DoCatalogo` resolve a sentinela no registro global — alimentado pelo `init()` do `errors.go` — e devolve o `RestErr` com código estável, mensagem e status; `rest_err.Interno` devolve 500 genérico com `ray_trace`.)

## Convenções Go

- Comentário de pacote em todo `model.go`: propósito do subdomínio + entidades + dependências.
- **Entidade nasce por `NewX(input)`** e só muda por método de comportamento — campos exportados são concessão ao GORM, não convite à mutação; struct literal de entidade fora do pacote é reprovada.
- **Value Object** = tipo nomeado com `ParseX(...)` que valida; dado de negócio com formato/invariante nunca viaja como string solta.
- **Interfaces sempre** (`Repository`, `Service`, `Controller`) — implementação `xxxImpl` **privada**, construtor `NewX(...)` exportado.
- `ctx context.Context` é o **primeiro parâmetro** de service e repository; dele saem `organization_uuid`/`workspace_uuid`/`user_uuid`/`ray_trace` (via `orgctx`).
- Erros: sentinela + `errors.Is/As`; **panic só no boot** (`MustUse`, config inválida).
- Log de boot: `[BOOTSTRAP-DI] Contêiner Identidade/<Subdominio> inicializado.`
- Credencial (senha, hash de chave) não sai do subdomínio: `json:"-"`, fora de DTO, comparação como método do service.
- **Idioma dos identificadores**: tipos e APIs técnicas em inglês (`Repository`, `ParseSlug`, `TableName`); **vocabulário de negócio em PT-BR** — campos, métodos de comportamento (`Inativar`, `Renomear`), sentinelas, permissões e helpers de DTO (`ParaEntrada`). Comentários e mensagens sempre em PT-BR.

## Testes

- **Mínimo obrigatório por subdomínio**: `service_test.go` **table-driven** com repository **fake em memória** (implementa a interface `Repository`).
- Regras críticas com teste garantido: slug reservado/duplicado/formato, escopo (service nunca devolve dado de outra organization), unicidade de e-mail por organization, atribuição de papel em workspace alheio.
- Teste que depende de banco fica em `integracao_test.go` e **pula sozinho** (`t.Skip`) quando o serviço não responde — o `go test ./...` continua verde sem docker.
- Rodar: `go build ./... && go vet ./... && go test ./...` antes de fechar a issue; `-race` quando mexer em invariante disputada.

## Checklist de pronto por subdomínio

- [ ] 8 arquivos criados + `permissions.go` (em `application/`: sem `model.go`/`repository.go`, com `contratos.go`)
- [ ] **VOs com validação no construtor** (`ParseX`) — dado com formato/invariante não viaja como string solta
- [ ] **Entidade nasce por `NewX`** (construtor que valida invariantes), nunca por struct literal fora do pacote; transições de estado por métodos de comportamento
- [ ] **Comentário de pacote** no `model.go`: propósito, entidades, dependências
- [ ] **Pacote descrito no `arch-go.yml`** no mesmo commit (coverage 100 reprova pacote sem regra)
- [ ] Termo de negócio novo entra no **glossário do `agents/00`** no mesmo commit
- [ ] Migration up/down criada e **testada** (`up → down → up` em banco efêmero; `migrate validate` sem conexão)
- [ ] Singleton registrado no `cmd/bootstrap` na ordem de dependência, com log `[BOOTSTRAP-DI]`
- [ ] Rotas registradas em `cmd/server/routes`, com a cadeia de auth declarada **rota a rota**
- [ ] Anotações Swagger completas + `swag init -g main.go -o docs` regenerado
- [ ] Auditoria em toda escrita (payload montado à mão)
- [ ] **Catálogo de erros registrado** no `rest_err` via `init()`: toda sentinela com código estável + mensagem PT-BR + status
- [ ] **`Catalogo()` de permissões** com metadados completos (descrição, pares rota+método, grupo de menu), registrado no **agregador do bootstrap**
- [ ] Escopo `orgctx.Scope`/`ScopeOrganization` em todas as queries (ou exceção documentada no `AGENTS.md` do pacote)
- [ ] Testes de service verdes (table-driven, repo fake, sem passar por `New`)
- [ ] `go build ./... && go vet ./... && go test ./...` verdes; **`go test -race ./...`** quando tocar invariante disputada (slug, documento, estado, unicidade)
