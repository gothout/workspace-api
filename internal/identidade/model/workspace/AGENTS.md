# AGENTS.md — `internal/identidade/model/workspace`

Modelo do subdomínio `workspace` (regras do nível em
`internal/identidade/model/AGENTS.md`). É o pacote usado como **exemplo
canônico** nos templates do `agents/05`.

## Conteúdo

- Entidade `Workspace` — raiz do agregado, tabela
  `identidade_workspace_workspace`.
- VO `Slug` com `ParseSlug` (regex `^[a-z0-9]([a-z0-9-]{1,61}[a-z0-9])$`) —
  a lista de reservados NÃO é daqui: é regra do service do subdomínio.
- Construtor `NewWorkspace` validando invariantes; métodos de comportamento
  (`Inativar`, `Renomear`).
- `CreateInput`/`UpdateInput`, `ListFilter`, `StatusWorkspace` e as
  sentinelas de invariante (`ErrSlugInvalido`, `ErrNomeInvalido`,
  `ErrJaInativo`).

## O que não entra

DTOs, repository, service, lista de slugs reservados, catálogo de erros,
permissões — tudo isso mora em `internal/identidade/domain/workspace`.
