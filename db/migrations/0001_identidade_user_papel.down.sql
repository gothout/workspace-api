-- Migration 0001_identidade_user_papel (down).
-- Reverte integralmente o up: nenhuma reversibilidade parcial.
DROP TABLE IF EXISTS identidade_user_papel_permissao;
DROP TABLE IF EXISTS identidade_user_papel;
