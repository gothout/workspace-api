# AGENTS.md — `internal/infra/database`

Camada de persistência: conexão (Postgres) e evolução de esquema
(migrations). Convenções de modelagem em `agents/02`.

## Convenções de tabelas

- Nome: **`identidade_{subdominio}_{entidade}`** (ex.:
  `identidade_workspace_workspace`) — o prefixo do domínio evita colisão
  quando novos domínios chegarem.
- Colunas obrigatórias em toda tabela de negócio: `uuid` (PK),
  `organization_uuid`, `workspace_uuid`, `created_at`, `updated_at`,
  `deleted_at` (soft delete). Timestamps sempre em UTC.
- **Todo índice de consulta tem raiz `(organization_uuid, workspace_uuid)`**
  — o escopo fail-closed (`orgctx.Scope`) filtra por elas, e um índice que
  não começa por elas não serve às queries reais.
- **Exceção precisa de motivo documentado** no `AGENTS.md` do subdomínio e
  na migration: tabela acima do workspace (só `organization_uuid`), tabela
  raiz sem escopo (organization), unicidade global (slug de workspace).
  Exceção sem motivo escrito é reprovada.

## Regras

- O esquema só muda por migration (`db/migrations/AGENTS.md`) — nunca por
  `AutoMigrate` nem DDL solto em código.
- Este pacote não importa outro `infra`; o runner de migrations recebe a
  conexão por interface.

## Definição de pronto

- Toda tabela criada segue as convenções acima, verificadas no teste
  `up → down → up` do runner.
