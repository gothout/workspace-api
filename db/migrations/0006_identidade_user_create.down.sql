-- Migration 0006_identidade_user_create (down).
-- Reverte integralmente o up; tokens e usuários são descartados.
DROP TABLE IF EXISTS identidade_user_refresh_token;
DROP TABLE IF EXISTS identidade_user_user;
