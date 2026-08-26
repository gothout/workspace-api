# AGENTS.md — `internal/todolist`

O PRIMEIRO **módulo** do monolito — prova ponta a ponta do licensing (F10).
Domínio de negócio comum (pasta direta de `internal/`, irmão de `identidade`
e `licensing`): um subdomínio `tarefa` com CRUD padrão.

## O que faz dele um MÓDULO

Nada estrutural — só a cadeia de acesso no `controller.go`: toda rota exige
`middleware.RequireAplicacao("todolist")` entre o `ResolveWorkspace` e o
`RequirePermission`. O slug é o contrato com o catálogo (`licensing_modulo`)
e com o header `Application` do cliente:

```
SetContextAuthorization → ResolveWorkspace → RequireAplicacao("todolist") → RequirePermission(todolist:tarefa:*)
```

Sem módulo `todolist` criado no catálogo, licença concedida e ativação no
workspace, TODA rota deste domínio responde 403 — fail-closed pela cadeia,
nenhuma regra extra aqui.

## Checklist de módulo novo (replicar para o próximo app)

1. Domínio irmão em `internal/{app}/` (model + domain, anatomia do agents/05).
2. `RequireAplicacao("{app}")` em TODA rota do controller.
3. Permissões `{app}:{sub}:*` + `Catalogo()` + seed dos papéis.
4. Registro: bootstrap (`New` + `[BOOTSTRAP-DI]`), `routes.go`, `catalogo.go`
   (permissões + eventos), `eventos_cobertura_test.go` (emissor conhecido).
5. Migration `NNNN_{app}_*` com escopo padrão `(organization_uuid,
   workspace_uuid)`.
6. Registro do módulo no catálogo via API do super_admin (`POST
   /api/domain/licensing/modulos` com o slug) — não nasce por seed.

## Definição de pronto

Checklist do `agents/05`; build/vet/test verdes (+ `-race`); E2E manual: criar
módulo → licenciar organization → ativar no workspace → login lista o app →
header `Application: todolist` abre as rotas; sem header = 403.
