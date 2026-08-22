-- Migration 0002_identidade_user_atribuicao (up).
-- Cria identidade_user_atribuicao: liga user × workspace × papel.
-- Escopo COMPLETO (organization_uuid, workspace_uuid) — tabela da vida dentro
-- do workspace; queries passam por orgctx.Scope (fail-closed).
-- FKs para identidade_workspace_workspace e identidade_user_user entram nas
-- fases F3/F4 (tabelas ainda inexistentes) — vínculo por uuid até lá.
CREATE TABLE identidade_user_atribuicao (
    uuid              uuid PRIMARY KEY,
    organization_uuid uuid NOT NULL,
    workspace_uuid    uuid NOT NULL,
    user_uuid         uuid NOT NULL,
    papel_uuid        uuid NOT NULL REFERENCES identidade_user_papel (uuid),
    created_at        timestamptz NOT NULL DEFAULT timezone('utc', now()),
    updated_at        timestamptz NOT NULL DEFAULT timezone('utc', now()),
    deleted_at        timestamptz
);

-- Índice de escopo: raiz (organization_uuid, workspace_uuid), convenção do doc 02.
CREATE INDEX idx_atribuicao_escopo
    ON identidade_user_atribuicao (organization_uuid, workspace_uuid);

-- Mesmo papel não se duplica para o mesmo par usuário×workspace enquanto ativo
-- (remoção lógica libera reatribuição — único parcial, doc 02).
CREATE UNIQUE INDEX uq_atribuicao_usuario_workspace_papel
    ON identidade_user_atribuicao (workspace_uuid, user_uuid, papel_uuid)
    WHERE deleted_at IS NULL;

-- Papéis efetivos do usuário em todos os workspaces (resolvedor do middleware).
CREATE INDEX idx_atribuicao_por_usuario
    ON identidade_user_atribuicao (user_uuid);

-- Suporte do admin_organization: papéis que o usuário exerce na organization.
CREATE INDEX idx_atribuicao_por_organization_usuario
    ON identidade_user_atribuicao (organization_uuid, user_uuid);
