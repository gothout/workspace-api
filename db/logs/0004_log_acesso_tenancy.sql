-- 0004 — tenancy na trilha de ACESSO (evolução E5, leitura de logs).
--
-- A trilha de acesso não tinha colunas de escopo: sem elas é impossível
-- recortar os logs de acesso por organization/workspace (o recorte do E5).
-- O middleware global preenche após a cadeia rodar — quando o ctx já tem a
-- identidade e o workspace resolvidos. Requisições fora da cadeia (404,
-- /doc) gravam vazio.
--
-- Aplicação MANUAL e idempotente (ver db/logs/AGENTS.md):
--   docker compose exec clickhouse clickhouse-client --multiquery < db/logs/0004_log_acesso_tenancy.sql

ALTER TABLE workspace_logs.log_acesso ADD COLUMN IF NOT EXISTS organization_uuid String DEFAULT '';
ALTER TABLE workspace_logs.log_acesso ADD COLUMN IF NOT EXISTS workspace_uuid String DEFAULT '';
ALTER TABLE workspace_logs.log_acesso ADD COLUMN IF NOT EXISTS user_uuid String DEFAULT '';
