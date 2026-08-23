# workspace-api

Template de API gerenciadora em **Go** com hierarquia **organization →
workspace → user**, arquitetura por domínio (DDD tático) e padrão singleton.
Pensado para ser clonado como ponto de partida de SaaS multi-tenant com
white-label para parceiros.

> **Status:** template completo e funcional — fundação, autenticação
> granular, os três subdomínios da hierarquia, aplicação de auth e catálogos
> do sistema implementados e cobertos por testes de unidade, integração
> (Postgres efêmero) e concorrência (`go test -race`). Gates de arquitetura
> executáveis (arch-go 100/100 + grafo de dependências).

## O modelo

```
Organization ── tem N ──▶ Workspace          (slug DNS: {slug}.{base_domain})
     │                         ▲
     │                         │ atribuição (papel por workspace)
     └── tem N ──▶ User ───────┘
```

- **Organization**: dono do contrato. Pode registrar um **domínio custom**
  (white-label): `*.{dominio-da-org}` resolve os workspaces dela, com
  validação de pertencimento — workspace alheio não resolve no domínio do
  parceiro.
- **Workspace**: unidade de tenancy, identificado por slug único global
  (`{slug}.{base_domain}`); slug removido não se libera (anti-takeover).
- **User**: pertence à organization; recebe **papéis por workspace** (RBAC
  granular `dominio:subdominio:acao`). Login indistinguível ("não existe" =
  "senha errada", inclusive por timing), refresh token persistido com
  revogação por `jti`.

## Stack

gin · Postgres (gorm + pgx) · golang-jwt · golang-migrate · cobra/viper ·
swaggo (Swagger versionado em `docs/`) · testify · arch-go. Evoluções:
Redis implementado (issue
[#8](https://github.com/gothout/workspace-api/issues/8) — cache, denylist e
lockout de login, degradável); ClickHouse (logs assíncronos) e errobserve
(observador de erros) planejadas.

## Como subir

Pré-requisitos: Go 1.25+, Postgres 14+ acessível e Docker (para a suíte de
testes de integração).

```bash
# 0. (opcional) Infra de dev com Docker Compose — Postgres + Redis + ClickHouse:
docker compose up -d

# 1. Config local (configs.json é ignorado pelo git; o example é o contrato)
cp configs_example.json configs.json
#    ajuste databases.postgres.* e security.jwt_secret para o seu ambiente
#    (jwt_secret: mínimo de 32 bytes — HS256 exige chave de 256 bits)

# 2. Rodar — as migrations sobem SOZINHAS no boot (advisory lock; rollback
#    nunca é automático). O seed NUNCA é automático.
go run . serve --config configs.json

# 3. Dados mínimos (papéis globais + organization raiz), idempotente:
go run . migrate up --config configs.json   # se quiser aplicar fora do boot
go run . seed --config configs.json
```

A API sobe em `http://localhost:8080`:

| Rota | Para quê |
|---|---|
| `GET /api/status` | sonda de saúde (banco incluído) |
| `/doc/index.html` | Swagger UI do contrato versionado em `docs/` |
| `POST /api/application/identidade/auth/login` | autenticação |

### Redis (opcional — degradável)

O template sobe **sem Redis** exatamente igual (log `[DEGRADADO]` no boot):
sem cache distribuído, sem denylist e **sem lockout de login** — nada quebra,
só fica mais caro/aberto. Para ligar:

1. Suba o serviço (`docker compose up -d redis`, ou o seu Redis).
2. Em `configs.json`: `"databases.redis.enabled": true` (+ host/porta/pass).
3. Opcionalmente ajuste `cache.*`: TTLs dos caches (`ttl_resolucao_seg`,
   `ttl_permissoes_seg`) e a política do lockout (`login_lockout`).

O que ele adiciona: cache da resolução `{slug}` → workspace
(`workspace:slug:*`), cache das permissões efetivas por usuário×workspace
(`perm:*`) com invalidação ativa nas escritas de atribuição, denylist do JWT
(`jwt:deny:*` — só cache da revogação persistida no Postgres) e
rate-limit/lockout de login por e-mail+IP (`lock:*`; 429 padronizado após o
teto de falhas). Prefixos completos documentados em
`internal/infra/redis/AGENTS.md`.

### ClickHouse — trilhas de log assíncronas (opcional — degradável)

O template sobe **sem ClickHouse** exatamente igual (log `[DEGRADADO]` no
boot): auditoria e acesso saem pelo **stdout** no mesmo formato. Para ligar:

1. Suba o serviço (`docker compose up -d clickhouse`, ou o seu servidor) e
   aplique o DDL versionado (manual, idempotente):

   ```bash
   docker compose exec -T clickhouse clickhouse-client --multiquery < db/logs/0001_log_acesso.sql
   docker compose exec -T clickhouse clickhouse-client --multiquery < db/logs/0002_log_auditoria.sql
   ```

2. Em `configs.json`: `"databases.clickhouse.enabled": true` (+ host/porta/
   user/pass/database; porta NATIVA 9000).
3. Opcionalmente ajuste `logs.*`: tamanho do lote, janela de flush, limite da
   fila e timeout de drain no shutdown.

O que ele adiciona: a trilha de **acesso** HTTP (um evento por requisição,
emitido pelo middleware global com o `ray_trace`) e a trilha de **auditoria**
das escritas dos subdomínios — ambas gravadas em lote FORA do caminho síncrono
do request. Fila cheia descarta e conta (a API nunca trava); shutdown drena o
que ficou pendente. Tabelas consultáveis em `workspace_logs.log_acesso` e
`workspace_logs.log_auditoria`.

### Provisionamento inicial (primeiro super_admin + workspace)

O seed básico cria papéis e a organization raiz — mas **nenhum usuário**.
Sem o provisionamento opcional abaixo, o template não tem quem entre nele
(chicken-and-egg): o primeiro `super_admin` e o workspace inicial são criados
SÓ via CLI, **nunca no boot**, e são idempotentes como o resto do seed
(reconhecem o que já existe e não alteram nada):

```bash
go run . seed --config configs.json \
  --super-admin-email admin@minhaempresa.com \
  --super-admin-senha 'troque-esta-senha' \
  --workspace-slug principal        # padrão: principal
```

- **Nunca automático**: sem as flags/env de provisionamento, o seed só semeia
  papéis + organization raiz (comportamento de sempre).
- **Regras de negócio intactas**: e-mail/slug/senha passam pelos VOs e
  services dos subdomínios — slug reservado/em uso (o provisionamento NUNCA
  toma endereço de outra tenant), política de senha (8–72 bytes), bcrypt e a
  atribuição `super_admin` validada pelo tripé usuário × workspace × papel.
- **Env no lugar das flags** (preferível em produção — senha fora da linha de
  comando): `WORKSPACE_API_SEED_SUPER_ADMIN_EMAIL`,
  `WORKSPACE_API_SEED_SUPER_ADMIN_SENHA`, `WORKSPACE_API_SEED_WORKSPACE_SLUG`.
- Nome do usuário: "Administrador da Plataforma" (renomeável pela API);
  nome do workspace inicial = próprio slug.
- Depois do provisionamento, o login já funciona ponta a ponta:
  `POST /api/application/identidade/auth/login` com Host
  `principal.{base_domain}` (em dev: `principal.localhost:8080`).

### CLI completa de migrations

```bash
go run . migrate up        # aplica pendentes
go run . migrate down 1    # reverte as últimas N (posicional, manual)
go run . migrate status    # estado de cada migration
go run . migrate validate  # confere pares up/down SEM conexão (gate de CI)
go run . migrate create identidade_workspace_descricao  # novo par up/down
```

### Deploy em servidor

Scripts prontos em `scripts/ops/` (instala binário/usuário/unit systemd,
start/stop/logs):

```bash
sudo ./scripts/ops/install-service.sh ./workspace-api
sudo nano /etc/workspace-api/configs.json   # config antes de subir
sudo ./scripts/ops/start.sh && ./scripts/ops/logs.sh
```

Sem systemd os scripts caem para `nohup` + pidfile automaticamente. O unit
roda como usuário sem privilégios, com `ProtectSystem=strict`.

## Como criar um subdomínio novo

Exemplo canônico: `internal/{dominio}/domain/workspace`. O caminho determina
o nome público de tudo:

| Origem | Derivação | Exemplo (`identidade` + `workspace`) |
|---|---|---|
| Tabela | `{dominio}_{subdominio}_{entidade}` | `identidade_workspace_workspace` |
| Migration | `NNNN_{dominio}_{subdominio}_{desc}.{up,down}.sql` | `0005_identidade_workspace_create` |
| Rota | `/api/domain/{dominio}/{recurso-plural}` | `/api/domain/identidade/workspaces` |
| Permissão | `{dominio}:{subdominio}:{acao}` | `identidade:workspace:editar` |
| Código de erro | `{dominio}.{subdominio}.{nome}` | `identidade.workspace.slug_em_uso` |

Passo a passo (templates completos em `agents/05`):

1. **Modelo exposto** — `internal/{dominio}/model/{subdominio}`: entidades,
   VOs com construtor validador (`ParseSlug`, `ParseEmail`...), inputs,
   sentinelas de invariante. Pacote-folha: importa só stdlib/libs +
   `internal/pkg`.
2. **Subdomínio** — `internal/{dominio}/domain/{subdominio}` com os
   **8 arquivos + `permissions.go`**: DTOs de entrada/saída, `errors.go`
   (sentinelas + catálogo registrado no `rest_err`), `repository.go` (toda
   query passa por `orgctx.Scope`/`ScopeOrganization` — fail-closed),
   `service.go` (toda a regra + auditoria em toda escrita), `controller.go`
   (`Routes()` declarando a cadeia de auth **rota a rota**), `singleton.go`
   (`New`/`Use`/`MustUse`).
3. **Migration** — par `up/down` em `db/migrations/`; todo `up` tem `down`
   no mesmo commit, exercitado por `up → down → up` em banco efêmero.
4. **Ligação** — adaptadores no `cmd/bootstrap` (é o único lugar onde
   pacotes se encontram) e registro das rotas via `Use()` em
   `cmd/server/routes`.
5. **Contrato** — annotations do swag no controller + `swag init -g main.go
   -o docs`; permissões novas entram no seed dos papéis; erros novos aparecem
   sozinhos em `GET /api/system/errors`.
6. **Gate** — `go build/vet/test ./...` verdes (+ `-race` se tocar invariante
   disputada) e arch-go 100/100 (o teste pula se a ferramenta não estiver
   instalada; com ela instalada, reprovação é gate).

O caso de uso que **cruza 2+ subdomínios** não mora em nenhum deles: sobe
para `internal/{dominio}/application/{nome}`, sem model/repository próprios —
os vizinhos entram por interfaces estreitas do `contratos.go`, ligadas no
bootstrap (exemplos: `auth`, `catalogo`).

## Como o front consome os catálogos

Zero hardcode de erro ou regra de acesso no front-end — os dois catálogos
são contratos:

```bash
# Login
POST /api/application/identidade/auth/login
{"email": "...", "senha": "..."}          → access_token + refresh_token

# O QUE o usuário pode fazer: árvore dominio→subdominio→ações filtrada pelo
# conjunto efetivo dele (mesmo matcher do RequirePermission)
GET /api/application/identidade/catalogo/permissoes/minhas

# O QUE PODE dar errado: mapa completo code estável + mensagem PT-BR + status
GET /api/system/errors
```

- Permissão nova aparece na árvore quando entra no `Catalogo()` do
  subdomínio; erro novo aparece no mapa quando entra no `errors.go` — o front
  lê os dois endpoints e nunca decora código.
- Toda rota de negócio exige a cadeia `Authorization: Bearer ...` +
  resolução de workspace pelo Host (`{slug}.{base_domain}` ou
  `{slug}.{dominio-custom}` white-label) + permissão exata rota a rota;
  cadeia ausente/incompleta = **403 fechada**, nunca aberta.

## Arquitetura em uma página

- **Camadas** (`internal/`): `pkg` (folha, utilitários) ← `infra` (Postgres,
  JWT — singletons com `Connect` puro) ← `{dominio}/model` (modelos expostos,
  folha importável por todos) ← `{dominio}/domain` (subdomínios) ←
  `{dominio}/application` ← `cmd` (composição). As regras de dependência são
  **executáveis**: arch-go (`arch-go.yml`, compliance+coverage 100 dentro do
  `go test ./...`) e o grafo de dependências conferido por teste
  (`grafo_dependencias_test.go`).
- **Singleton**: todo pacote de infra e todo subdomínio segue `New(deps...)` →
  `Use()` → `MustUse()` com `sync.Once`. Postgres/JWT são fatais; futuras
  dependências (Redis, ClickHouse) degradam com log `[DEGRADADO]`.
- **Escopo fail-closed**: toda query de tabela de negócio passa por
  `orgctx.Scope`/`ScopeOrganization` — sem escopo, a query falha.
- **Autorização granular rota a rota**: cada rota declara sua permissão exata
  via `RequirePermission`; cadeia não inicializada = 403, nunca aberta.
- **Migrations**: SQL puro em pares `up`/`down`; `up` automático no boot
  (advisory lock), `down` testado (`up → down → up` em banco efêmero).
- **Testes**: unidade com dublês (nunca singleton), integração real com
  Postgres efêmero (pula sem Docker), concorrência nas invariantes
  disputadas com pool aquecido (`go test -race ./...`), cobertura Swagger nos
  dois sentidos e grafo de dependências — todos parte do `go test ./...`.

## Estrutura

```
agents/          especificação para quem codifica (ler README.md de lá primeiro)
cmd/             cli, bootstrap (DI) e server
internal/
  pkg/           config, rest_err, pagination, orgctx, validator (folha)
  infra/         database (postgres, migrations), jwt, redis (degradável)
  middleware/    cadeia de auth/autorização (fail-closed)
  identidade/    DOMÍNIO
    model/       modelos expostos: entidades, VOs, invariantes (folha)
    domain/      subdomínios: organization, workspace, user
    application/ aplicações: auth, catalogo
db/migrations/   SQL puro up/down
docs/            Swagger GERADO e versionado (swag init -g main.go -o docs)
```

Cada pasta tem seu `AGENTS.md` com regras específicas e definição de pronto.

## Fases

| Issue | Fase |
|---|---|
| [#1](https://github.com/gothout/workspace-api/issues/1) | F0 — Fundação (config, postgres, rest_err, migrations, server, arch-go) ✅ |
| [#2](https://github.com/gothout/workspace-api/issues/2) | F1 — Autenticação e autorização granular ✅ |
| [#3](https://github.com/gothout/workspace-api/issues/3) | F2 — Subdomínio organization (com domínio custom) ✅ |
| [#4](https://github.com/gothout/workspace-api/issues/4) | F3 — Subdomínio workspace (slug, white-label) ✅ |
| [#5](https://github.com/gothout/workspace-api/issues/5) | F4 — Subdomínio user + aplicação auth ✅ |
| [#6](https://github.com/gothout/workspace-api/issues/6) | F5 — Catálogos do sistema (permissões + mapa de erros) ✅ |
| [#7](https://github.com/gothout/workspace-api/issues/7) | F6 — Hardening do template ✅ |
| [#8](https://github.com/gothout/workspace-api/issues/8)–[#10](https://github.com/gothout/workspace-api/issues/10) | Evoluções futuras (Redis, ClickHouse, errobserve) |

## Convenções

- Docs, comentários e mensagens de erro em **PT-BR**; tipos e APIs técnicas
  em inglês, vocabulário de negócio em PT-BR (ver `agents/05`).
- Migration aplicada nunca é editada; mudança destrutiva é expand-and-contract.
- Termo de negócio novo entra no glossário do `agents/00` no mesmo commit.

## Como contribuir (loop de execução)

1. Abrir `agents/README.md` e seguir o protocolo.
2. Pegar a issue aberta mais antiga — uma por vez, nunca pular fase.
3. Antes de codar, reler `agents/01`, `agents/04`, `agents/05` e o
   `AGENTS.md` das pastas tocadas.
4. Fechar com `go build ./... && go vet ./... && go test ./...` verdes
   (`-race` se tocar invariante disputada) e registrar no log do `agents/06`.

## Ralph loop (OpenCode)

A automação orientada por PRD está em `scripts/ralph/`:

- `scripts/ralph/AGENT.md` — instruções para agentes de IA.
- `scripts/ralph/prd.json` — backlog de user stories por fase (issues #1 a #7).
- `scripts/ralph/run-loop.sh` — orquestra o loop de implementação.

Para rodar com **OpenCode**:

```bash
opencode run scripts/ralph/run-loop.sh
```

O agente lê a fase atual do `prd.json`, implementa **uma story por vez**, roda
`go build/vet/test`, commita e atualiza o progresso em
`scripts/ralph/progress.txt`.

Branch de teste inicial: `ox-alpha/code`.
