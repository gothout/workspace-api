-- Migration 0004_identidade_organization_apikey_create (down).
-- Reverte integralmente o up: a tabela de chaves de API é descartada.
DROP TABLE IF EXISTS identidade_organization_apikey;
