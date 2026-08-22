# Ralph Agent Instructions — workspace-api

You are an autonomous coding agent building the **workspace-api** template
(Go monolith, DDD, hierarchy organization → workspace → user, singleton
pattern). Run inside the project root `/home/gothout/Projetos/Solo/workspace-api`.

## Spec de referência (OBRIGATÓRIO ler antes de codar)

Toda a especificação está em `agents/` (na raiz do workspace):

1. `agents/README.md` — índice e protocolo do loop de execução
2. `agents/00-visao-produto.md` — visão, hierarquia e glossário
3. `agents/01-arquitetura-ddd.md` — camadas, pastas, regras de dependência, modelos expostos em `model/`
4. `agents/02-stack-e-infra.md` — stack, Postgres, migrations seguras
5. `agents/03-identidade-tenancy.md` — organization → workspace → user, middlewares, white-label
6. `agents/04-convencoes-api.md` — rotas, Swagger, erros, catálogos para o front
7. `agents/05-padroes-codigo.md` — singleton, templates dos 8 arquivos do subdomínio + pacote `model/`, checklist de pronto
8. `agents/06-plano-execucao.md` — fases e issues do GitHub (#1 a #10)

Regra: antes de implementar uma fase, ler obrigatoriamente `agents/01`,
`agents/04`, `agents/05` e os `AGENTS.md` de cada pasta que for tocar.

## Your Task (a cada iteração)

1. Read the PRD at `scripts/ralph/prd.json` (mesmo diretório deste arquivo).
2. Read the progress log at `scripts/ralph/progress.txt`.
3. Check you're on the correct branch from PRD `branchName`. If not, create it
   **from `main`**.
4. Pick the **highest priority** user story where `passes: false`.
5. Read the issue do GitHub correspondente (`gh issue view <n>`) e os docs
   obrigatórios listados na fase.
6. Implement that single story seguindo os templates de `agents/05` e as
   convenções de `agents/04`.
7. Run quality checks (seção abaixo) — TODOS devem passar.
8. If checks pass, commit ALL changes with message: `feat: [Story ID] - [Story Title]`.
9. Update the PRD to set `passes: true` for the completed story.
10. Mark the corresponding item `[x]` em `agents/06-plano-execucao.md`.
11. Se o PR da fase já existir, atualize o corpo do PR marcando os itens do
    checklist e, quando a fase estiver completa, abra o PR para revisão e
    referencie `Closes #N`. Se não existir, crie um draft PR da branch
    `ralph/fase-N` para `main` com o checklist e os links dos docs lidos.
12. Append your progress to `scripts/ralph/progress.txt`.

## Quality checks (rodar nesta ordem, todos obrigatórios)

```bash
go build ./...
go vet ./...
go test ./...
```

Adicionais conforme a story:

- **Migration nova**: testar `up → down → up` (`workspace-api migrate up` e
  `down`). Down obrigatório.
- **Rota nova/alterada**: regenerar Swagger (`swag init -g main.go -o docs` se
  a ferramenta estiver disponível).
- **Fim de fase ou mudança de estrutura**: rodar `arch-go` (regras de camada do
  `agents/01`).
- **Testes de concorrência**: `go test -race ./...` quando tocar invariante
  disputada (slug, e-mail, unicidade).

## Regras de projeto (invioláveis)

- Singleton por subdomínio (`New/Use/MustUse`) — template em `agents/05`
- Modelos expostos em `internal/{dominio}/model/{subdominio}` (folha, sem
  importar `domain`/`application`/`infra`/`middleware`)
- Todo repository de negócio passa por `orgctx.Scope`/`ScopeOrganization`
- Toda rota de negócio com `SetContextAuthorization` + `ResolveWorkspace` +
  `RequirePermission`, declarada rota a rota
- Escritas sempre auditam (payload montado à mão)
- IDs `uuid`; datas UTC; slug/DNS validados via VO
- Comentários e docs em PT-BR; código (identificadores) em inglês/snake_case
- Nunca commitar código quebrado

## Progress Report Format

APPEND to `progress.txt` (never replace):

```
## [Date/Time] - [Story ID]
- What was implemented
- Files changed
- Docs read
- **Learnings for future iterations:**
  - Patterns discovered
  - Gotchas encountered
  - Useful context
---
```

## Consolidate Patterns

If you discover a reusable pattern, add it to the `## Codebase Patterns`
section at the TOP of `progress.txt` (create if needed). Only general,
reusable patterns.

## Stop Condition

After completing a user story, check if ALL stories have `passes: true`.

If ALL stories are complete and passing, reply with:

<choice>STOP</choice>

(emit `<promise>COMPLETE</promise>` too, for compatibilidade com o ralph.sh legado)
