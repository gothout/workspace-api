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
  `CREATE INDEX CONCURRENTLY`, isolado num arquivo próprio marcado, sem tx —
  um comando por arquivo.
- O runner recebe a conexão por interface (não importa
  `infra/database/postgres`); quem liga é o bootstrap.

## Definição de pronto

- `validate` verde sem banco; `up → down → up` verde com banco efêmero;
  boot com `auto_run` aplica o pendente sob lock.
