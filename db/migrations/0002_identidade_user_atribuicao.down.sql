-- Migration 0002_identidade_user_atribuicao (down).
-- Reverte integralmente o up; os dados de atribuição são descartados.
DROP TABLE IF EXISTS identidade_user_atribuicao;
