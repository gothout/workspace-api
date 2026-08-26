# AGENTS.md — `internal/licensing/model`

Modelos expostos do domínio licensing: um pacote por subdomínio (`modulo`,
`licenca`, `ativacao`). Mesmas regras do `internal/identidade/model`: folha
absoluta, entidade com `TableName()`, VOs com `ParseX`, construtor `NewX`
validando invariantes, sentinelas de invariante aqui e catálogo delas no
`errors.go` do subdomínio.

Particularidades:

- `modulo` carrega o VO `Slug` (formato DNS via `validator.SlugValido`) — o
  valor do header `Application`.
- Projeções de leitura com join (`LicencaComModulo`, `AtivacaoComModulo`)
  vivem AQUI (folha) para qualquer camada consumir sem importar irmão.
- Licença e ativação são BINÁRIAS: não há métodos de transição de estado —
  existe (viva) ou está removido logicamente.

## Definição de pronto

`go list -deps ./internal/licensing/model/...` não contém `domain`,
`application`, `infra` nem `middleware`.
