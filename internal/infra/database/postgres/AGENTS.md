# AGENTS.md — `internal/infra/database/postgres`

Conexão com o Postgres (gorm/pgx, stack em `agents/02`).

## Regras

- **`Connect(cfg)` puro e testável**: recebe config, devolve `*gorm.DB` ou
  erro — sem estado global, sem log escondido. Os testes usam só ele.
- Singleton do processo: **`InitPostgres` / `GetDB` / `Close`** com
  `sync.Once`. `GetDB` fora do boot devolve erro (`Use`), nunca reconecta
  silenciosamente.
- **Postgres é FATAL**: banco fora do ar no boot derruba o processo com
  mensagem clara — uma API gerenciadora sem o banco que ela gerencia não
  tem modo degradado útil.
- **Pool configurável** pelo `configs.json` (max open/idle conns, lifetime):
  sem limite explícito o padrão do driver vira gargalo ou derruba o banco.
- **Timestamps em UTC** na conexão — fuso de servidor nunca entra na
  persistência.
- **Health check** exposto (ping com timeout) para o `/api/status`; a sonda
  é injetada no servidor pelo bootstrap — este pacote não conhece HTTP.
- Senha do DSN nunca em log: mensagens de erro citam host/porta/database,
  não credenciais.

## Definição de pronto

- `Connect` testado contra banco de teste (ou DryRun onde couber); o ciclo
  de vida do singleton coberto em um único teste.
