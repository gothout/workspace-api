# AGENTS.md — `internal/pkg/orgctx`

Escopo da hierarquia **organization → workspace → user** carregado no
contexto da requisição e aplicado às queries. Desenho do tenancy em
`agents/03`.

## Regras

- O ctx carrega `organization_uuid`, `workspace_uuid`, `user_uuid` e o
  conjunto de **permissões** resolvidas — injetados pelo
  `internal/middleware`, nunca pelo corpo da requisição.
- **`Scope(db, ctx)` é aplicado por TODO repository de negócio.** Sem escopo
  no contexto a query **FALHA** (`ErrEscopoAusente`) — nunca roda aberta
  varrendo a tabela inteira. Fail-closed é a regra que protege todos os
  tenants; uma query aberta "só dessa vez" é o incidente.
- **`ScopeOrganization(db, ctx)`** é a variante para tabelas que vivem
  *acima* do workspace (ex.: `workspace` é filho de organization): exige só
  `organization_uuid`. Usar fora desse caso precisa de **exceção documentada
  com motivo** no `AGENTS.md` do subdomínio.
- Consultas que fogem do escopo por natureza (resolução de slug pelo Host,
  que acontece *antes* de existir escopo) são exceções nomeadas no
  subdomínio — com motivo escrito e teste que impede o vazamento do
  resultado para rotas de administração.

## Definição de pronto

- Teste prova que query sem escopo falha e que o filtro gerado contém
  exatamente as colunas de escopo da tabela.
