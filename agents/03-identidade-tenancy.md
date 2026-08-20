# 03 — Identidade, Tenancy e Autorização

## Modelo hierárquico

```
Organization  (dono do contrato; opcionalmente tem domínio custom white-label)
  └── Workspace  (unidade de trabalho; slug DNS único global)
        └── Atribuição  (user × workspace × papel)
              └── User  (pessoa; login único na organization)
```

Cenário-alvo: um cliente **dono de contrato** (organization) mantém **vários workspaces** (filiais, unidades, projetos). Um gestor pode ser `admin_workspace` em todos; um operador tem `usuario_workspace` só no workspace "Filial Sul".

## Entidades (domínio `identidade`)

| Subdomínio | Tabela | Campos-chave |
|---|---|---|
| `organization` | `identidade_organization_organization` | uuid, nome, documento, **dominio** (custom, opcional, único global), live, created_at, updated_at, deleted_at |
| `organization` | `identidade_organization_apikey` | uuid, organization_uuid, nome, key_hash, escopo (`organization` ou lista de workspaces), permissoes[], expires_at, live |
| `workspace` | `identidade_workspace_workspace` | uuid, organization_uuid, nome, **slug** (único global, DNS-safe), live, created_at, updated_at, deleted_at |
| `user` | `identidade_user_user` | uuid, organization_uuid, nome, email (único por organization), senha_hash (bcrypt, `json:"-"`), live |
| `user` | `identidade_user_papel` | uuid, nome (único), descricao — papéis seed globais |
| `user` | `identidade_user_papel_permissao` | papel_uuid, permissao (string `dominio:subdominio:acao`) |
| `user` | `identidade_user_atribuicao` | uuid, user_uuid, workspace_uuid, papel_uuid |

Regras de escopo por tabela: `organization` é a **raiz** — não tem escopo acima (exceção documentada, sem `orgctx.Scope`); `workspace` escopa por `organization_uuid`; as demais por `organization_uuid` (+ `workspace_uuid` onde a coluna existe, como em `atribuicao`).

### O campo `dominio` da organization (white-label)

A organization pode registrar um **domínio próprio** (ex.: `parceiro.com`): opcional, **único global**, formato de domínio validado. Com ele, `*.{dominio-da-organization}` resolve os workspaces **daquela organization** — white-label para parceiros. Inativar a organization suspende seus workspaces e desativa o domínio custom. DNS/TLS wildcard do domínio do parceiro são **configuração do parceiro**, documentados no README do template — não entram no código.

## Papéis seed (5, globais)

| Papel | Permissões | Quem é |
|---|---|---|
| `super_admin` | `*:*` | Admin da plataforma: atravessa qualquer exigência, entra em qualquer workspace de qualquer organization. |
| `admin_organization` | `identidade:*` | Dono do contrato: administra workspaces, usuários, papéis e chaves da organization; entra como admin em qualquer workspace **da própria organization** (acesso de suporte). |
| `admin_workspace` | `identidade:workspace:editar` + `identidade:user:*` no workspace | Administrador de um workspace: tudo dentro dos workspaces em que exerce o papel, menos mudar a estrutura do contrato. |
| `usuario_workspace` | ações operacionais do workspace, sem administração de identidade | Operador: trabalha no workspace, não cria usuário nem mexe em papéis. |
| `somente_leitura` | só as ações `:ler` | Consulta sem escrita. |

Os conjuntos exatos de permissões de cada papel são definidos no **seed** (F1/F4) a partir das constantes `PermX` dos subdomínios. Papéis customizados por organization são evolução possível — o template entrega os 5 seed.

## Autenticação — dois mecanismos

### 1. JWT (usuário humano)

- `POST /api/application/identidade/auth/login` → `access_token` (curto — `security.jwt_ttl_min`) + `refresh_token` (longo — `security.jwt_refresh_ttl_hours`).
- Claims do access token: `sub` (user uuid), `org` (organization uuid), `wks` (workspace uuid ativo, quando houver), `name`, `email`, `typ=access`. Refresh: `typ=refresh`, `jti` único.
- `POST /api/application/identidade/auth/refresh` → novo par de tokens.
- `POST /api/application/identidade/auth/logout` → revoga o refresh.
- Resposta de autenticação **não distingue "usuário não existe" de "senha errada"** — nem no erro nem no tempo gasto.
- Denylist de refresh em Redis é **evolução futura**: a interface já é declarada no `infra/jwt`; sem Redis, o logout revoga pelo armazenamento disponível e a ligação distribuída acontece no bootstrap quando a evolução chegar.

### 2. X-Api-Key (integrações/server-to-server)

- Header `X-Api-Key: <chave>`. Escopo: a **organization inteira** OU uma **lista de workspaces** dela. Permissões explícitas gravadas na chave.
- Criada/gerenciada em `/api/domain/identidade/apikeys` (apenas `admin_organization`).
- A chave é exibida **uma única vez** na criação; o banco guarda só o hash. Chave nunca ganha acesso de suporte — o alcance dela é só o da credencial.

## Cadeia de middlewares (obrigatória)

Pacote `internal/middleware`. Cadeia nas rotas protegidas, nesta ordem:

```
SetContextAuthorization()   → valida JWT ou X-Api-Key; injeta a identidade no ctx
ResolveWorkspace()          → resolve o workspace pelo Host (subdomínio) ou X-Workspace-Id (fallback); valida vínculo; injeta o escopo
RequirePermission("identidade:workspace:editar")  → checa a permissão granular declarada NA ROTA
```

- O middleware **não importa `domain`** (seria ciclo — os controllers de `domain` importam ELE): tudo o que precisa do negócio entra por interfaces em `contratos.go`, ligadas no `cmd/bootstrap`.
- **Fail-closed**: cadeia não inicializada = rota fechada (403), nunca aberta.
- Falha de permissão = 403 com `rest_err` padrão + auditoria `success=false`.

## Resolução de workspace pelo Host

```
https://filial-sul.{base_domain}/api/...     → workspace "filial-sul" resolvido no domínio da PLATAFORMA
https://filial-sul.parceiro.com/api/...      → workspace "filial-sul" da organization dona de parceiro.com (white-label)
https://painel.{base_domain}/...             → console master (sem workspace; slug reservado)
https://api.{base_domain}/...                → acesso direto (integrações; workspace via X-Workspace-Id)
```

Regras do `ResolveWorkspace`:

1. **Subdomínio é a fonte primária**: extrai `{slug}` e o domínio-base do header `Host`.
2. **Resolve o domínio-base para a organization dona**: se o domínio-base é um `dominio` custom registrado → a organization dona dele; senão, fallback para o `base_domain` da plataforma (config).
3. **Resolve o workspace validando o pertencimento**: o workspace de `{slug}` TEM que pertencer àquela organization — **workspace alheio no domínio do parceiro = 404** (não vaza existência).
4. Workspace inexistente ou inativo = **404**.
5. **`X-Workspace-Id` só como fallback**: aceito apenas quando o Host NÃO tem subdomínio de workspace (`api.{base_domain}`, `localhost`, IP) — integrações server-to-server e dev local.
6. Ambos presentes e divergentes → **subdomínio vence** (previne spoofing de workspace por header no navegador).
7. **Vínculo user↔workspace obrigatório**: subdomínio correto sem atribuição = **403** (exceção: acesso de suporte, abaixo).
8. Slug: `^[a-z0-9]([a-z0-9-]{1,61}[a-z0-9])$`, único global, fora da **lista de reservados**: `www`, `api`, `app`, `admin`, `docs`, `status`, `mail`, `suporte`, `painel`.

## CORS

- `AllowOriginFunc` aceita qualquer origem com sufixo `.{base_domain}` **e** qualquer origem sob os **domínios custom registrados** pelas organizations — workspace novo e parceiro novo não exigem mudança de config.
- Origens exatas extras vêm de `server.http.cors.allowed_origins`.

## Autorização granular

- Toda rota declara a permissão exata que a libera — `RequirePermission("identidade:workspace:editar")` **rota a rota, nunca no grupo**.
- Permissão é string **`dominio:subdominio:acao`** (ex.: `identidade:user:criar`).
- **Curinga só para papel admin**: `identidade:*` (admin_organization) e `*:*` (super_admin).
- O `permissions.go` de cada subdomínio tem as constantes `PermX` **e** o `Catalogo()` com os metadados de cada permissão: `{permissao, descricao PT-BR, rotas, metodo, grupo_menu}` (template no doc 05).

## Catálogo consultável

- O **bootstrap agrega** o `Catalogo()` de todos os subdomínios num **registro único**, entregue ao subdomínio de aplicação `application/identidade/catalogo`.
- `GET /api/application/identidade/catalogo/permissoes/minhas` devolve ao usuário autenticado **só o que ele pode acessar**, já em árvore `dominio → subdominio → ações` com rota/método/descrição (contrato completo no doc 04).
- O front-end monta menu e botões a partir desse endpoint — **sem hardcode de regra de acesso**.

## Acesso de suporte

- `admin_organization` entra como admin em **qualquer workspace da própria organization**, mesmo sem atribuição lá — a identidade continua sendo a dele e o acesso carrega as permissões de admin.
- `super_admin` entra em qualquer workspace de qualquer organization com `*:*`.
- Toda concessão de suporte é **auditada** (quem entrou, onde, com que papel).
- Chave de API **nunca** ganha suporte.
- O console master da plataforma vive em `painel.{base_domain}` (slug reservado).

## Escopo de dados (fail-closed)

- Toda query de tabela de negócio passa por `orgctx.Scope(db, ctx)` — aplica `organization_uuid` (+ `workspace_uuid` quando a tabela o tem) vindos do ctx.
- Sem escopo no ctx, a query **falha** — nunca roda aberta.
- Exceções documentadas: `organization` é a raiz (sem escopo acima); `workspace` escopa só por `organization_uuid`. Cada exceção tem o motivo escrito no `AGENTS.md` do pacote.
- Nenhum endpoint expõe dado de outra organization, mesmo com UUID correto.

## Auditoria de acesso

Login, refresh, logout e login falho auditam com `dominio=identidade`. No núcleo do template a trilha sai por **log estruturado** (slog); o destino assíncrono (ClickHouse) é evolução futura (doc 02). Payload de auditoria montado à mão com identificadores e vocabulário fechado — nunca texto livre (doc 04).
