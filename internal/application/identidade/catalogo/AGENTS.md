# AGENTS.md — `internal/application/identidade/catalogo`

O **mapping oficial do front-end**: agrega no bootstrap o `Catalogo()` de
permissões de todos os subdomínios e o catálogo global de erros do
`rest_err`, e os expõe como contrato de API (formatos em `agents/04`).

## Rotas

- **`GET /api/application/identidade/catalogo/permissoes/minhas`** — árvore
  **filtrada pelo usuário autenticado**: `dominio → subdominio → ações`, cada
  ação com `rota`, `metodo`, `descrição` (PT-BR) e `grupo_menu`. O usuário
  vê só o que pode acessar — o front monta menus sem hardcode de regra.
- **`GET /api/system/errors`** — mapa **completo** dos erros possíveis do
  sistema: `{dominio, subdominio, erros: [{code, message, status}]}` — o
  front traduz e lista sem hardcode de código de erro.

## Regras

- **Nada é hardcoded aqui**: permissões saem do `Catalogo()` de cada
  subdomínio e erros do registro global do `rest_err` — ambos agregados no
  bootstrap via `contratos.go`. Subdomínio novo aparece nas duas rotas sem
  tocar neste pacote.
- A filtragem de `permissoes/minhas` usa o conjunto resolvido pelo
  middleware — a mesma fonte do `RequirePermission`, nunca uma releitura
  própria.
- Este pacote não persiste nada e não conhece `domain` — só interfaces.
- Mudança no formato das duas respostas é **quebra de contrato com o
  front-end**: exige atualização do `agents/04` no mesmo commit.

## Definição de pronto

- Teste prova que um `Catalogo()` falso registrado aparece na árvore e que a
  filtragem por usuário remove o que ele não tem; `/api/system/errors`
  reflete o registro sem duplicar entradas.
