-- Migration 0005_identidade_workspace_create (down).
-- Reverte integralmente o up: a tabela do subdomínio workspace e seus
-- índices são descartados.
DROP TABLE IF EXISTS identidade_workspace_workspace;
