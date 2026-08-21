# AGENTS.md — `internal/infra/database`

Camada de persistência: conexão (Postgres) e evolução de esquema
(migrations). Convenções de modelagem em `agents/02`.

## Convenções de tabelas

- Nome: **`{dominio}_{subdominio}_{entidade}`** (hoje `{dominio}` =
  `identidade`; ex.: `identidade_workspace_workspace`) — o prefixo do
  domínio evita colisão quando novos domínios chegarem.
- Colunas obrigatórias em toda tabela de negócio: `uuid` (PK),
  `organization_uuid`, `workspace_uuid`, `created_at`, `updated_at`,
  `deleted_at` (soft delete). Timestamps sempre em UTC.
- **Todo índice de consulta tem raiz `(organization_uuid, workspace_uuid)`**
  — o escopo fail-closed (`orgctx.Scope`) filtra por elas, e um índice que
  não começa por elas não serve às queries reais.
- **Exceção precisa de motivo documentado** no `AGENTS.md` do subdomínio e
  na migration: tabela acima do workspace (só `organization_uuid`), tabela
  raiz sem escopo (organization), **globais da plataforma sem escopo**
  (`papel`/`papel_permissao` — papéis seed), unicidade **total** (slug de
  workspace, `dominio` custom — valor removido não se libera). Exceção sem
  motivo escrito é reprovada.

## Regras

- O esquema só muda por migration (`db/migrations/AGENTS.md`) — nunca por
  `AutoMigrate` nem DDL solto em código.
- Este pacote não importa outro `infra`; o runner de migrations recebe a
  conexão por interface.

## Definição de pronto

- Toda tabela criada segue as convenções acima, conferidas na **revisão da
  migration** (checklist do `agents/05`) — o teste `up → down → up` prova
  reversibilidade, não convenção de modelagem.
