-- Migration 0008_licensing_licenca_create (up).
-- Cria licensing_licenca_licenca: a LICENÇA de um módulo para uma
-- organization — concedida pelo super_admin (atribuir/remover). Binária:
-- linha viva = licenciada; remoção lógica = revogada. Tabela ACIMA do
-- workspace: escopo por organization_uuid (orgctx.ScopeOrganization,
-- fail-closed); a escrita cruza organizations e é exclusiva do super_admin.
CREATE TABLE licensing_licenca_licenca (
    uuid              uuid PRIMARY KEY,
    organization_uuid uuid NOT NULL REFERENCES identidade_organization_organization (uuid),
    modulo_uuid       uuid NOT NULL REFERENCES licensing_modulo_modulo (uuid),
    concedida_por     uuid,
    created_at        timestamptz NOT NULL DEFAULT timezone('utc', now()),
    updated_at        timestamptz NOT NULL DEFAULT timezone('utc', now()),
    deleted_at        timestamptz
);

-- Índice de escopo: raiz (organization_uuid) — tabela acima do workspace.
CREATE INDEX idx_licenca_escopo
    ON licensing_licenca_licenca (organization_uuid);

-- Uma licença viva por par (organization, módulo): índice único PARCIAL —
-- revogada (soft delete), o par se libera para concessão nova com histórico
-- preservado (convenção padrão do agents/02).
CREATE UNIQUE INDEX uq_licenca_org_modulo
    ON licensing_licenca_licenca (organization_uuid, modulo_uuid)
    WHERE deleted_at IS NULL;
