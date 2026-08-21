# AGENTS.md — `db/migrations`

Arquivos SQL puro das migrations. O runner está em
`internal/infra/database/migrations`; a estratégia completa em `agents/02`.

## Regras

- **SQL puro**, par obrigatório:
  `NNNN_{dominio}_{subdominio}_{descricao}.up.sql` +
  `...down.sql` (hoje `{dominio}` = `identidade`), numeração **sequencial
  sem buracos** (o `migrate validate` reprova buraco).
- **Todo `up` nasce com o `down` no mesmo commit**, exercitado pelo teste
  `up → down → up` em banco efêmero — `down` que não desfaz o `up` reprova a
  suíte.
- **Migration aplicada NUNCA é editada** — correção é migration nova. Editar
  o passado diverge os bancos que já a aplicaram.
- Mudança destrutiva segue **expand-and-contract**: cria a estrutura nova →
  backfill → remove a velha numa migration posterior. Nunca troca no mesmo
  passo.
- **Toda tabela de negócio nasce com índice de escopo** cuja raiz é
  `(organization_uuid, workspace_uuid)` — convenção e exceções em
  `internal/infra/database/AGENTS.md`.
- **`CREATE INDEX CONCURRENTLY` isolado** num arquivo próprio, sem
  transação, **um comando por arquivo** — é a única exceção ao
  "transacional por padrão" e o arquivo leva `-- manual` no cabeçalho:
  **ignorado pelo `auto_run`** (o boot segue com log de alerta), listado
  pelo `status` como **pendente-manual** e executado à mão em produção.
- Criar novas **via CLI**: `workspace-api migrate create {descricao}` — gera
  o par numerado seguinte já no padrão, nunca o arquivo à mão.

## Definição de pronto

- `migrate validate` verde sem conexão; `up → down → up` verde em banco
  efêmero; nenhum arquivo aplicado modificado no diff.
