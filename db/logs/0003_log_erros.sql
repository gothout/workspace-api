-- 0003 — trilha de ERROS observados no ClickHouse (evolução errobserve, E4).
--
-- Um evento por erro devolvido pelos services (errobserve.Observe): código
-- estável do catálogo do subdomínio, severidade declarada no singleton e a
-- causa como texto (para o log — nunca volta ao cliente). Sentinela fora do
-- catálogo grava codigo='desconhecido' com desconhecido=1 e severidade
-- critical.
--
-- Aplicação MANUAL e idempotente (ver db/logs/AGENTS.md):
--   docker compose exec clickhouse clickhouse-client --multiquery < db/logs/0003_log_erros.sql

CREATE DATABASE IF NOT EXISTS workspace_logs;

CREATE TABLE IF NOT EXISTS workspace_logs.log_erro (
    instante          DateTime64(3, 'UTC'),
    dominio           String,
    subdominio        String,
    codigo            String,
    mensagem          String,
    severidade        LowCardinality(String),
    desconhecido      UInt8,
    organization_uuid String,
    workspace_uuid    String,
    user_uuid         String,
    ray_trace         String,
    causa             String
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(instante)
ORDER BY (instante, ray_trace);
