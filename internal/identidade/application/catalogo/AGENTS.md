# AGENTS.md — `internal/identidade/application/catalogo`

O **mapping oficial do front-end**: agrega no bootstrap o `Catalogo()` de
permissões de todos os subdomínios, os `CatalogoEventos()` deles (eventos de
auditoria) e o catálogo global de erros do `rest_err`, e os expõe como
contrato de API (formatos em `agents/04`).

## Rotas

- **`GET /api/application/identidade/catalogo/permissoes/minhas`** — árvore
  **filtrada pelo usuário autenticado**: `dominio → subdominio → ações`, cada
  ação com `rota`, `metodo`, `descrição` (PT-BR) e `grupo_menu` (uma ação por
  par rota+método do `Catalogo()`). O usuário vê só o que pode acessar — o
  front monta menus sem hardcode de regra. **Cadeia completa**
  (`SetContextAuthorization → ResolveWorkspace → RequirePermission`).
- **`GET /api/system/errors`** — mapa **completo** dos erros possíveis do
  sistema: `{dominio, subdominio, erros: [{code, message, status}]}` — o
  front traduz e lista sem hardcode de código de erro. **Rota de sistema
  pública por decisão**: fora do prefixo `/api/application` e fora da
  cadeia — o mapping de tradução precisa existir antes de qualquer auth.
- **`GET /api/system/eventos`** — mapa **completo** dos eventos de auditoria
  (E3): `{dominio, subdominio, eventos: [{acao, descricao, campos}]}` — a
  `acao` é a chave estável emitida nas trilhas, a `descricao` PT-BR é o
  default exibido e `campos` descreve as chaves extras do payload. Mesma
  natureza pública da rota de erros; fonte: agregador do bootstrap sobre os
  `CatalogoEventos()` nativos.

## Regras

- **Nada é hardcoded aqui**: os erros se **auto-registram** — o `init()` de
  cada subdomínio inscreve o catálogo no registro global do `rest_err`
  (**code duplicado = panic no boot**, nunca sobrescrita); as permissões
  saem do `Catalogo()` de cada subdomínio e os eventos do
  `CatalogoEventos()` deles (declarados no `events.go`), tudo agregado no
  bootstrap e entregue via `contratos.go`. O bootstrap garante o **import
  de todos os subdomínios antes** de montar este pacote: subdomínio novo
  aparece nas três rotas sem tocar neste arquivo.
- A filtragem de `permissoes/minhas` usa o conjunto resolvido pelo
  middleware — a mesma fonte do `RequirePermission`, nunca uma releitura
  própria.
- Este pacote não persiste nada e não conhece `domain` — só interfaces.
- Mudança no formato das respostas é **quebra de contrato com o front-end**:
  exige atualização do `agents/04` no mesmo commit.

## Definição de pronto

- Teste prova que um `Catalogo()` falso registrado aparece na árvore e que a
  filtragem por usuário remove o que ele não tem; `/api/system/errors`
  reflete o registro sem duplicar entradas; `/api/system/eventos` agrupa e
  ordena o agregado determinísticamente (`campos` nulo sai como lista vazia,
  nunca null).
