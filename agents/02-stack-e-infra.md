# 02 — Stack Técnica e Infraestrutura

## Stack Go (libs obrigatórias)

| Finalidade | Lib | Observação |
|---|---|---|
| HTTP router | `github.com/gin-gonic/gin` | Padrão do projeto |
| CORS | `github.com/gin-contrib/cors` | `AllowOriginFunc` por sufixo — doc 03 |
| Config | `github.com/spf13/viper` | `configs.json` (modelo abaixo) |
| CLI | `github.com/spf13/cobra` | `serve`, `migrate`, `seed` |
| PostgreSQL | `gorm.io/gorm` + driver `github.com/jackc/pgx/v5` (`stdlib`) | Com pool configurado |
| JWT | `github.com/golang-jwt/jwt/v5` | Access + refresh |
| UUID | `github.com/google/uuid` | PKs `uuid` em todas as tabelas |
| Validação | `github.com/go-playground/validator/v10` | Via binding do Gin; tags custom no `pkg/validator` |
| Swagger | `github.com/swaggo/swag`, `github.com/swaggo/gin-swagger`, `github.com/swaggo/files` | UI em `/doc/index.html` |
| Migrations | `github.com/golang-migrate/migrate/v4` | SQL puro em `db/migrations` |
| Testes | `github.com/stretchr/testify` | Table-driven |
| Senha/hash | `golang.org/x/crypto` | bcrypt para credenciais |
| Testes de integração | `github.com/testcontainers/testcontainers-go` | Banco efêmero do teste `up → down → up` |

Sem Redis, ClickHouse ou errobserve no núcleo — evoluções futuras (fim deste doc), cada uma com issue própria.

## Configuração (`configs.json` / `configs_example.json`)

```json
{
  "app": {
    "name": "workspace-api",
    "env": "dev",
    "version": "0.1.0",
    "base_domain": "localhost"
  },
  "server": {
    "http": {
      "port": 8080,
      "read_timeout_sec": 15,
      "write_timeout_sec": 30,
      "idle_timeout_sec": 60,
      "shutdown_timeout_sec": 10,
      "trusted_proxy": [],
      "cors": { "allowed_origins": [] }
    }
  },
  "security": {
    "jwt_secret": "trocar-em-producao",
    "jwt_ttl_min": 60,
    "jwt_refresh_ttl_hours": 168
  },
  "databases": {
    "postgres": {
      "host": "localhost",
      "port": 5432,
      "user": "workspace",
      "pass": "workspace",
      "name": "workspace",
      "ssl_mode": "disable",
      "pool": {
        "max_open_conns": 25,
        "max_idle_conns": 10,
        "conn_max_lifetime_min": 5,
        "conn_max_idle_time_min": 2
      }
    },
    "migrations": {
      "path": "db/migrations",
      "auto_run": true,
      "lock_timeout_sec": 5,
      "statement_timeout_min": 10
    }
  }
}
```

- `configs_example.json` é o modelo **versionado**; `configs.json` é local e ignorado pelo git.
- `config.Init(path)` no boot; `config.Use()` / `config.MustUse()` depois. Erro de config é **fatal**, com mensagem acionável (qual chave faltou, qual valor é inválido).
- `app.base_domain` é o domínio-base da plataforma — dele derivam a resolução de workspace pelo Host e o CORS (doc 03).

## PostgreSQL (transacional) — `internal/infra/database/postgres`

- **`Connect(cfg) (*gorm.DB, error)` puro**: testável, sem estado, sem variável de pacote.
- **Singleton do processo** com `sync.Once`: `InitPostgres()`, `GetDB()`, `Close()` — mesmo desenho do `config`.
- Postgres é **fatal**: banco fora do ar no boot derruba o processo com log claro.
- Pool lido de `databases.postgres.pool` (defaults entre parênteses): `SetMaxOpenConns` (25), `SetMaxIdleConns` (10), `SetConnMaxLifetime` (5min), `SetConnMaxIdleTime` (2min).
- DSN com `TimeZone=UTC`; **todos os timestamps em UTC** (`created_at`, `updated_at`, `deleted_at`).
- GORM com logger em nível `warn`. **Nunca `AutoMigrate`** — schema só por migration (seção abaixo).

## Convenção de tabelas

- Nome: `{dominio}_{subdominio}_{entidade}`, snake_case (hoje `{dominio}` = `identidade`) — ex.: `identidade_workspace_workspace`, `identidade_user_atribuicao`.
- PK `uuid` (uuid v4 gerado no app ou `gen_random_uuid()`).
- Colunas obrigatórias: `uuid`, `organization_uuid`, `workspace_uuid` (nas tabelas de negócio), `created_at`, `updated_at`, `deleted_at` (remoção lógica).
- **Índice de escopo**: toda tabela de negócio nasce com índice cuja RAIZ é `(organization_uuid, workspace_uuid)`. As tabelas acima do workspace usam a raiz que couber — `(organization_uuid)` em `identidade_workspace_workspace`, nenhuma em `identidade_organization_organization` (raiz da hierarquia) — sempre com o motivo escrito no `AGENTS.md` do pacote. Tabela sem nenhuma coluna de escopo precisa de motivo documentado — é o caso das **globais da plataforma** `identidade_user_papel` e `identidade_user_papel_permissao` (papéis seed globais).
- Único em tabela com `deleted_at` é índice único **parcial** (`WHERE deleted_at IS NULL`) — senão a linha removida reserva a chave para sempre e a recriação responde 409 apontando para um registro invisível. **Exceção documentada**: `slug` de workspace e `dominio` custom de organization usam índice único **TOTAL** — o valor removido **não se libera**, para evitar takeover de endereço/domínio por outro tenant (o Postgres aceita múltiplos NULLs, então o único total funciona com `dominio` opcional).

## Migrations — especificação completa

Ferramenta: `golang-migrate/migrate/v4`, SQL puro, tabela de controle `schema_migrations`. Runner em `internal/infra/database/migrations`, superfície na CLI `workspace-api migrate`.

### Arquivos

- Par obrigatório: `db/migrations/NNNN_{dominio}_{subdominio}_{descricao}.up.sql` + `.down.sql` (hoje `{dominio}` = `identidade`).
- `NNNN` sequencial a partir de `0001`, **sem buracos** — o `validate` reprova buraco e número duplicado.
- Um par por tabela ou grupo pequeno de tabelas do subdomínio.
- Cabeçalho comentado em todo arquivo: o que cria/altera e, nos downs com dado, o que é ou não reversível.

### Down obrigatório e testado

- Todo `up` nasce com o `down` **no mesmo commit**.
- `TestMigrationsSobemEDescem` aplica **`up → down → up`** em banco efêmero (docker/testcontainers) e **pula sozinho** (`t.Skip`) quando não há docker disponível — o `go test ./...` continua verde sem docker, mas com docker a migration cujo down não desfaz o up reprova a suíte.
- A **CI do template instala arch-go e docker**: o `t.Skip` dos testes é para o **dev local** — na CI os gates rodam de verdade.

### Seeds

Seeds rodam via **`workspace-api seed`**, são **idempotentes** e **nunca automáticos no boot** — subir o processo nunca grava dado de negócio sozinho.

O **provisionamento inicial** (R3) é opcional e também só via CLI: com `--super-admin-email`/`--super-admin-senha` (+ `--workspace-slug`, padrão `principal`), o seed cria o **primeiro super_admin** e o **workspace inicial** na organization raiz — idempotente, passando pelas regras dos subdomínios (VOs de slug/e-mail/senha, bcrypt, atribuição validada). Nunca toma slug de outra tenant; sem as flags, o seed continua sendo só papéis + organization raiz.

### Validate sem conexão

- `migrate validate` roda **sem abrir conexão**: confere par up/down, sequência sem buracos e SQL não vazio. É o gate rápido de CI/local e roda com o banco fora do ar.

### Aplicação automática no boot

- O boot roda `migrate up` antes de abrir o HTTP quando `databases.migrations.auto_run=true`.
- O **advisory lock do Postgres** (`pg_advisory_lock`) impede réplicas de migrar juntas; quem perde o lock espera e reconfere a versão antes de prosseguir.
- Rollback é **manual e explícito** via CLI (`migrate down N`) — nunca automático.

### CLI

`workspace-api migrate <comando>`:

| Comando | Ação |
|---|---|
| `up` | aplica todas as pendentes |
| `down N` | reverte as últimas N |
| `goto V` | sobe/desce até a versão V |
| `force V` | marca a versão manualmente (recuperação de estado "dirty") |
| `status` | versão atual + pendentes |
| `validate` | confere pares/sequência/SQL não vazio — sem conexão |
| `create {descricao}` | gera o par `NNNN_{descricao}.{up,down}.sql` com o número seguinte; a descrição já deve vir no padrão `{dominio}_{subdominio}_{desc}` (o comando valida) |

### Regras invioláveis

1. **Transacional por padrão**: cada arquivo roda numa transação (o DDL do Postgres é transacional); falha = rollback automático, sem meio-termo. Exceção única: `CREATE INDEX CONCURRENTLY` (não aceita transação) — isolado num arquivo próprio, um comando só, **marcado com `-- manual` no cabeçalho**: o `auto_run` do boot **ignora** arquivos manuais (segue com log de alerta), o `status` os lista como **pendente-manual** e a execução é manual em produção.
2. **Migration aplicada nunca é editada.** Correção = migration nova (fix forward).
3. **Expand-and-contract** para mudança destrutiva (renomear/dropar coluna, mudar tipo): *expand* cria a coluna nova + dual-write/backfill numa release; *contract* remove a antiga numa migration posterior. Proibido `DROP COLUMN`, `RENAME` ou `NOT NULL` sem default na mesma release do código que depende deles.
4. **Compatibilidade retroativa**: migration N funciona com o código N-1 em produção (deploy e migração não são atômicos).
5. **Timeouts curtos** na conexão de migração: `lock_timeout` (5s) e `statement_timeout` (10min) — falha rápida em vez de lock em produção.
6. Toda tabela de negócio criada aqui **nasce com o índice de escopo** `(organization_uuid, workspace_uuid)` (ver "Convenção de tabelas").
7. Nunca `GORM AutoMigrate` — schema só por migration.

## Ferramentas de mapeamento do sistema

Obrigatórias no projeto — servem para qualquer dev/agente entender e auditar o template:

1. **Swagger (swaggo)** — mapa vivo de todas as rotas. Toda handler TEM anotação. `swag init -g main.go -o docs` a cada rota nova/alterada; `docs/` é gerado **e versionado**; UI em `/doc/index.html`.
2. **arch-go** — regras de camada executáveis (`arch-go.yml`), compliance + coverage 100, rodando dentro do `go test ./...` (detalhes no doc 01).
3. **Go Architect — Dependency Graph** — conferência **visual** do grafo de pacotes ao fechar cada fase: [docs](https://go-architect.github.io/docs/analysis-tools/dependency-graph/). Verifica que o fluxo de camadas está valendo e flagra dependência inesperada.
4. **Comentários de pacote (`go doc`)** — todo pacote começa com comentário de pacote: propósito, entidades principais, dependências.

## Evoluções futuras (NÃO implementar antes da hora)

Cada uma tem **issue própria no GitHub** (label `evolucao`) e só entra no loop depois do F6 fechar (doc 06):

| Evolução | Para quê | Desenho acordado |
|---|---|---|
| **Redis** | Cache de permissões e de workspace por slug, locks de concorrência, denylist de JWT | Cliente **degradável**: `Connect` nunca erra — devolve cliente **nulo** com log `[DEGRADADO]` e o consumidor é obrigado a tratar a ausência. A denylist entra por interface declarada no `infra/jwt`, ligada no bootstrap, e é **só cache da revogação persistida** no Postgres. Cache com **TTL curto obrigatório** + **invalidação ativa** em inativação de organization/workspace e troca de `dominio`; a invariante "filho nunca mais vivo que o pai" ganha teste na evolução. |
| **ClickHouse** | Logs assíncronos (auditoria, acesso, erro) fora do caminho síncrono do request | Writer em lote (flush por tamanho/intervalo); log nunca entra no caminho síncrono; com o banco fora, o lote cai no **stdout** em vez de sumir — nunca bloqueia nem derruba a API. |
| **errobserve** | Observador de erros por subdomínio: todo erro vira evento estruturado consultável | Uma linha por subdomínio no `singleton.go`; sinks plugáveis (slog sempre ativo, ClickHouse quando existir); o catálogo de erros do `errors.go` (doc 05) é a fonte dos códigos. |

Enquanto não existem: auditoria sai por **log estruturado** (slog), permissões consultam o banco direto (sem cache), refresh tokens operam sem denylist distribuída.
