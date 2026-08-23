# AGENTS.md — `internal/infra`

Adaptadores técnicos: Postgres, JWT, runner de migrations, Redis. Infra não
tem regra de negócio — sabe conectar, executar e fechar.

## Regras

- Importa só `internal/pkg` + libs externas — **NUNCA outro pacote de
  `infra`**, nem em arquivo de teste (regra 2 de `agents/01`). Conformidade
  com contrato declarado em outro pacote é **estrutural** (ex.: a denylist
  do `infra/redis` implementa `jwt.RevogadorDeRefresh` sem importá-lo) — a
  ligação acontece no `cmd/bootstrap`.
- Quando um infra precisar do outro (ex.: migrations precisam do pool do
  Postgres), a dependência entra por **interface declarada no consumidor** e
  a ligação acontece no `cmd/bootstrap` — nunca por import direto.
- Todo pacote segue o par **função pura + singleton do processo**
  (`agents/05`): `Connect(...)` puro, testável e sem estado; `Init*`/
  `Get*`/`Close` com `sync.Once` para o uso do processo.
- Postgres e JWT são **fatais**: erro no boot derruba o processo.
  Dependências degradáveis (**Redis** hoje; ClickHouse depois) devolvem
  cliente nulo com log `[DEGRADADO]`, e o consumidor é obrigado a tratar a
  ausência.
- Credenciais nunca aparecem em log nem em mensagem de erro.

## Definição de pronto

- Cada pacote tem `Connect` testado isoladamente (sem o singleton) e o
  ciclo de vida do singleton testado em UM teste só (`sync.Once` não se
  desfaz entre casos).
