-- Migration 0009_licensing_ativacao_create (up).
-- Cria licensing_ativacao_ativacao: a ATIVAÇÃO de um módulo num workspace —
-- aplicada pela organization DENTRO das licenças que ela possui. É a ponte
-- licença → uso: o acesso por `Application` consulta exatamente esta tabela
-- cruzada com a licença viva da organization. Escopo completo padrão do
-- template (organization + workspace, orgctx.Scope fail-closed).
CREATE TABLE licensing_ativacao_ativacao (
    uuid              uuid PRIMARY KEY,
    organization_uuid uuid NOT NULL REFERENCES identidade_organization_organization (uuid),
    workspace_uuid    uuid NOT NULL REFERENCES identidade_workspace_workspace (uuid),
    modulo_uuid       uuid NOT NULL REFERENCES licensing_modulo_modulo (uuid),
    ativado_por       uuid,
    created_at        timestamptz NOT NULL DEFAULT timezone('utc', now()),
    updated_at        timestamptz NOT NULL DEFAULT timezone('utc', now()),
    deleted_at        timestamptz
);

-- Índice de escopo padrão: raiz (organization_uuid, workspace_uuid).
CREATE INDEX idx_ativacao_escopo
    ON licensing_ativacao_ativacao (organization_uuid, workspace_uuid);

-- Uma ativação viva por par (workspace, módulo): índice único PARCIAL —
-- desativada (soft delete), o par se libera para reativação com histórico.
CREATE UNIQUE INDEX uq_ativacao_ws_modulo
    ON licensing_ativacao_ativacao (workspace_uuid, modulo_uuid)
    WHERE deleted_at IS NULL;

-- Resolução de acesso pergunta por workspace + vitalidade do módulo.
CREATE INDEX idx_ativacao_resolucao
    ON licensing_ativacao_ativacao (workspace_uuid)
    WHERE deleted_at IS NULL;
