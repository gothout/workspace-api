# 01 — Arquitetura DDD e Regras de Camadas

## Visão geral

Monólito modular organizado por **domínio**. Dentro de `internal/`, o negócio se divide em duas camadas:

```
internal/
├── domain/identidade/{subdominio}/       ← camada de DOMÍNIO (regras de negócio do contexto)
└── application/identidade/{subdominio}/  ← camada de APLICAÇÃO (casos de uso cross-domain)
```

- **Domínio (`domain`)**: bounded context do negócio. `identidade` é o domínio do template; `{subdominio}` é o módulo dentro dele (`organization`, `workspace`, `user`). Contém entidade, repositório, serviço, controller, DTOs, erros, permissões e singleton.
- **Aplicação (`application`)**: caso de uso que **orquestra 2+ subdomínios** ou agrega dados deles. Não tem entidade nem persistência próprias; consome services da camada de domínio por **interfaces estreitas** declaradas em `contratos.go`. O subdomínio do template é `application/identidade/catalogo`, que agrega os catálogos de permissões e de erros de todos os subdomínios.
- Regra prática: se a lógica cabe em UM subdomínio → `domain`. Se cruza subdomínios → `application`.

## Estrutura de pastas completa

```
workspace-api/
├── main.go
├── go.mod
├── configs_example.json            ← modelo versionado de config (configs.json é local, ignorado pelo git)
├── arch-go.yml                     ← regras executáveis de camada (ver "Validação da arquitetura")
├── agents/                         ← esta especificação
├── cmd/
│   ├── cli/                        ← entrypoint CLI (cobra): serve, migrate, seed
│   ├── bootstrap/                  ← composição do processo + DI (ver "Padrão de inicialização")
│   └── server/
│       ├── http_server.go          ← gin engine, timeouts, graceful shutdown
│       └── routes/
│           └── routes.go           ← engine, middlewares globais, grupos /api/domain e /api/application
├── db/
│   └── migrations/                 ← migrations SQL (golang-migrate), sequenciais — especificação no doc 02
├── docs/                           ← Swagger GERADO e versionado (swag init -g main.go -o docs)
└── internal/
    ├── pkg/                        ← FOLHA: não importa nada de internal/ fora de pkg
    │   ├── config/                 ← Init(path) no boot, Use()/MustUse() depois
    │   ├── rest_err/               ← corpo de erro padrão + code estável + registro global de erros + WriteError
    │   ├── pagination/             ← page/pageSize (teto 100), Response[T]
    │   ├── orgctx/                 ← escopo organization/workspace no ctx + Scope(db, ctx) fail-closed
    │   └── validator/              ← tags de binding customizadas (slug DNS, documento...)
    ├── infra/                      ← importa só pkg + libs externas; NUNCA outro infra
    │   ├── database/
    │   │   ├── postgres/           ← Connect puro + InitPostgres/GetDB/Close (singleton, FATAL)
    │   │   └── migrations/         ← runner golang-migrate: up no boot (advisory lock) + CLI completa
    │   └── jwt/                    ← emissor/validador de tokens (singleton, FATAL)
    ├── middleware/                 ← SetContextAuthorization, ResolveWorkspace, RequirePermission
    │                               ← NÃO importa domain: dependências por interfaces em contratos.go
    ├── domain/
    │   └── identidade/
    │       ├── organization/       ← raiz da hierarquia; campo `dominio` custom (white-label); apikey
    │       ├── workspace/          ← slug DNS único global; escopo por organization
    │       └── user/               ← credencial fechada no subdomínio; papéis e atribuições por workspace
    └── application/
        └── identidade/
            └── catalogo/           ← agrega Catalogo() de permissões e o catálogo de erros
```

## Anatomia de um subdomínio (pacote Go)

Cada `domain/identidade/{subdominio}/` tem **exatamente** estes arquivos (templates canônicos no doc `05`):

| Arquivo | Responsabilidade |
|---|---|
| `model.go` | Comentário de pacote, constantes `Dominio`/`Subdominio`, tipos nomeados com `Valido()`, entidade GORM com `TableName()`, `CreateInput`/`UpdateInput`, `ListFilter` |
| `dto_request.go` | DTOs de entrada (tags de binding + validação) com `ParaEntrada()`; PATCH usa ponteiros |
| `dto_response.go` | DTOs de saída com construtores `NovoXResponseDto`; nunca expõe hash nem campos internos |
| `errors.go` | Erros sentinela + catálogo {código estável, mensagem PT-BR, status} registrado no mapa global do `rest_err` |
| `permissions.go` | Constantes `PermX` + `Catalogo()` com metadados — alimenta o endpoint de permissões |
| `repository.go` | Interface `Repository` + `repositoryImpl` privada; toda query passa por `orgctx.Scope` |
| `service.go` | Interface `Service` + `serviceImpl` privada; TODA a regra de negócio; auditoria em toda escrita |
| `controller.go` | Interface `Controller` + handlers HTTP finos + `Routes()` com a cadeia de middlewares declarada rota a rota |
| `singleton.go` | `New(deps...)`, `Use()`, `MustUse()` (padrão obrigatório — doc 05) |

Subdomínios de `application/` têm os mesmos arquivos, **exceto**: sem `model.go` (não têm entidade própria) e sem `repository.go` (não persistem nada). No lugar deles, um **`contratos.go`** declara as interfaces estreitas de que o subdomínio precisa — ligadas no `cmd/bootstrap` por adaptadores que resolvem o singleton do lado de lá na chamada e traduzem o vocabulário de erro.

## Regras de dependência (invioláveis)

1. **`pkg` é folha**: não importa nada de `internal/` fora de `pkg`.
2. **`infra` importa só `pkg` + libs externas — nunca outro `infra`.** Quando um infra precisar do outro (ex.: JWT com denylist no Redis, evolução futura), a dependência entra por interface declarada no consumidor e o `cmd/bootstrap` faz a ligação. Nem no código, nem nos testes.
3. **`domain` importa `pkg` + `infra`. Nunca importa `application`.**
4. **Um subdomínio não importa irmão.** A dependência entra por **interface declarada no consumidor** e ligada no `cmd/bootstrap` (adaptador que resolve o singleton do outro lado na chamada). Nem por import direto, nem por variável global compartilhada.
5. **`application` importa `domain` + `pkg` + `infra`.** Mesmo com a regra permitindo o import direto, a fronteira preferida é a interface estreita do `contratos.go` — o pacote fica testável sem banco e sem singleton, e o que atravessa a fronteira é o mínimo.
6. **Dentro do subdomínio: `controller → service → repository`.** Nunca o contrário.
7. **Controller nunca toca `*gorm.DB`.** Recebe o `Service` pela interface.
8. **Todo método de repository recebe `ctx` e aplica `orgctx.Scope`.** Sem escopo a query falha (fail-closed), nunca roda aberta. As exceções (tabelas acima do workspace) estão no doc `03` e no `AGENTS.md` do pacote, com o motivo escrito.

Fluxo de dependências permitido:

```
pkg ← infra ← domain ← application ← cmd (bootstrap/routes)
```

O `internal/middleware` fica fora dessa linha: importa `pkg` + `infra` (JWT) e **não importa `domain`** — os controllers de `domain` importam ELE, então importar os dois lados seria ciclo. Tudo o que o middleware precisa do negócio (vínculo user↔workspace, permissões efetivas, resolução de workspace pelo Host) entra por interface em `contratos.go`, ligada no bootstrap. Cadeia não inicializada = rota **fechada** (403), nunca aberta.

## Padrão de inicialização (bootstrap DI)

Ordem obrigatória no boot (`cmd/bootstrap`):

1. **Config** — `config.Init(path)` (viper lê `configs.json`); erro de config é fatal, com mensagem acionável.
2. **Infra** — `postgres.InitPostgres()` e `jwt.Init(...)`; cada um loga seu estado.
3. **Migrations** — `migrate up` se `databases.migrations.auto_run=true` (o advisory lock do Postgres impede réplicas de migrar juntas); rollback nunca é automático — especificação completa no doc `02`.
4. **Domínios** — `subdominio.New(deps...)` na **ordem de dependência** (`organization` → `workspace` → `user` → `catalogo`), cada um logando `[BOOTSTRAP-DI] Contêiner Identidade/<Subdominio> inicializado.`
5. **HTTP** — `cmd/server/routes` monta o engine e registra as rotas via `Use()` de cada subdomínio; `cmd/server` sobe com graceful shutdown.

O `cmd/bootstrap` é o **único lugar onde os pacotes se encontram**: dois `infra`, um subdomínio e seu irmão, middleware e domain — toda ligação de interface vive aqui.

### Modo degradado

| Dependência | Se cair / faltar |
|---|---|
| PostgreSQL | **Fatal** — erro no boot derruba o processo |
| JWT (secret) | **Fatal** — erro no boot derruba o processo |
| Redis, ClickHouse (evoluções futuras) | Degradáveis: `Connect` devolve cliente **nulo** com log `[DEGRADADO]`; o consumidor é obrigado a tratar a ausência |

## Validação da arquitetura

As regras 1–8 valem em duas frentes complementares:

### 1. arch-go (gate executável)

- `arch-go.yml` na raiz descreve cada camada com **`shouldOnlyDependsOn`**: a camada declara o que **pode** importar — pacote criado depois nasce **proibido**, não permitido.
- Roda dentro do `go test ./...` (`TestArquiteturaDoProjeto` na raiz), que **pula sozinho** se a ferramenta não estiver instalada — o `go test` continua verde sem ela; com ela instalada, a reprovação é gate.
- **Compliance e coverage em 100**: pacote novo que nenhuma regra descreve reprova — subdomínio novo entra no `arch-go.yml` no mesmo commit em que nasce.
- O arch-go só enxerga imports de **arquivos de produção** — import que só existe em `_test.go` não é verificado; as regras 2 e 4 valem **à mão também nos testes**.

### 2. Dependency Graph do Go Architect (conferência visual)

- Ferramenta: [Dependency Graph](https://go-architect.github.io/docs/analysis-tools/dependency-graph/) do Go Architect. Apontar para a pasta do projeto: ela desenha o grafo de pacotes, classificando os nós em Internal / External / StandardLib / Organization.
- **Quando rodar:** ao fechar cada fase (ver doc `06`).
- **O que conferir:** o fluxo `pkg ← infra ← domain ← application ← cmd` está valendo; nenhuma aresta `infra → infra`, nenhuma `subdomínio → irmão`, nenhuma `domain → application`, nenhuma `middleware → domain`.
- Dependência inesperada no grafo = parar e corrigir antes de fechar a fase; registrar a conferência no log do `06`.
