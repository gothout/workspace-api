# 04 — Convenções de API (Rotas, Swagger, Erros, Paginação)

## Workspace por subdomínio (Host)

A API é servida no subdomínio do workspace: `https://{slug}.{base_domain}/api/...` ou `https://{slug}.{dominio-custom}/api/...` (white-label). O path NÃO muda — o workspace vem do `Host`, resolvido pelo middleware (doc 03). Hosts fixos: `painel.{base_domain}` (console master, sem workspace) e `api.{base_domain}` (acesso direto, workspace via `X-Workspace-Id`).

Dev local: subdomínios de `localhost` funcionam nativamente nos browsers (`http://filial-sul.localhost:8080/api/...`); alternativa: `{slug}.127.0.0.1.nip.io`.

## Hierarquia de rotas

Duas famílias, espelhando as camadas:

```
/api/domain/identidade/{subdominio}/...        ← recursos do bounded context (camada domain)
/api/application/identidade/{subdominio}/...   ← casos de uso cross-domain (camada application)
```

- Diretório do pacote Go = **singular snake_case** (`organization`, `user`); rota = **plural kebab-case** (`organizations`, `users`).
- A auth é declarada **rota a rota** pelo `Routes()` do controller (doc 03) — nunca escondida no grupo.

### Exemplos

```
POST   /api/domain/identidade/organizations
GET    /api/domain/identidade/organizations/{uuid}
GET    /api/domain/identidade/workspaces?page=1&pageSize=10&nome=filial
PATCH  /api/domain/identidade/workspaces/{uuid}
DELETE /api/domain/identidade/workspaces/{uuid}
POST   /api/domain/identidade/workspaces/{uuid}/acoes/reativar

POST   /api/application/identidade/auth/login
POST   /api/application/identidade/auth/refresh
GET    /api/application/identidade/catalogo/permissoes/minhas
GET    /api/system/errors
```

## Verbos e ações (REST estrito)

| Operação | Método + path |
|---|---|
| Criar | `POST /api/domain/{d}/{s}` |
| Ler um | `GET /api/domain/{d}/{s}/{uuid}` |
| Listar | `GET /api/domain/{d}/{s}?page=1&pageSize=10&...filtros` |
| Atualizar (parcial) | `PATCH /api/domain/{d}/{s}/{uuid}` |
| Remover | `DELETE /api/domain/{d}/{s}/{uuid}` |
| Ação de negócio no recurso | `POST /api/domain/{d}/{s}/{uuid}/acoes/{acao}` (ex.: `.../workspaces/{uuid}/acoes/reativar`) |
| Caso de uso de aplicação | `POST /api/application/{d}/{caso}/{verbo}` |

**Proibido** subrotas verbais como `/create`, `/list`, `/update` — aqui é REST estrito.

## Swagger (obrigatório em todo handler)

```go
// @Summary      Cria um workspace
// @Description  Cria workspace na organization autenticada, validando slug único global e reservados
// @Tags         Identidade · Workspace
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        X-Workspace-Id header string false "UUID do workspace (fallback quando o host não tem subdomínio)"
// @Param        request body CreateWorkspaceRequestDto true "Dados do workspace"
// @Success      201 {object} WorkspaceResponseDto
// @Failure      400 {object} rest_err.RestErr
// @Failure      403 {object} rest_err.RestErr
// @Failure      409 {object} rest_err.RestErr "Slug em uso"
// @Router       /api/domain/identidade/workspaces [post]
```

- `swag init -g main.go -o docs` regenera a cada rota nova/alterada — **não é opcional**; `docs/` é gerado e versionado. UI em `/doc/index.html`.
- Tags no formato `Identidade · {Subdominio}`.
- Security definitions no `main.go`: `BearerAuth` (header `Authorization`) e `ApiKeyAuth` (header `X-Api-Key`).

## Erros — formato padrão

`pkg/rest_err.RestErr`:

```json
{
  "code": 409,
  "error": "conflict",
  "message": "Slug já está em uso por outro workspace.",
  "ray_trace": "01J2K4..."
}
```

| HTTP | Uso |
|---|---|
| 400 | body/query inválido, UUID malformado, regra de entrada |
| 401 | token/chave ausente, inválida ou expirada |
| 403 | sem permissão / sem vínculo com o workspace |
| 404 | recurso não encontrado (no escopo resolvido) |
| 409 | conflito de negócio (duplicidade, slug em uso, unicidade) |
| 422 | regra de negócio violada (entidade válida, operação proibida — ex.: slug reservado) |
| 500 | erro interno (sempre com `ray_trace` logado) |

O controller traduz **sentinela → status** num `switch` (template `traduzir()` do doc 05) e responde **só por `rest_err.WriteError`** — nunca monta `c.JSON` de erro à mão. Todo código de erro estável entra no mapa de erros (contrato no fim deste doc).

## Paginação

- Query: `page` (>= 1, default 1), `pageSize` (<= 100, default 10) — teto de 100 aplicado pelo `pkg/pagination`.
- Resposta de lista: `{ "items": [...], "page": 1, "page_size": 10, "total": 137 }`.
- Filtros do subdomínio como query params extras, documentados no Swagger.

## Datas e identificadores

- Datas: ISO 8601 UTC (`2026-08-20T22:45:43Z`). Filtros de dia: `AAAA-MM-DD`.
- UUID v4 em path/query/body; **inválido = 400** (não 404, não 500).

## Headers

| Header | Obrigatório | Uso |
|---|---|---|
| `Authorization: Bearer` | rotas humanas | JWT access |
| `X-Api-Key` | integrações | chave de API da organization |
| `X-Workspace-Id` | fallback sem subdomínio | workspace ativo (UUID) — só em `api.{base_domain}`/dev; **subdomínio vence** se ambos presentes |
| `X-Request-Id` | opcional | se ausente, o servidor gera o `ray_trace` |

## Auditoria (obrigatória)

Toda operação de **escrita** (POST/PATCH/DELETE e ações de negócio) audita: domínio, subdomínio, ação, função, identidade vinda do ctx (organization/workspace/user/ray_trace), `success`, input/output. O payload é um `map[string]any` **montado à mão**, com identificadores, contagens, datas e vocabulário fechado — **nunca texto livre**. No núcleo do template a trilha sai por log estruturado (slog); o destino assíncrono (ClickHouse) é evolução futura (doc 02).

## CONTRATO — endpoint de permissões

`GET /api/application/identidade/catalogo/permissoes/minhas` (auth: `BearerAuth` ou `ApiKeyAuth`).

Devolve a árvore `dominio → subdominio → ações` **já filtrada pelas permissões efetivas do usuário** no workspace ativo. O front-end monta menu/botões a partir dela: ação ausente na árvore = controle escondido no front. `super_admin` recebe a árvore inteira.

```json
{
  "dominios": [
    {
      "dominio": "identidade",
      "subdominios": [
        {
          "subdominio": "workspace",
          "acoes": [
            {
              "permissao": "identidade:workspace:ler",
              "descricao": "Listar e consultar workspaces da organization",
              "rota": "/api/domain/identidade/workspaces",
              "metodo": "GET",
              "grupo_menu": "Identidade · Workspaces"
            },
            {
              "permissao": "identidade:workspace:editar",
              "descricao": "Editar dados do workspace",
              "rota": "/api/domain/identidade/workspaces/{uuid}",
              "metodo": "PATCH",
              "grupo_menu": "Identidade · Workspaces"
            }
          ]
        }
      ]
    }
  ]
}
```

Regras do contrato: `permissao` é o valor exato exigido pela rota; `descricao` em PT-BR; `rota` é o path com `{uuid}` onde couber; `grupo_menu` é o agrupamento sugerido para o menu do front. Fonte dos dados: `Catalogo()` de cada subdomínio, agregado no bootstrap (doc 03).

## CONTRATO — mapa de erros

Todo erro do sistema carrega um **code estável** `identidade.{subdominio}.{nome}` (snake_case), definido no `errors.go` do subdomínio e registrado no mapa global do `rest_err`.

`GET /api/system/errors` — rota auxiliar pública do sistema. Devolve **todos os erros possíveis**, organizados por domínio/subdomínio:

```json
{
  "erros": [
    {
      "dominio": "identidade",
      "subdominio": "workspace",
      "erros": [
        { "code": "identidade.workspace.nao_encontrado", "message": "Workspace não encontrado.", "status": 404 },
        { "code": "identidade.workspace.slug_em_uso", "message": "Slug já está em uso por outro workspace.", "status": 409 },
        { "code": "identidade.workspace.slug_reservado", "message": "Slug reservado pela plataforma.", "status": 422 }
      ]
    },
    {
      "dominio": "identidade",
      "subdominio": "user",
      "erros": [
        { "code": "identidade.user.credenciais_invalidas", "message": "Credenciais inválidas.", "status": 401 },
        { "code": "identidade.user.email_em_uso", "message": "E-mail já cadastrado nesta organization.", "status": 409 }
      ]
    }
  ]
}
```

O front-end consome como **mapping de tradução/listagem**: o `code` é a chave estável (para tradução e estilização) e a `message` PT-BR do servidor é o default exibido. Sentinela nova sem entrada no catálogo **não fecha o checklist** do subdomínio (doc 05) — é esse catálogo que alimenta a rota.
