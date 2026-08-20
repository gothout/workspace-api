# AGENTS.md — `internal/pkg`

Utilidades transversais da aplicação. **Folha do grafo de dependências**:
não importa nada de `internal/` fora de `pkg` — nem `infra`, nem `domain`,
nem `middleware` (regra 1 de `internal/AGENTS.md`).

## Regras

- Cada subpacote resolve UM problema transversal (config, erro HTTP,
  paginação, escopo, validação). Utilidade usada por um subdomínio só mora
  no subdomínio, não aqui.
- Sem estado global além do padrão singleton documentado no `AGENTS.md` raiz
  (`Init`/`Use`/`MustUse` onde couber).
- **Quando um pacote daqui precisar de infra no futuro** (ex.: cache no
  `orgctx`), ele declara a **interface** e o adaptador vive no lado que
  conhece o detalhe — ligado no `cmd/bootstrap`. `pkg` nunca ganha import de
  `infra`, por mais "temporário" que pareça.

## Definição de pronto

- `go list -deps ./internal/pkg/...` não contém `internal/infra`,
  `internal/domain`, `internal/application` nem `internal/middleware`.
- Todo subpacote tem testes próprios, sem depender do boot do processo.
