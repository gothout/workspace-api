# AGENTS.md — `internal/pkg/rest_err`

Corpo de erro padrão da API e o registro global de erros do sistema.
Convenções de status e payload em `agents/04`.

## Regras

- Corpo de erro padronizado:
  `{code, error, message, ray_trace}` — `code` é o identificador **estável**
  (`identidade.workspace.slug_em_uso`) que o front-end usa como mapping;
  `message` é PT-BR para humanos; `ray_trace` correlaciona com o log.
- **`WriteError` é a ÚNICA forma de responder erro num controller.**
  `c.JSON(...)` com corpo improvisado é reprovado em revisão — fora do
  padrão o front-end não consegue mapear.
- **Registro global de erros**: cada subdomínio inscreve seu catálogo
  (`code` estável + mensagem + status HTTP, declarado no `errors.go` dele)
  neste pacote, e o registro alimenta a rota `GET /api/system/errors`.
  Sentinela sem entrada no catálogo não fecha o checklist do `agents/05`.
- Construtores padronizados: `NewBadRequestError`, `NewUnauthorizedError`,
  `NewForbiddenError`, `NewNotFoundError`, `NewConflictError`,
  `NewUnprocessableEntityError`, `NewInternalServerError`... — todos com
  variante **`ComCausa`** que preserva o erro original para o log sem
  vazá-lo na resposta.
- Erro de infraestrutura (Postgres fora, timeout) vira 500 com `code` de
  sistema — nunca é traduzido para 4xx de negócio.

## Definição de pronto

- Todo erro que um subdomínio pode devolver está no catálogo dele e
  registrado aqui; a rota `/api/system/errors` reflete isso sem hardcode.
