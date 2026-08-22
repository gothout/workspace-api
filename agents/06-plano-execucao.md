# 06 — Plano de Execução e Log de Iterações

## Como o agente trabalha

O backlog vivo é o **GitHub Issues** (uma issue por fase, com checklist dentro). Este documento espelha as fases e guarda o log de iterações. O protocolo completo está no `README.md` desta pasta; resumo:

1. Abrir a **issue aberta mais antiga** (ignorando as de label `evolucao`).
2. Reler `01`, `04`, `05` + o `AGENTS.md` das pastas tocadas (e a seção de migrations do `02` se a issue criar tabelas).
3. Implementar pelos templates do `05`.
4. Validar: `go build ./... && go vet ./... && go test ./...` (+ `-race` se invariante disputada).
5. Fechar a issue e registrar a linha no **log de iterações** abaixo — ao fechar a **fase**, anexar a conferência do grafo de pacotes no **Dependency Graph do Go Architect** (doc `01`).

Uma issue por vez. Nunca pular fase. Nunca deixar o build quebrado.

## Fases

| Fase | Tema | Issue | Definição de pronto |
|---|---|---|---|
| **F0** | Fundação | [#1](https://github.com/gothout/workspace-api/issues/1) | CLI cobra (`serve`/`migrate`/`seed`), `pkg/config`, `rest_err` (com registro global de erros), `pagination`, `orgctx`, `validator`, postgres (Connect puro + singleton fatal), runner de migrations com CLI completa + `up` automático no boot (advisory lock) + teste `up → down → up`, `GET /api/status`, Swagger no ar, `arch-go.yml` + teste com coverage 100 — build/vet/test verdes. |
| **F1** | Autenticação e autorização granular | [#2](https://github.com/gothout/workspace-api/issues/2) | `infra/jwt` (access+refresh), cadeia `SetContextAuthorization → ResolveWorkspace → RequirePermission` fail-closed no `internal/middleware` (sem importar `domain`), resolução por Host com white-label + `X-Workspace-Id` como fallback, permissão `dominio:subdominio:acao` declarada rota a rota, migrations de `identidade_user_papel`/`identidade_user_papel_permissao` (**globais da plataforma, sem coluna de escopo** — exceção documentada) e `identidade_user_atribuicao` (com `organization_uuid` + `workspace_uuid`), seed dos 5 papéis com as permissões granulares, resolvedor de permissões do middleware como **adaptador no `cmd/bootstrap`** consultando essas tabelas (provisório documentado: na F4 passa a delegar ao service do user). |
| **F2** | Subdomínio organization | [#3](https://github.com/gothout/workspace-api/issues/3) | Os 8 arquivos do subdomínio + `permissions.go` + **pacote `model/` do domínio** (`internal/identidade/model/organization`) com entidades/VOs + migration up/down testada + rotas + campo `dominio` custom (opcional, único global, validado) + seed de organization raiz; checklist do `05` completo. |
| **F3** | Subdomínio workspace | [#4](https://github.com/gothout/workspace-api/issues/4) | Idem F2, com slug único global + reservados, resolução por Host validando o pertencimento à organization dona do domínio, e acesso de suporte auditado; checklist do `05` completo. |
| **F4** | Subdomínio user + login | [#5](https://github.com/gothout/workspace-api/issues/5) | CRUD de user (escopo por organization), atribuição de papéis por workspace, tabela `identidade_user_refresh_token` (revogação persistida por `jti`), credencial fechada no subdomínio com comparação de senha exposta **por interface**; aplicação `internal/identidade/application/auth` com login/refresh/logout **sem distinguir "não existe" de "senha errada"**, rodando só com a resolução da organization pelo Host; resolvedor de permissões do middleware passa a **delegar ao service do user**; checklist do `05` completo. |
| **F5** | Catálogos do sistema | [#6](https://github.com/gothout/workspace-api/issues/6) | `internal/identidade/application/catalogo` agregando os registros no bootstrap; `GET /api/application/identidade/catalogo/permissoes/minhas` (árvore filtrada do usuário) e `GET /api/system/errors` (mapa completo) no ar, conforme os contratos do `04`. |
| **F6** | Hardening do template | [#7](https://github.com/gothout/workspace-api/issues/7) | Testes de concorrência onde houver invariante (slug, e-mail, unicidade), cobertura Swagger conferida por teste, arch-go com coverage 100, grafo de dependências validado com o Go Architect (registro no log abaixo), README de uso do template. |

## Evoluções futuras (label `evolucao` — fora do loop até o F6 fechar)

| Tema | Issue | Resumo (desenho no doc 02) |
|---|---|---|
| Redis | [#8](https://github.com/gothout/workspace-api/issues/8) | Cache de permissões e de workspace por slug, locks, denylist de JWT — cliente degradável, ligado por interface. |
| ClickHouse + logs assíncronos | [#9](https://github.com/gothout/workspace-api/issues/9) | Auditoria/acesso/erro fora do caminho síncrono, writer em lote, stdout como destino degradado. |
| errobserve | [#10](https://github.com/gothout/workspace-api/issues/10) | Observador de erros por subdomínio com sinks plugáveis, alimentado pelo catálogo de erros. |

## Log de iterações

Uma linha por issue fechada. Formato: data ISO, issue (número real), arquivos criados/alterados, resultado do build/testes, observações úteis para a próxima iteração.

| data | issue | arquivos criados/alterados | resultado build/testes | observações |
|---|---|---|---|---|
| 2026-08-22 | #1 (F0) | `main.go`, `configs_example.json`, `arch-go.yml`, `arquitetura_test.go`; `internal/pkg/{config,rest_err,pagination,orgctx,validator}`; `internal/infra/database/postgres`, `internal/infra/database/migrations`, `internal/infra/jwt`; `cmd/cli`, `cmd/bootstrap`, `cmd/server` + `cmd/server/routes` | build/vet/test verdes; arch-go compliance 100 + coverage 100; smoke test end-to-end (`serve` com Postgres efêmero: `/api/status` ok, Swagger 200, 404 padronizado, SIGTERM drena) | `-race` indisponível localmente (sem gcc/CGO) — rodar na CI; migrations runner ignora arquivos `-- manual`; `migrate validate/create` rodam sem conexão |
| 2026-08-22 | #2 (F1) | `db/migrations/000{1,2}_identidade_user_*.{up,down}.sql`; `internal/infra/jwt/{token,revogacao}.go`; `internal/pkg/orgctx` (getter `Permissoes`); **`internal/middleware/{contratos,cadeia,autorizacao,resolucao,log}.go`**; `cmd/bootstrap/{middleware,seed}.go` + ligação em `bootstrap.go`; CORS com domínios custom em `routes.go` | build/vet/test verdes (incl. `up→down→up` real das novas migrations e integração de seed/resolvedor com Postgres efêmero); arch-go compliance 100 + coverage 100; smoke boot com middleware ligado | resolvedores provisórios consultam as tabelas de autorização direto (`cmd/bootstrap/middleware.go`) — na F3/F4 passam a delegar aos subdomínios, contratos do middleware NÃO mudam; provedor de domínios custom e validador de X-Api-Key entram na F2 (até lá: fail-closed); `-race` segue para a CI; conferência visual pendente até o fim da fase no Dependency Graph do Go Architect |
