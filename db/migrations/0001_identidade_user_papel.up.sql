-- Migration 0001_identidade_user_papel (up).
-- Cria as tabelas de autorização da plataforma:
--   identidade_user_papel            — os papéis seed globais (super_admin, admin_organization,
--                                      admin_workspace, usuario_workspace, somente_leitura);
--   identidade_user_papel_permissao  — permissões granulares de cada papel
--                                      (string "dominio:subdominio:acao", curingas só para admin).
-- EXCEÇÃO de escopo documentada (agents/03 e db/migrations/AGENTS.md): tabelas
-- GLOBAIS da plataforma — sem organization_uuid/workspace_uuid, logo sem índice
-- de escopo; a consulta delas pelo resolvedor provisório é por nome do papel.
CREATE TABLE identidade_user_papel (
    uuid       uuid PRIMARY KEY,
    nome       text NOT NULL UNIQUE,
    descricao  text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT timezone('utc', now()),
    updated_at timestamptz NOT NULL DEFAULT timezone('utc', now())
);

CREATE TABLE identidade_user_papel_permissao (
    papel_uuid uuid NOT NULL REFERENCES identidade_user_papel (uuid) ON DELETE CASCADE,
    permissao  text NOT NULL,
    PRIMARY KEY (papel_uuid, permissao)
);

-- Resolvedor pergunta "quais papéis liberam esta permissão?" sem varrer tudo.
CREATE INDEX idx_papel_permissao_permissao
    ON identidade_user_papel_permissao (permissao);
