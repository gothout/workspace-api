# 00 — Visão do Produto

## O que é

O **workspace-api** é um **template de API gerenciadora em Go**: a base de identidade, tenancy e autorização sobre a qual produtos SaaS B2B são construídos. Não é um produto final — é o esqueleto completo (código, convenções e verificações executáveis) que já nasce com:

- Hierarquia real de tenancy: **organization → workspace → user**.
- Autenticação JWT (access + refresh) e `X-Api-Key` para integrações.
- Autorização **RBAC granular** `dominio:subdominio:acao`, declarada rota a rota.
- **White-label**: cada organization pode registrar um **domínio custom** próprio — `*.{dominio-da-organization}` resolve os workspaces dela.
- **Catálogos consultáveis** para o front-end: árvore de permissões do usuário (`permissoes/minhas`) e mapa completo de erros (`/api/system/errors`) — mapping sem hardcode.
- Arquitetura por domínio (DDD) com padrão singleton e regras de dependência verificadas por ferramenta: **arch-go** (gate executável) + **Dependency Graph do Go Architect** (conferência visual).

O template é **propositalmente enxuto**: sem camada de "sistema/vertente" e sem Redis, ClickHouse ou errobserve no núcleo. Essas evoluções estão especificadas no doc `02` (seção "Evoluções futuras") e têm issues próprias — **não se implementam antes da hora**.

## Hierarquia de tenancy

```
Organization  (dono do contrato — ex.: "Grupo ACME"; pode ter domínio custom white-label)
  └── Workspace  (unidade de trabalho — ex.: "Filial Sul"; slug DNS único global)
        └── User  (pessoa; papéis POR workspace — admin num, leitor em outro)
```

Regras:

1. A organization é a **raiz** do seu recorte: dona do contrato, dos workspaces e dos usuários.
2. Todo workspace pertence a **uma** organization e tem **slug DNS único global** — é ele que forma o endereço: `{slug}.{base_domain}` da plataforma ou `{slug}.{dominio-custom}` da organization dona.
3. Todo usuário pertence a **uma** organization (login único nela) e acessa **N workspaces** dela, com **papéis diferentes em cada um** (tabela de atribuição).
4. Toda requisição de negócio carrega: credencial (JWT ou `X-Api-Key`) + workspace ativo (resolvido pelo `Host`, com `X-Workspace-Id` como fallback fora de subdomínio).
5. Toda tabela de negócio tem `organization_uuid` + `workspace_uuid` — isolamento por linha (row-level scoping), aplicado de forma **fail-closed** por `orgctx.Scope`.

Detalhes, entidades e invariantes no doc `03`.

## Decisões já tomadas (não rediscutir no loop)

| Decisão | Valor |
|---|---|
| Arquitetura | Monólito DDD (`domain` + `application`), sem camada de vertente |
| HTTP | `gin` (+ `gin-contrib/cors`) |
| Banco transacional | PostgreSQL via `gorm` + driver `jackc/pgx/v5`, com pool |
| Auth | JWT (access + refresh) **e** `X-Api-Key` |
| Autorização | RBAC granular `dominio:subdominio:acao`, declarado **rota a rota** |
| Docs API | Swagger (swaggo), obrigatório em todo handler |
| Migrations | SQL puro up/down (golang-migrate), `up` automático no boot (advisory lock), rollback manual via CLI |
| Rotas | `/api/domain/identidade/{subdominio}` e `/api/application/identidade/{subdominio}` |
| Escopo | `orgctx.Scope` fail-closed em toda query de negócio |
| Contrato p/ front-end | Catálogo de permissões (`permissoes/minhas`) + mapa de erros (`/api/system/errors`) |
| Validação de arquitetura | arch-go (gate executável, coverage 100) + Dependency Graph do Go Architect (visual) |
| Idioma | Docs, comentários e mensagens de erro em **PT-BR**; identificadores de código em inglês |

## Não-escopo (núcleo do template)

- **Redis** (cache/locks/denylist JWT), **ClickHouse** (logs assíncronos) e **errobserve** (observador de erros por subdomínio) — especificados como evoluções futuras no doc `02`, cada um com sua issue.
- Qualquer domínio de negócio além de `identidade` — o template entrega a base; o produto que o adotar adiciona seus domínios seguindo os mesmos padrões (docs `01` e `05`).
