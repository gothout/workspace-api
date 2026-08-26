-- Migration 0009_licensing_ativacao_create (down).
-- Reverte integralmente o up: a tabela de ativações por workspace e índices.
DROP TABLE IF EXISTS licensing_ativacao_ativacao;
