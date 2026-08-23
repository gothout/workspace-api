# AGENTS.md — `db/logs`

DDL das tabelas de LOG no **ClickHouse** (trilhas assíncronas, evolução #9).
Não passa pelo runner de migrations do Postgres (`db/migrations` é do banco
transacional; ClickHouse não tem golang-migrate no template) — a aplicação é
**manual, versionada e idempotente**, na ordem numérica.

## Regras

- Nome: `NNNN_{tabela}.sql`, sequencial a partir de `0001`, sem buracos.
- Todo arquivo é **idempotente** (`CREATE DATABASE/TABLE IF NOT EXISTS`;
  ALTER usa `ADD COLUMN IF NOT EXISTS`) e auto-suficiente (cria o database
  se faltar) — rodar duas vezes é inofensivo.
- **DDL aplicado nunca é editado.** Correção = arquivo novo (fix forward),
  mesmo princípio de `db/migrations`.
- Tabelas: `workspace_logs.log_acesso` (middleware global, com colunas de
  tenancy desde o E5), `workspace_logs.log_auditoria` (escritas dos
  subdomínios) e `workspace_logs.log_erro` (erros observados — evolução
  errobserve: código estável, severidade, flag de desconhecido e causa).
  O nome do database vem de `databases.clickhouse.database`
  (default `workspace_logs`) — mudar o default aqui exige mudar lá junto.
- MergeTree com partição mensal e `ORDER BY (instante, ray_trace)`:
  consulta típica por janela de tempo correlacionada por requisição.

## Aplicação

```bash
# dev (docker-compose sobe o clickhouse):
docker compose exec -T clickhouse clickhouse-client --multiquery < db/logs/0001_log_acesso.sql
docker compose exec -T clickhouse clickhouse-client --multiquery < db/logs/0002_log_auditoria.sql
docker compose exec -T clickhouse clickhouse-client --multiquery < db/logs/0003_log_erros.sql
docker compose exec -T clickhouse clickhouse-client --multiquery < db/logs/0004_log_acesso_tenancy.sql
```

Em produção, os mesmos comandos contra o servidor gerenciado. O teste de
integração do `internal/infra/clickhouse` aplica estes arquivos no contêiner
efêmero antes de gravar/ler — DDL quebrado reprova a suíte quando há docker.

## Tenancy na trilha de acesso (0004, E5)

A trilha de acesso nasceu SEM colunas de escopo; o recorte da leitura de
logs (E5) exigiu `organization_uuid`, `workspace_uuid` e `user_uuid`. O
middleware global preenche DEPOIS da cadeia rodar (`c.Next()` retorna com o
ctx completo); requisições fora da cadeia (404, `/doc`) gravam vazio. O
gravador usa lista EXPLÍCITA de colunas no INSERT — a ordem física das
colunas pós-ALTER não pode ser dependência silenciosa.
