# AGENTS.md — `internal/infra/database/migrations`

Runner de migrations (golang-migrate) sobre os arquivos SQL de
`db/migrations/`. As regras dos arquivos em si estão em
`db/migrations/AGENTS.md`; a estratégia completa em `agents/02`.

## Regras

- **`up` automático no boot** quando `migrations.auto_run` está ligado, com
  **advisory lock do Postgres** — duas réplicas subindo juntas não correm a
  mesma migration em paralelo. Falha aqui é fatal: o processo não sobe com
  esquema atrasado.
- Rollback é **manual e explícito** via CLI, nunca automático.
- CLI completa (montada em `cmd/cli`): `up` | `down N` | `goto V` |
  `force V` | `status` | `validate` | `create {descricao}`.
  - `validate` roda **SEM conexão**: checa par up/down presente, sequência
    sem buracos e SQL não vazio. É o gate rápido de CI/local.
  - `create` gera o par numerado seguinte já no padrão de nome.
- **Teste `up → down → up` em banco efêmero** (docker/testcontainers)
  aplicando TODAS as migrations e conferindo o esquema — migration cujo
  `down` não desfaz o `up` reprova a suíte. **Sem docker o teste pula
  sozinho** (`t.Skip`), nunca falha por ausência de ambiente.
- **Transacional por padrão**: cada arquivo roda numa transação. Exceção:
  `CREATE INDEX CONCURRENTLY`, isolado num arquivo próprio **marcado com
  `-- manual` no cabeçalho**, sem tx — um comando por arquivo. Arquivo
  manual é **ignorado pelo `auto_run`** (o boot segue com log de alerta) e
  listado pelo `status` como **pendente-manual**; a execução é manual em
  produção.
- **Sessão DEDICADA de migração**: toda operação (`up`/`down`/`goto`/`force`)
  roda num pool exclusivo de **1 conexão** aberto por `FonteConexao.SessaoDedicada`
  (implementado no bootstrap fora do pool gorm). Os timeouts curtos são SET
  de sessão e vão NESSA conexão — a mesma que o driver usa para advisory
  lock, `schema_migrations` e todo o DDL (valores em ms puros: o formato de
  duração do Go não é sintaxe válida do Postgres). O pool do negócio nunca
  recebe SET nem DDL, e a conexão da migração é fechada ao fim da operação —
  nenhuma conexão compartilhada sai com configuração alterada.
- O runner recebe a conexão por interface (não importa
  `infra/database/postgres`); quem liga é o bootstrap.

## Definição de pronto

- `validate` verde sem banco; `up → down → up` verde com banco efêmero;
  boot com `auto_run` aplica o pendente sob lock; pool de negócio sai da
  migração SEM configuração de sessão alterada (testado contra postgres
  efêmero).
