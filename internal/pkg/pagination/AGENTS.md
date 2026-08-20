# AGENTS.md — `internal/pkg/pagination`

Paginação padrão das listagens da API (contrato em `agents/04`).

## Regras

- Parâmetros de query: `page` (1-based) e `pageSize`, com defaults sensatos
  e **teto de 100** itens por página — acima disso, clamp para 100 (não
  erro: o cliente recebe o máximo possível em vez de uma negativa que ele
  não sabe tratar).
- Valores inválidos (não numéricos, negativos, zero) caem nos defaults —
  paginação ruim nunca deve derrubar uma listagem.
- Resposta genérica **`Response[T]`**:
  `{items: []T, page, page_size, total}` — `total` é o total sem paginação,
  para o front montar o paginador sem segunda chamada.
- O cálculo de `OFFSET/LIMIT` sai daqui pronto para o repository; nenhum
  repository reinventa a conta.

## Definição de pronto

- Testes table-driven cobrindo defaults, teto, inválidos e a conta de
  offset.
