# AGENTS.md — `cmd/server`

Servidor HTTP do processo: `http.Server` sobre o `gin.Engine` montado em
`routes`. Sabe subir, sabe descer — e nada mais.

## Regras

- **Timeouts explícitos sempre**: `ReadHeaderTimeout`, `ReadTimeout`,
  `WriteTimeout`, `IdleTimeout` e timeout próprio para o shutdown. Servidor
  sem timeout é servidor refém de cliente lento.
- **Graceful shutdown**: no SIGTERM/SIGINT, para de aceitar conexões e
  **drena as requisições em voo** até o timeout de shutdown; só então o
  bootstrap segue o fechamento LIFO.
- **NÃO importa `internal/infra`**: as sondas de saúde (ex.: ping do
  Postgres usado pelo `/api/status`) são injetadas pelo bootstrap como
  funções/interfaces. Motivo: o servidor testável não pode depender de um
  banco de pé, e o arch-go mantém a camada honesta.
- Não registra rota de negócio — isso é do `routes` + controllers.

## Definição de pronto

- Teste sobe o servidor numa porta livre, faz uma requisição e prova que o
  shutdown drena uma requisição lenta em vez de cortá-la.
