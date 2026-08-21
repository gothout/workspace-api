# 01 — Arquitetura DDD e Regras de Camadas

## Visão geral

Monólito modular organizado por **domínio**. O negócio dentro de `internal/`
tem **três níveis**, e cada um responde a uma pergunta diferente:

| Nível | Pergunta que responde | Onde mora |
|---|---|---|
| **Domínio** | De que grande área do negócio estamos falando? | `internal/{dominio}/` — pasta **direta** de `internal/` |
| **Subdomínio** | Qual recorte coeso, com entidades e tabelas próprias? | `internal/{dominio}/domain/{subdominio}/` |
| **Aplicação** | Qual caso de uso atravessa 2+ subdomínios? | `internal/{dominio}/application/{nome}/` |

Hoje existe um único domínio — `identidade` — com três subdomínios
(`organization`, `workspace`, `user`) e duas aplicações (`catalogo` e
`auth`). A forma se repete para todo domínio futuro (ex.: `billing`).

## Os três níveis em detalhe

### Domínio — `internal/{dominio}/`

O domínio é o **bounded context**: uma grande área do negócio com vocabulário
e invariantes próprios. `identidade` agrupa tudo o que fala de organization,
workspace, usuário, papel e permissão — esses conceitos compartilham
invariantes (a hierarquia organization → workspace → user, o escopo por
linha), então moram no mesmo domínio.

Na prática, o domínio é uma **pasta direta de `internal/`** que contém sempre
dois filhos:

- `domain/` — os subdomínios (a regra de negócio);
- `application/` — os casos de uso que orquestram esses subdomínios.

**Critério de decisão — domínio novo ou encaixar no existente?** Cria-se um
domínio novo quando a área tem vocabulário e invariantes que **não se
misturam** com os de um domínio existente: `billing` (faturas, planos,
cobrança) não compartilha entidades nem regras com `identidade` — vira
`internal/billing/`. Se o conceito novo fala o vocabulário de um domínio
existente e participa das invariantes dele (ex.: "convite de usuário" fala de
user e workspace), ele é **subdomínio** daquele domínio, não domínio novo.

### Subdomínio — `internal/{dominio}/domain/{subdominio}/`

O subdomínio é o **recorte coeso** dentro do domínio: dono de entidades e
tabelas próprias, com a anatomia completa de **8 arquivos +
`permissions.go`** (detalhe na seção "Anatomia" abaixo). É nele que mora
TODA a regra de negócio do recorte.

**Regra prática:** a lógica cabe dentro de um subdomínio **sem conversar com
o vizinho**? Então é `domain/`. Criar workspace, por exemplo, só precisa das
tabelas e invariantes do próprio `workspace` — mora em
`internal/identidade/domain/workspace`.

### Aplicação — `internal/{dominio}/application/{nome}/`

A aplicação é o **caso de uso que orquestra 2+ subdomínios do mesmo
domínio** (ou agrega dados de todos eles). Ela **não tem entidade nem
persistência próprias** — sem `model.go` e sem `repository.go`. No lugar
deles, um **`contratos.go`** declara as interfaces estreitas de que a
aplicação precisa (o consumidor dita o contrato), ligadas no `cmd/bootstrap`
por adaptadores que resolvem o singleton do subdomínio **na chamada**.

**Regra prática:** o caso de uso **cruza subdomínios**? Sobe para
`application/`. Orquestração de UM subdomínio só não é aplicação — é sinal
de que a lógica está na camada errada e pertence ao subdomínio.

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
    ├── pkg/                        ← CAMADA TRANSVERSAL, folha: não importa nada de internal/ fora de pkg
    │   ├── config/                 ← Init(path) no boot, Use()/MustUse() depois
    │   ├── rest_err/               ← corpo de erro padrão + code estável + registro global de erros + WriteError
    │   ├── pagination/             ← page/pageSize (teto 100), Response[T]
    │   ├── orgctx/                 ← escopo organization/workspace no ctx + Scope(db, ctx) fail-closed
    │   └── validator/              ← tags de binding customizadas (slug DNS, documento...)
    ├── infra/                      ← CAMADA TÉCNICA: importa só pkg + libs externas; NUNCA outro infra
    │   ├── database/
    │   │   ├── postgres/           ← Connect puro + InitPostgres/GetDB/Close (singleton, FATAL)
    │   │   └── migrations/         ← runner golang-migrate: up no boot (advisory lock) + CLI completa
    │   └── jwt/                    ← emissor/validador de tokens (singleton, FATAL)
    ├── middleware/                 ← SetContextAuthorization, ResolveWorkspace, RequirePermission
    │                               ← NÃO importa nenhum {dominio}/domain: dependências por interfaces em contratos.go
    └── identidade/                 ← DOMÍNIO (bounded context) — pasta direta de internal/
        ├── domain/                 ← camada de DOMÍNIO: subdomínios com regra de negócio e tabelas próprias
        │   ├── organization/       ← raiz da hierarquia; campo `dominio` custom (white-label); apikey
        │   ├── workspace/          ← slug DNS único global; escopo por organization
        │   └── user/               ← credencial fechada no subdomínio; papéis e atribuições por workspace
        └── application/            ← camada de APLICAÇÃO: casos de uso que cruzam os subdomínios acima
            ├── catalogo/           ← agrega Catalogo() de permissões e o catálogo de erros
            └── auth/               ← login/refresh/logout: user × resolução da organization pelo Host
```

Domínio futuro nasce com a mesma forma: `internal/billing/domain/{...}` +
`internal/billing/application/{...}`.

## Exemplo ponta a ponta

Dois casos mostram onde cada coisa mora:

1. **"Criar workspace"** — regra de negócio de UM recorte: validar formato e
   unicidade global do slug, gravar na tabela `identidade_workspace_workspace`.
   Tudo acontece dentro de **`internal/identidade/domain/workspace`**, que
   expõe `POST /api/domain/identidade/workspaces`.
2. **"Listar as permissões que o usuário tem"** — cruza `organization` +
   `workspace` + `user` (vínculo, papéis por workspace, permissões de cada
   papel) e agrega o `Catalogo()` de todos os subdomínios. Nenhum subdomínio
   sozinho sabe responder — sobe para
   **`internal/identidade/application/catalogo`**, que recebe os três por
   interfaces do `contratos.go` e expõe
   `GET /api/application/identidade/catalogo/permissoes/minhas`.

## Anatomia de um subdomínio (pacote Go)

Cada `internal/{dominio}/domain/{subdominio}/` tem **exatamente** estes arquivos (templates canônicos no doc `05`):

| Arquivo | Responsabilidade |
|---|---|
| `model.go` | Comentário de pacote, constantes `Dominio`/`Subdominio`, tipos nomeados com `Valido()`, **Value Objects** com construtor validador (`ParseSlug`), entidade GORM com `TableName()` + **construtor `NewX` que valida invariantes** + métodos de comportamento, `CreateInput`/`UpdateInput`, `ListFilter` |
| `dto_request.go` | DTOs de entrada (tags de binding + validação) com `ParaEntrada()`; PATCH usa ponteiros |
| `dto_response.go` | DTOs de saída com construtores `NovoXResponseDto`; nunca expõe hash nem campos internos |
| `errors.go` | Erros sentinela + catálogo {código estável, mensagem PT-BR, status} registrado no mapa global do `rest_err` |
| `permissions.go` | Constantes `PermX` + `Catalogo()` com metadados — alimenta o endpoint de permissões |
| `repository.go` | Interface `Repository` + `repositoryImpl` privada; **um por agregado**, métodos em linguagem de negócio; toda query passa por `orgctx.Scope` |
| `service.go` | Interface `Service` + `serviceImpl` privada; **domain service do agregado** — TODA a regra de negócio; auditoria em toda escrita |
| `controller.go` | Interface `Controller` + handlers HTTP finos + `Routes()` com a cadeia de middlewares declarada rota a rota |
| `singleton.go` | `New(deps...)`, `Use()`, `MustUse()` (padrão obrigatório — doc 05) |

Aplicações de `internal/{dominio}/application/` têm os mesmos arquivos, **exceto**: sem `model.go` (não têm entidade própria) e sem `repository.go` (não persistem nada). No lugar deles, o **`contratos.go`** declara as interfaces estreitas de que a aplicação precisa — ligadas no `cmd/bootstrap` por adaptadores que resolvem o singleton do lado de lá na chamada e traduzem o vocabulário de erro.

## Do caminho para o contrato: rotas, tabelas e permissões

O caminho `internal/{dominio}/domain/{subdominio}` determina o nome público
de tudo o que o subdomínio expõe — mudou o lugar, mudam juntos:

| Origem | Derivação | Exemplo (`identidade` + `workspace`) |
|---|---|---|
| Tabela | `{dominio}_{subdominio}_{entidade}` (doc 02) | `identidade_workspace_workspace` |
| Migration | `NNNN_{dominio}_{subdominio}_{desc}.{up,down}.sql` | `0002_identidade_workspace_create.up.sql` |
| Rota | `/api/domain/{dominio}/{recurso-plural}` | `/api/domain/identidade/workspaces` |
| Permissão | `{dominio}:{subdominio}:{acao}` | `identidade:workspace:editar` |
| Código de erro | `{dominio}.{subdominio}.{nome}` | `identidade.workspace.slug_em_uso` |

Aplicação segue a mesma lógica na família `/api/application`:
`internal/identidade/application/catalogo` →
`/api/application/identidade/catalogo/...`. Aplicação **não** tem tabela nem
migration próprias — não persiste nada.

**Rotas de sistema são exceção de prefixo**: `/api/status` (sondas) e
`/api/system/errors` (mapa de erros) ficam fora das famílias
`/api/domain|/api/application` — decidido uma vez, documentado aqui e no
doc `04`; nenhuma rota nova sai do padrão sem a mesma formalidade.

## Padrões táticos de DDD (onde cada padrão mora)

Até aqui, o DDD **estratégico**: contextos e fronteiras. Dentro de cada
subdomínio valem os padrões **táticos** (Evans/Vernon), adaptados ao Go sem
cerimônia. Templates canônicos no doc `05`.

### Linguagem onipresente

Os termos do negócio — organization, workspace, user, papel, atribuição,
catálogo — são os **mesmos** no código, nas tabelas, nas rotas e nas
mensagens de erro. O glossário canônico mora no `agents/00`; termo de negócio
novo entra no glossário **no mesmo commit** em que entra no código. Se o nome
não serve numa conversa com o negócio, ele está errado no código também.

### Entidade vs. Value Object

- **Entidade** tem identidade (`uuid`) e ciclo de vida: `Organization`,
  `Workspace`, `User`. Duas instâncias com os mesmos dados e uuids diferentes
  são coisas diferentes.
- **Value Object** se define inteiramente pelos valores e é imutável:
  `Slug`, `Email`, `Dominio` (custom). Em Go, VO = **tipo nomeado com
  construtor que valida** (`ParseSlug(string) (Slug, error)`) mais métodos de
  comportamento — nunca uma string solta viajando pelos inputs, nunca struct
  anêmica espalhada pelo pacote.

### Agregado e raiz de agregado

Cada subdomínio hospeda **um agregado** (ou poucos, com raiz explícita). A
raiz é a **única porta de entrada**: nada altera um workspace senão pelo
agregado `Workspace`; registro filho (ex.: `atribuicao`, no subdomínio
`user`) só é tocado pela sua raiz. Invariantes de consistência imediata vivem
**dentro do agregado** — construtor e métodos. Entre agregados, a referência
é **por uuid, nunca por join de escrita**: `Workspace.OrganizationUUID`
aponta para a organization, e quem precisa do dado dela pergunta ao
subdomínio dela (por interface — regra 4).

### Repositório: um por agregado

Interface `Repository` declarada no próprio subdomínio, **uma por agregado**,
com métodos em **linguagem de negócio** (`FindBySlug`, `SlugOcupado`) — o que
o negócio pergunta, não um CRUD genérico disfarçado. Todo método recebe `ctx`
e aplica `orgctx.Scope` (regra 8).

### Service de domínio vs. service de aplicação

O `service.go` do subdomínio é o **domain service**: orquestra as regras do
agregado que não cabem num método da entidade (lista de reservados,
unicidade amigável, cascata de inativação). O que **cruza subdomínios não é
domain service** — é service de **aplicação** e sobe para
`internal/{dominio}/application/`. A regra já era inviolável; aqui ela ganha
o nome DDD.

### Invariantes no construtor

`model.go` expõe `NewX(input) (*X, error)`, que valida as invariantes antes
de devolver a entidade. Controller e service **nunca** montam entidade campo
a campo: struct literal de entidade fora do pacote é reprovada em revisão.
Entidade ≠ DTO ≠ input — o DTO carrega dado cru, o construtor devolve
entidade válida.

### Anti-corrupção entre subdomínios

As interfaces de `contratos.go` + os adaptadores do `cmd/bootstrap` são a
**camada anti-corrupção (ACL)** do monólito: o consumidor dita o contrato no
**próprio vocabulário** e o adaptador traduz o modelo do vizinho — nenhum
modelo de subdomínio vaza para outro.

### Eventos de domínio

**Fora do núcleo.** Comunicação entre agregados hoje é chamada direta via
contratos; eventos de domínio entram como evolução futura, junto ao
errobserve/ClickHouse (doc `02`). Não inventar antes da hora.

## Regras de dependência (invioláveis)

1. **`pkg` é folha**: não importa nada de `internal/` fora de `pkg`.
2. **`infra` importa só `pkg` + libs externas — nunca outro `infra`.** Quando um infra precisar do outro (ex.: JWT com denylist no Redis, evolução futura), a dependência entra por interface declarada no consumidor e o `cmd/bootstrap` faz a ligação. Nem no código, nem nos testes.
3. **`internal/{dominio}/domain` importa `pkg` + `infra` + `middleware`. Nunca importa `application` nem `cmd`.**
4. **Um subdomínio não importa irmão.** A dependência entra por **interface declarada no consumidor** e ligada no `cmd/bootstrap` (adaptador que resolve o singleton do outro lado na chamada). Nem por import direto, nem por variável global compartilhada.
5. **`internal/{dominio}/application` importa `pkg` + `infra` + `middleware` — nunca os pacotes de `domain/` do próprio domínio.** Os subdomínios vizinhos entram **só por interface estreita** do `contratos.go`: o pacote fica testável sem banco e sem singleton, e o que atravessa a fronteira é o mínimo.
6. **Dentro do subdomínio: `controller → service → repository`.** Nunca o contrário.
7. **Controller nunca toca `*gorm.DB`.** Recebe o `Service` pela interface.
8. **Todo método de repository recebe `ctx` e aplica o escopo — duas variantes, ambas fail-closed**: `orgctx.Scope` (organization **e** workspace; tabelas da vida dentro do workspace) ou `orgctx.ScopeOrganization` (só organization; tabelas **acima** do workspace, como `workspace` e `user`). Sem escopo a query falha, nunca roda aberta. As exceções (raiz sem escopo, globais da plataforma, `FindBySlug` global) estão no doc `03` e no `AGENTS.md` do pacote, com o motivo escrito.

Fluxo de dependências permitido:

```
pkg ← infra ← {dominio}/domain ← {dominio}/application ← cmd (bootstrap/routes)
```

O `internal/middleware` fica fora dessa linha: importa `pkg` + `infra` (JWT) e **não importa nenhum `internal/{dominio}/domain`** — os controllers de `domain` importam ELE, então importar os dois lados seria ciclo. Tudo o que o middleware precisa do negócio (vínculo user↔workspace, permissões efetivas, resolução de workspace pelo Host) entra por interface em `contratos.go`, ligada no bootstrap. Cadeia não inicializada = rota **fechada** (403), nunca aberta.

## Padrão de inicialização (bootstrap DI)

Ordem obrigatória no boot (`cmd/bootstrap`):

1. **Config** — `config.Init(path)` (viper lê `configs.json`); erro de config é fatal, com mensagem acionável.
2. **Validator** — registro das tags customizadas do `pkg/validator` no gin.
3. **Infra** — `postgres.InitPostgres()` e `jwt.Init(...)`, ambos **fatais**; cada um loga seu estado.
4. **Migrations** — `migrate up` se `databases.migrations.auto_run=true` (o advisory lock do Postgres impede réplicas de migrar juntas); rollback nunca é automático — especificação completa no doc `02`. Arquivos marcados `-- manual` são ignorados aqui, com log de alerta.
5. **Middleware** — `middleware.New(...)`: liga os contratos via adaptadores **antes** do registro de rotas, porque o `Routes()` dos controllers consome a cadeia. Os adaptadores resolvem os singletons **na chamada**, então o middleware sobe antes dos domínios sem conhecer o concreto deles.
6. **Domínios** — `New(deps...)` dos subdomínios de `internal/identidade/domain/` na **ordem de dependência** (`organization` → `workspace` → `user`) e depois das aplicações (`internal/identidade/application/catalogo`, `.../auth`), cada um logando `[BOOTSTRAP-DI] Contêiner Identidade/<Subdominio> inicializado.`
7. **HTTP** — `cmd/server/routes` monta o engine e registra as rotas via `Use()` de cada subdomínio; `cmd/server` sobe com graceful shutdown.

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

Esqueleto das regras (o `arch-go.yml` canônico nasce na F0 — este é o formato
que as regras de camada seguem):

```yaml
version: 1
threshold:
  compliance: 100
  coverage: 100
dependenciesRules:
  - package: "workspace-api/internal/pkg"           # folha: só enxerga pkg
    shouldOnlyDependsOn:
      internal:
        - "workspace-api/internal/pkg"
  - package: "workspace-api/internal/infra"         # infra: só pkg + libs
    shouldOnlyDependsOn:
      internal:
        - "workspace-api/internal/pkg"
  - package: "workspace-api/internal/middleware"    # sem {dominio}/domain (ciclo)
    shouldOnlyDependsOn:
      internal:
        - "workspace-api/internal/pkg"
        - "workspace-api/internal/infra"
  - package: "workspace-api/internal.*.domain"      # subdomínios de QUALQUER domínio
    shouldOnlyDependsOn:
      internal:
        - "workspace-api/internal/pkg"
        - "workspace-api/internal/infra"
        - "workspace-api/internal/middleware"
  - package: "workspace-api/internal.*.application" # aplicações de QUALQUER domínio
    shouldOnlyDependsOn:
      internal:
        - "workspace-api/internal/pkg"
        - "workspace-api/internal/infra"
        - "workspace-api/internal/middleware"
  - package: "workspace-api/cmd"                    # composição: pode importar todas as camadas
    shouldOnlyDependsOn:
      internal:
        - "workspace-api/internal"
  - package: "workspace-api"                        # pacote raiz (main + teste de arquitetura) e docs/ gerado
    shouldOnlyDependsOn:
      internal:
        - "workspace-api"
```

Sem regra para `cmd/**`, pacote raiz e `docs/` (Swagger gerado), o coverage
100 reprova a F0: `shouldOnlyDependsOn` nasce proibindo — todo pacote
precisa de regra, mesmo que permissiva.

Como `shouldOnlyDependsOn` lista tudo o que é permitido, o que não está na
lista reprova: irmão importando irmão (`...domain/user` → `...domain/organization`),
`application` importando `domain/`, `middleware` importando qualquer
`internal/{dominio}/...` — todos caem no gate sem regra extra.

### 2. Dependency Graph do Go Architect (conferência visual)

- Ferramenta: [Dependency Graph](https://go-architect.github.io/docs/analysis-tools/dependency-graph/) do Go Architect. Apontar para a pasta do projeto: ela desenha o grafo de pacotes, classificando os nós em Internal / External / StandardLib / Organization.
- **Quando rodar:** ao fechar cada fase (ver doc `06`).
- **O que conferir:** o fluxo `pkg ← infra ← {dominio}/domain ← {dominio}/application ← cmd` está valendo; nenhuma aresta `infra → infra`, nenhuma `subdomínio → irmão`, nenhuma `domain → application`, nenhuma `middleware → internal/{dominio}/domain`.
- Dependência inesperada no grafo = parar e corrigir antes de fechar a fase; registrar a conferência no log do `06`.
