-- 0001 — trilha de ACESSO HTTP no ClickHouse (evolução #9, E2).
--
-- Um evento por requisição, emitido pelo middleware global (primeiro da
-- cadeia). `rota` é o padrão casado pelo gin (agrupável); `path` é o caminho
-- cru; `ray_trace` correlaciona com rest_err e com a auditoria.
--
-- Aplicação MANUAL e idempotente (ver db/logs/AGENTS.md):
--   docker compose exec clickhouse clickhouse-client --multiquery < db/logs/0001_log_acesso.sql

CREATE DATABASE IF NOT EXISTS workspace_logs;

CREATE TABLE IF NOT EXISTS workspace_logs.log_acesso (
    instante   DateTime64(3, 'UTC'),
    metodo     String,
    path       String,
    rota       String,
    status     Int32,
    duracao_ms Int64,
    ip         String,
    user_agent String,
    ray_trace  String
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(instante)
ORDER BY (instante, ray_trace);
