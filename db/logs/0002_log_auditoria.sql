-- 0002 — trilha de AUDITORIA das escritas no ClickHouse (evolução #9, E2).
--
-- Payload montado à mão pelos services (doc 04): identificadores e vocabulário
-- fechado, nunca texto livre e nunca segredo. `detalhes` é JSON com chaves
-- ordenadas (vazio quando o evento não tem detalhe extra).
--
-- Aplicação MANUAL e idempotente (ver db/logs/AGENTS.md):
--   docker compose exec clickhouse clickhouse-client --multiquery < db/logs/0002_log_auditoria.sql

CREATE DATABASE IF NOT EXISTS workspace_logs;

CREATE TABLE IF NOT EXISTS workspace_logs.log_auditoria (
    instante          DateTime64(3, 'UTC'),
    dominio           String,
    subdominio        String,
    acao              String,
    sucesso           UInt8,
    organization_uuid String,
    workspace_uuid    String,
    user_uuid         String,
    ray_trace         String,
    detalhes          String
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(instante)
ORDER BY (instante, ray_trace);
