-- Migration 0007_licensing_modulo_create (up).
-- Cria licensing_modulo_modulo: catálogo GLOBAL das aplicações (módulos) que
-- o monolito oferece — cada linha é um "app" licenciável (ex.: todolist).
-- Tabela da plataforma, SEM escopo de tenancy: precede a organization
-- (mesma natureza de identidade_user_papel — exceção documentada no
-- AGENTS.md do domínio licensing). Escrita restrita ao super_admin
-- (licensing:modulo:*); leitura aberta aos papéis de administração.
CREATE TABLE licensing_modulo_modulo (
    uuid       uuid PRIMARY KEY,
    slug       text NOT NULL,
    nome       text NOT NULL,
    descricao  text,
    ativo      boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT timezone('utc', now()),
    updated_at timestamptz NOT NULL DEFAULT timezone('utc', now()),
    deleted_at timestamptz
);

-- EXCEÇÃO ao índice único parcial (agents/02): índice único TOTAL no slug —
-- o slug é o valor do header `Application` e o rótulo amarrado ao código do
-- monolito (RequireAplicacao("slug")); valor removido NÃO se libera, evitando
-- takeover de identidade de aplicação por registro novo.
CREATE UNIQUE INDEX uq_modulo_slug
    ON licensing_modulo_modulo (slug);

-- Resolução de acesso lista módulos ativos; removido/desativado sai na hora.
CREATE INDEX idx_modulo_ativo
    ON licensing_modulo_modulo (ativo)
    WHERE deleted_at IS NULL;
