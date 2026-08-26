-- Migration 0010_todolist_tarefa_create (up).
-- Cria todolist_tarefa_tarefa: o PRIMEIRO MÓDULO do monolito — caso de teste
-- ponta a ponta do licensing (F10). Tabela de negócio comum: vive DENTRO do
-- workspace, escopo completo padrão (organization + workspace,
-- orgctx.Scope fail-closed); o acesso ao módulo em si é garantido pelo
-- RequireAplicacao("todolist") da cadeia, não por coluna própria.
CREATE TABLE todolist_tarefa_tarefa (
    uuid              uuid PRIMARY KEY,
    organization_uuid uuid NOT NULL REFERENCES identidade_organization_organization (uuid),
    workspace_uuid    uuid NOT NULL REFERENCES identidade_workspace_workspace (uuid),
    titulo            text NOT NULL,
    descricao         text,
    concluida         boolean NOT NULL DEFAULT false,
    created_at        timestamptz NOT NULL DEFAULT timezone('utc', now()),
    updated_at        timestamptz NOT NULL DEFAULT timezone('utc', now()),
    deleted_at        timestamptz
);

-- Índice de escopo padrão: raiz (organization_uuid, workspace_uuid).
CREATE INDEX idx_tarefa_escopo
    ON todolist_tarefa_tarefa (organization_uuid, workspace_uuid);

-- Listagem do módulo ordena por criação dentro do par resolvido.
CREATE INDEX idx_tarefa_listagem
    ON todolist_tarefa_tarefa (organization_uuid, workspace_uuid, created_at DESC)
    WHERE deleted_at IS NULL;
