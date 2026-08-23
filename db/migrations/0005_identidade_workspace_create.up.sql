-- Migration 0005_identidade_workspace_create (up).
-- Cria identidade_workspace_workspace: filho da organization, dono do
-- endereço público {slug}.{base_domain} (e {slug}.{dominio-custom} do
-- white-label). Tabela ACIMA do workspace que ela define: escopo por
-- organization_uuid — queries passam por orgctx.ScopeOrganization
-- (fail-closed); EXCEÇÃO documentada: BuscarPorSlug é GLOBAL porque a
-- resolução pelo Host acontece antes de existir escopo (agents/03 e
-- AGENTS.md do subdomínio).
CREATE TABLE identidade_workspace_workspace (
    uuid              uuid PRIMARY KEY,
    organization_uuid uuid NOT NULL REFERENCES identidade_organization_organization (uuid),
    nome              text NOT NULL,
    slug              text NOT NULL,
    status            text NOT NULL DEFAULT 'ativo',
    created_at        timestamptz NOT NULL DEFAULT timezone('utc', now()),
    updated_at        timestamptz NOT NULL DEFAULT timezone('utc', now()),
    deleted_at        timestamptz
);

-- Índice de escopo: raiz (organization_uuid) — tabela acima do workspace.
CREATE INDEX idx_workspace_escopo
    ON identidade_workspace_workspace (organization_uuid);

-- EXCEÇÃO ao índice único parcial (agents/02): índice único TOTAL no slug —
-- valor removido (soft delete) NÃO se libera, evitando takeover de endereço
-- por outro tenant; o slug é o endereço da plataforma inteira
-- ({slug}.{base_domain}), não de uma organization só.
CREATE UNIQUE INDEX uq_workspace_slug
    ON identidade_workspace_workspace (slug);

-- Resolução pelo Host lista/busca por slug com vitalidade — workspace
-- inativo/removido sai da resolução imediatamente.
CREATE INDEX idx_workspace_slug_ativo
    ON identidade_workspace_workspace (slug)
    WHERE status = 'ativo' AND deleted_at IS NULL;
