-- Migration 0004_identidade_organization_apikey_create (up).
-- Cria identidade_organization_apikey: chaves de API da organization
-- (integrações server-to-server, header X-Api-Key).
-- O banco guarda SÓ o SHA-256 da chave (`key_hash`) — o token em claro é
-- exibido uma única vez na criação e nunca persistido.
-- Escopo por organization_uuid (tabela ACIMA do workspace — queries passam
-- por orgctx.ScopeOrganization, fail-closed).
CREATE TABLE identidade_organization_apikey (
    uuid                  uuid PRIMARY KEY,
    organization_uuid     uuid NOT NULL REFERENCES identidade_organization_organization (uuid),
    nome                  text NOT NULL,
    key_hash              text NOT NULL,
    escopo_organization   boolean NOT NULL DEFAULT false,
    workspaces_permitidos jsonb NOT NULL DEFAULT '[]',
    permissoes            jsonb NOT NULL DEFAULT '[]',
    expires_at            timestamptz,
    status                text NOT NULL DEFAULT 'ativo',
    created_at            timestamptz NOT NULL DEFAULT timezone('utc', now()),
    updated_at            timestamptz NOT NULL DEFAULT timezone('utc', now()),
    deleted_at            timestamptz
);

-- Índice de escopo: raiz (organization_uuid) — tabela acima do workspace.
CREATE INDEX idx_apikey_escopo
    ON identidade_organization_apikey (organization_uuid);

-- Resolvedor do middleware busca pelo hash — hash removido (soft delete) não
-- revalida; a unicidade segue a convenção parcial (doc 02) porque colisão de
-- token aleatório é impraticável e a recriação não pode herdar o vínculo.
CREATE UNIQUE INDEX uq_apikey_key_hash
    ON identidade_organization_apikey (key_hash)
    WHERE deleted_at IS NULL;
