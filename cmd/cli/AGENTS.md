# AGENTS.md — `cmd/cli`

Ponto de entrada do binário, montado com **cobra**. Subcomandos: `serve`
(sobe a API), `migrate` (opera o runner de migrations) e `seed` (dados
mínimos: papéis, organization raiz).

## Regras

- Comandos montados de forma **testável**: `NewRootCommand()` devolve o
  `*cobra.Command` sem executar nada — os testes instanciam, injetam args e
  buffers e chamam `Execute()`. Nada de `init()` com efeito colateral nem de
  flag registrada em variável global fora do construtor.
- Flag persistente `--config` (caminho do `configs.json`) no comando raiz;
  todo subcomando que sobe o processo repassa o valor ao `config.Init`.
- `migrate` cobre a CLI completa do runner: `up` | `down N` | `goto V` |
  `force V` | `status` | `validate` | `create {descricao}`
  (ver `internal/infra/database/migrations/AGENTS.md`). `validate` roda
  **sem conexão** — é o gate rápido de CI/local.
- O CLI não conhece regra de negócio: ele traduz argv em chamadas ao
  bootstrap e ao runner. Erro de subcomando sai com mensagem PT-BR e exit
  code não zero.

## Definição de pronto

- `--help` de cada subcomando documenta o uso em PT-BR.
- Teste de `NewRootCommand` cobrindo pelo menos a montagem e o erro de
  config ausente.
