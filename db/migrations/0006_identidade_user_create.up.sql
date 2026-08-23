-- Migration 0006_identidade_user_create (up).
-- Cria as tabelas do subdomínio user que ainda não existiam:
--   identidade_user_user            — a pessoa; administração escopa por
--                                     organization (tabela ACIMA do workspace:
--                                     orgctx.ScopeOrganization, fail-closed);
--                                     e-mail único POR organization (índice único
--                                     PARCIAL — remoção lógica libera o valor,
--                                     diferente do slug/domínio que são endereço
--                                     público da plataforma);
--   identidade_user_refresh_token   — revogação persistida do refresh: uma linha
--                                     por jti (único TOTAL — a linha nunca sai;
--                                     logout marca revogado_em).
-- As tabelas de autorização (0001/0002: papel, papel_permissao, atribuicao)
-- já existem; os vínculos de atribuicao continuam por uuid (precedente da F3).
CREATE TABLE identidade_user_user (
    uuid              uuid PRIMARY KEY,
    organization_uuid uuid NOT NULL REFERENCES identidade_organization_organization (uuid),
    nome              text NOT NULL,
    email             text NOT NULL,
    senha_hash        text NOT NULL,
    status            text NOT NULL DEFAULT 'ativo',
    created_at        timestamptz NOT NULL DEFAULT timezone('utc', now()),
    updated_at        timestamptz NOT NULL DEFAULT timezone('utc', now()),
    deleted_at        timestamptz
);

-- Índice de escopo: raiz (organization_uuid) — tabela acima do workspace.
CREATE INDEX idx_user_escopo
    ON identidade_user_user (organization_uuid);

-- E-mail único POR organization enquanto o usuário existir (parcial).
CREATE UNIQUE INDEX uq_user_email_por_organization
    ON identidade_user_user (organization_uuid, email)
    WHERE deleted_at IS NULL;

-- Login busca por e-mail dentro da organization resolvida.
CREATE INDEX idx_user_email_ativo
    ON identidade_user_user (organization_uuid, email)
    WHERE status = 'ativo' AND deleted_at IS NULL;

CREATE TABLE identidade_user_refresh_token (
    uuid              uuid PRIMARY KEY,
    organization_uuid uuid NOT NULL,
    user_uuid         uuid NOT NULL REFERENCES identidade_user_user (uuid),
    jti               text NOT NULL,
    expira_em         timestamptz NOT NULL,
    revogado_em       timestamptz,
    created_at        timestamptz NOT NULL DEFAULT timezone('utc', now())
);

-- O jti é a chave da revogação persistida: único TOTAL (doc 03) — a linha é
-- imutável em existência (logout só carimba revogado_em), nunca é removida.
CREATE UNIQUE INDEX uq_refresh_jti
    ON identidade_user_refresh_token (jti);

-- Índice de escopo: raiz (organization_uuid).
CREATE INDEX idx_refresh_escopo
    ON identidade_user_refresh_token (organization_uuid);

-- Revogação em cascata ao inativar/remover usuário e sessões dele.
CREATE INDEX idx_refresh_por_usuario
    ON identidade_user_refresh_token (user_uuid)
    WHERE revogado_em IS NULL;
