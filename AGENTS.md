# AGENTS.md — workspace-api

Template de API gerenciadora em Go com hierarquia **organization → workspace →
user**, arquitetura por domínio (DDD) e padrão singleton. Convenções que valem
para qualquer alteração neste repositório. A especificação completa está em
`agents/` — **ler `agents/README.md` primeiro**.

## Toolchain Go

- Go **1.25** (ver `go.mod`). Não subir o `go` directive sem necessidade real.
- Dependência nova entra pelo código que a usa, nunca por arquivo-âncora.

## Checks obrigatórios antes de commitar

```bash
go build ./... && go vet ./... && go test ./...
```

Quem mexer em invariante disputada (slug, documento, estado, unicidade) roda
também o detector de corridas:

```bash
go test -race ./...
```

## Estrutura e regras de dependência

Layout de pastas e regras invioláveis: `agents/01`. O **domínio é pasta direta
de `internal/`** (hoje `identidade` e `licensing`): os modelos expostos
(entidades, VOs, invariantes) moram em
`internal/{dominio}/model/{subdominio}` (folha, importável por qualquer
camada), os subdomínios em `internal/{dominio}/domain/{subdominio}` e as
orquestrações que atravessam 2+ subdomínios em
`internal/{dominio}/application/{nome}`. Resumo do fluxo permitido:

```
pkg ← infra ← {dominio}/model ← {dominio}/domain ← {dominio}/application ← cmd
```

- `internal/pkg` é **folha**: não importa nada de `internal/` fora de `pkg`.
- `internal/infra` importa só `pkg` + libs externas — **nunca outro `infra`**.
- Um subdomínio **não importa irmão** nem `application`; dependência entra por
  interface declarada no consumidor e ligada no `cmd/bootstrap`.
- Dentro do subdomínio: `controller → service → repository`, nunca o contrário.
- `internal/middleware` **não importa nenhum `internal/{dominio}/domain`**
  (seria ciclo): as dependências entram por interfaces em `contratos.go`,
  ligadas no bootstrap.

Validação dupla da arquitetura:

1. **arch-go** (`arch-go.yml`) é o gate executável: roda dentro do
   `go test ./...`, com `coverage 100` — pacote novo que nenhuma regra descreve
   reprova. O padrão é `shouldOnlyDependsOn`: cada camada declara o que **pode**
   importar, então camada criada depois nasce proibida.
2. **Dependency Graph do Go Architect**
   ([docs](https://go-architect.github.io/docs/analysis-tools/dependency-graph/))
   é a conferência visual: apontar a ferramenta para a pasta do projeto e
   verificar no grafo que o fluxo de camadas está valendo — sem infra
   importando infra, sem irmão importando irmão. Rodar ao fechar cada fase.

## Padrão singleton

Todo pacote de `infra` e todo subdomínio segue o par **função pura + singleton
do processo** (detalhes e templates em `agents/05`):

- `Connect(...)`/construtores puros, testáveis e sem estado.
- `New(deps...)` com `sync.Once` montando repo → service → controller.
- `Use()` devolve erro se não inicializado (usado pelo registro de rotas);
  `MustUse()` entra em pânico (restrito ao bootstrap).
- Postgres e JWT são **fatais** (erro no boot derruba o processo); dependências
  futuras degradáveis (Redis, ClickHouse) devolvem cliente nulo com log
  `[DEGRADADO]` e o consumidor é obrigado a tratar a ausência.

## Configuração

- `internal/pkg/config`: `config.Init(path)` no boot, `config.Use()`/`MustUse()`
  depois.
- `configs_example.json` é o modelo **versionado**; `configs.json` é local e
  ignorado pelo git.

## Migrations

SQL puro em `db/migrations/NNNN_{dominio}_{subdominio}_{desc}.{up,down}.sql`
(hoje `{dominio}` = `identidade` e `licensing`).
**Todo `up` tem `down`** no mesmo commit, exercitado por `up → down → up` em
banco efêmero; migration aplicada nunca é editada (correção = migration nova);
`up` roda automaticamente no boot (advisory lock), rollback é manual via CLI.
Especificação completa em `agents/02` e `db/migrations/AGENTS.md`.

## Escopo e autorização

- **Fail-closed**: toda query de tabela de negócio passa por `orgctx.Scope` —
  sem escopo a query falha, nunca roda aberta.
- Auth é declarada **rota a rota** pelo `Routes()` do controller, nunca no
  grupo: `RequirePermission("identidade:workspace:editar")` com permissão
  `dominio:subdominio:acao` granular.
- Resolução de workspace pelo Host: `{slug}.{base_domain}` da plataforma **e**
  `{slug}.{dominio-custom}` da organization (white-label), validando o
  pertencimento. Detalhes em `agents/03`.

## Observabilidade de contrato (mapping para o front-end)

- Todo erro possível do sistema é mapeado no `errors.go` do subdomínio (code
  estável + mensagem + status) e exposto na rota auxiliar
  `GET /api/system/errors`.
- Toda permissão granular é registrada no `permissions.go` (`Catalogo()`) e
  exposta no endpoint de permissões do usuário.
- Todo evento de auditoria é catalogado no `events.go` do subdomínio (ação
  estável + descrição PT-BR + campos do payload), validado pelo `auditar()`
  e exposto na rota auxiliar `GET /api/system/eventos`.
- Todo retorno de erro do service é OBSERVADO (evolução errobserve): o
  observador do subdomínio — severidades no `singleton.go`, códigos vindos
  do próprio catálogo de erros — vira evento estruturado para os sinks
  (slog sempre ativo, ClickHouse, alerta agregado) sem nunca mudar a
  resposta; o vocabulário de erros + o namespace reservado `sistema.*`
  aparecem em `GET /api/system/eventos` e na CLI `workspace-api errors`.
- As trilhas gravadas (auditoria, acesso, erros) são consultáveis via
  aplicação `logs` — `GET /api/application/identidade/logs/{auditoria,
  acesso,erros}` com recorte plataforma/organization/workspace imposto pelo
  ctx; ClickHouse ausente = 503 padronizado.
- O front-end consome os três como mapping — sem hardcode de código de erro,
  regra de acesso nem nome de evento.

## Regra de trabalho (loop de execução)

1. Abrir `agents/README.md` e seguir o protocolo.
2. **Uma issue por vez**, na ordem das fases; nunca pular fase.
3. Antes de codar, reler `agents/01`, `agents/04`, `agents/05` e o
   `AGENTS.md` da pasta.
4. Fechar a issue só com build/vet/test verdes e checklist do `agents/05`
   cumprido. Nunca deixar o build quebrado.

## Idioma

Comentários, docs e mensagens de erro em **PT-BR**; tipos e APIs técnicas em
inglês; vocabulário de negócio (campos, métodos de comportamento, sentinelas,
permissões, helpers de DTO) em **PT-BR**, conforme os templates do
`agents/05`.
