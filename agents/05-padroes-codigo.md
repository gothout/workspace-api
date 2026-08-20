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
    ErrNotInitialized  = errors.New("workspace controller not initialized")
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

## Templates de arquivo

### model.go

```go
// Package workspace implementa o subdomínio workspace do domínio identidade:
// unidade de trabalho de uma organization, endereçada por slug DNS único global.
// Entidades: Workspace. Dependências: pkg (orgctx, rest_err, pagination), infra (gorm/pgx).
package workspace

import (
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

// Workspace é a entidade raiz do subdomínio.
type Workspace struct {
    UUID             uuid.UUID       `gorm:"column:uuid;type:uuid;primaryKey" json:"uuid"`
    OrganizationUUID uuid.UUID       `gorm:"column:organization_uuid;type:uuid;not null;index:idx_workspace_scope,priority:1" json:"organization_uuid"`
    Nome             string          `gorm:"column:nome;not null" json:"nome"`
    Slug             string          `gorm:"column:slug;not null" json:"slug"`
    Status           StatusWorkspace `gorm:"column:status;not null;default:'ativo'" json:"status"`
    CreatedAt        time.Time       `gorm:"column:created_at;autoCreateTime" json:"created_at"`
    UpdatedAt        time.Time       `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
    DeletedAt        gorm.DeletedAt  `gorm:"column:deleted_at;index" json:"-"`
}

func (Workspace) TableName() string { return "identidade_workspace_workspace" }

// CreateInput e UpdateInput carregam só o que a regra permite escrever.
type CreateInput struct {
    Nome string
    Slug string
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
        Slug: w.Slug, Status: string(w.Status),
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
var (
    ErrNotFound      = errors.New("workspace não encontrado")
    ErrInvalidInput  = errors.New("dados de entrada inválidos")
    ErrSlugEmUso     = errors.New("slug já está em uso por outro workspace")
    ErrSlugReservado = errors.New("slug reservado pela plataforma")
    ErrSlugInvalido  = errors.New("slug fora do formato DNS")
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
    PermExcluir = "identidade:workspace:excluir"
)

// PermissaoMeta — metadados da permissão para o catálogo consultável (doc 03).
// Mesma forma em todos os subdomínios; o bootstrap agrega os Catalogo() no registro
// único do application/identidade/catalogo (domain não importa application — regra 3).
type PermissaoMeta struct {
    Permissao string   // valor exato exigido pela rota
    Descricao string   // PT-BR: o que a permissão libera
    Rotas     []string // paths que exigem esta permissão
    Metodo    string   // método HTTP
    GrupoMenu string   // agrupamento sugerido para o menu do front-end
}

// Catalogo devolve TODAS as permissões do subdomínio com metadados.
// Sem ele o subdomínio não aparece no endpoint de permissões e o checklist não fecha.
func Catalogo() []PermissaoMeta {
    return []PermissaoMeta{
        {Permissao: PermCriar, Descricao: "Criar workspace na organization", Rotas: []string{"/api/domain/identidade/workspaces"}, Metodo: http.MethodPost, GrupoMenu: "Identidade · Workspaces"},
        {Permissao: PermLer, Descricao: "Listar e consultar workspaces da organization", Rotas: []string{"/api/domain/identidade/workspaces", "/api/domain/identidade/workspaces/{uuid}"}, Metodo: http.MethodGet, GrupoMenu: "Identidade · Workspaces"},
        {Permissao: PermEditar, Descricao: "Editar dados do workspace", Rotas: []string{"/api/domain/identidade/workspaces/{uuid}"}, Metodo: http.MethodPatch, GrupoMenu: "Identidade · Workspaces"},
        {Permissao: PermExcluir, Descricao: "Remover workspace", Rotas: []string{"/api/domain/identidade/workspaces/{uuid}"}, Metodo: http.MethodDelete, GrupoMenu: "Identidade · Workspaces"},
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

type Repository interface {
    Create(ctx context.Context, w *Workspace) error
    FindByUUID(ctx context.Context, id uuid.UUID) (*Workspace, error)
    FindBySlug(ctx context.Context, slug string) (*Workspace, error)
    List(ctx context.Context, f ListFilter) ([]Workspace, int64, error)
    Update(ctx context.Context, w *Workspace) error
    Delete(ctx context.Context, id uuid.UUID) error
}

type repositoryImpl struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) Repository { return &repositoryImpl{db: db} }

// Toda query passa pelo escopo — sem escopo no ctx, orgctx.Scope FALHA (fail-closed).
func (r *repositoryImpl) FindByUUID(ctx context.Context, id uuid.UUID) (*Workspace, error) {
    var w Workspace
    err := orgctx.Scope(r.db.WithContext(ctx), ctx).
        Where("uuid = ?", id).First(&w).Error
    if errors.Is(err, gorm.ErrRecordNotFound) {
        return nil, ErrNotFound
    }
    if err != nil {
        return nil, err
    }
    return &w, nil
}

func (r *repositoryImpl) Create(ctx context.Context, w *Workspace) error {
    return traduzirErroDriver(orgctx.Scope(r.db.WithContext(ctx), ctx).Create(w).Error)
}

// Update e Delete conferem RowsAffected: 0 linhas = registro inexistente ou fora do escopo.
func (r *repositoryImpl) Delete(ctx context.Context, id uuid.UUID) error {
    res := orgctx.Scope(r.db.WithContext(ctx), ctx).
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

    "github.com/google/uuid"
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
```

TODA a regra de negócio vive aqui: normalização do slug, lista de reservados, formato DNS, unicidade amigável (checagem prévia além do 23505). O controller nunca tem regra. **Toda escrita audita** (doc 04) com payload montado à mão — identificadores e vocabulário fechado, nunca texto livre.

### controller.go

```go
package workspace

import (
    "errors"
    "net/http"

    "github.com/gin-gonic/gin"

    "workspace-api/internal/middleware"
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
func (ctrl *controllerImpl) Routes(routes gin.IRouter) {
    g := routes.Group(PrefixoRotas)
    mw := middleware.MustUse().Middleware
    g.POST("", mw.SetContextAuthorization(), mw.ResolveWorkspace(), mw.RequirePermission(PermCriar), ctrl.Create)
    g.GET("", mw.SetContextAuthorization(), mw.ResolveWorkspace(), mw.RequirePermission(PermLer), ctrl.List)
    g.GET("/:uuid", mw.SetContextAuthorization(), mw.ResolveWorkspace(), mw.RequirePermission(PermLer), ctrl.Read)
    g.PATCH("/:uuid", mw.SetContextAuthorization(), mw.ResolveWorkspace(), mw.RequirePermission(PermEditar), ctrl.Update)
    g.DELETE("/:uuid", mw.SetContextAuthorization(), mw.ResolveWorkspace(), mw.RequirePermission(PermExcluir), ctrl.Delete)
}

// Handlers finos: bind → service → c.JSON. Erro sai SÓ por rest_err.WriteError.
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
    default:
        return rest_err.Interno(err)
    }
}
```

(`rest_err.DoCatalogo` resolve a sentinela no registro global — alimentado pelo `init()` do `errors.go` — e devolve o `RestErr` com código estável, mensagem e status; `rest_err.Interno` devolve 500 genérico com `ray_trace`.)

## Convenções Go

- Comentário de pacote em todo `model.go`: propósito do subdomínio + entidades + dependências.
- **Interfaces sempre** (`Repository`, `Service`, `Controller`) — implementação `xxxImpl` **privada**, construtor `NewX(...)` exportado.
- `ctx context.Context` é o **primeiro parâmetro** de service e repository; dele saem `organization_uuid`/`workspace_uuid`/`user_uuid`/`ray_trace` (via `orgctx`).
- Erros: sentinela + `errors.Is/As`; **panic só no boot** (`MustUse`, config inválida).
- Log de boot: `[BOOTSTRAP-DI] Contêiner Identidade/<Subdominio> inicializado.`
- Credencial (senha, hash de chave) não sai do subdomínio: `json:"-"`, fora de DTO, comparação como método do service.
- Métodos e funções em PT-BR nos comentários; identificadores em inglês.

## Testes

- **Mínimo obrigatório por subdomínio**: `service_test.go` **table-driven** com repository **fake em memória** (implementa a interface `Repository`).
- Regras críticas com teste garantido: slug reservado/duplicado/formato, escopo (service nunca devolve dado de outra organization), unicidade de e-mail por organization, atribuição de papel em workspace alheio.
- Teste que depende de banco fica em `integracao_test.go` e **pula sozinho** (`t.Skip`) quando o serviço não responde — o `go test ./...` continua verde sem docker.
- Rodar: `go build ./... && go vet ./... && go test ./...` antes de fechar a issue; `-race` quando mexer em invariante disputada.

## Checklist de pronto por subdomínio

- [ ] 8 arquivos criados + `permissions.go` (em `application/`: sem `model.go`/`repository.go`, com `contratos.go`)
- [ ] Migration up/down criada e **testada** (`up → down → up` em banco efêmero; `migrate validate` sem conexão)
- [ ] Singleton registrado no `cmd/bootstrap` na ordem de dependência, com log `[BOOTSTRAP-DI]`
- [ ] Rotas registradas em `cmd/server/routes`, com a cadeia de auth declarada **rota a rota**
- [ ] Anotações Swagger completas + `swag init -g main.go -o docs` regenerado
- [ ] Auditoria em toda escrita (payload montado à mão)
- [ ] **Catálogo de erros registrado** no `rest_err`: toda sentinela com código estável + mensagem PT-BR + status
- [ ] **`Catalogo()` de permissões** com metadados completos (descrição, rotas, método, grupo de menu)
- [ ] Escopo `orgctx.Scope` em todas as queries (ou exceção documentada no `AGENTS.md` do pacote)
- [ ] Testes de service verdes (table-driven, repo fake)
- [ ] `go build ./... && go vet ./... && go test ./...` verdes
