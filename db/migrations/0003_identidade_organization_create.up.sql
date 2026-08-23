-- Migration 0003_identidade_organization_create (up).
-- Cria identidade_organization_organization: raiz da hierarquia (dona do
-- contrato) e do domínio custom white-label.
-- EXCEÇÃO de escopo documentada (agents/03 e AGENTS.md do subdomínio): tabela
-- RAIZ — não tem organization_uuid para filtrar (ela define o escopo de todo o
-- resto); o acesso é controlado por permissão própria
-- (identidade:organization:*), restrita a super_admin/suporte auditado.
CREATE TABLE identidade_organization_organization (
    uuid       uuid PRIMARY KEY,
    nome       text NOT NULL,
    documento  text NOT NULL DEFAULT '',
    dominio    text,
    status     text NOT NULL DEFAULT 'ativo',
    created_at timestamptz NOT NULL DEFAULT timezone('utc', now()),
    updated_at timestamptz NOT NULL DEFAULT timezone('utc', now()),
    deleted_at timestamptz
);

-- EXCEÇÃO ao índice único parcial (agents/02): índice único TOTAL no dominio —
-- valor removido NÃO se libera (evita takeover de domínio por outro tenant);
-- o Postgres aceita múltiplos NULLs, então funciona com dominio opcional.
CREATE UNIQUE INDEX uq_organization_dominio
    ON identidade_organization_organization (dominio);

-- Provedor de domínios custom (resolução pelo Host e CORS) lista só os ativos.
CREATE INDEX idx_organization_dominio_ativo
    ON identidade_organization_organization (dominio)
    WHERE status = 'ativo' AND deleted_at IS NULL;
