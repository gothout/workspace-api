-- Migration 0003_identidade_organization_create (down).
-- Reverte integralmente o up: a tabela raiz e seus índices são descartados.
DROP TABLE IF EXISTS identidade_organization_organization;
