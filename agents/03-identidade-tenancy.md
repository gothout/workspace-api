# 03 — Identidade, Tenancy e Autorização

## Modelo hierárquico

```
Organization  (dono do contrato; opcionalmente tem domínio custom white-label)
  ├── Workspace  (unidade de trabalho; slug DNS único global)
  └── User  (pessoa; login único na organization)
        └── Atribuição  (liga user × workspace × papel)
```

Cenário-alvo: um cliente **dono de contrato** (organization) mantém **vários workspaces** (filiais, unidades, projetos). Um gestor pode ser `admin_workspace` em todos; um operador tem `usuario_workspace` só no workspace "Filial Sul".

## Entidades (domínio `identidade`)

| Subdomínio | Tabela | Campos-chave |
|---|---|---|
| `organization` | `identidade_organization_organization` | uuid, nome, documento, **dominio** (custom, opcional, único global), status (ativo/inativo), created_at, updated_at, deleted_at |
| `organization` | `identidade_organization_apikey` | uuid, organization_uuid, nome, key_hash (SHA-256), escopo (`organization` ou lista de workspaces), permissoes[], expires_at, status |
| `workspace` | `identidade_workspace_workspace` | uuid, organization_uuid, nome, **slug** (único global, DNS-safe), status (ativo/inativo), created_at, updated_at, deleted_at |
| `user` | `identidade_user_user` | uuid, organization_uuid, nome, email (único por organization), senha_hash (bcrypt, `json:"-"`), status (ativo/inativo) |
| `user` | `identidade_user_refresh_token` | uuid, user_uuid, organization_uuid, **jti** (único), expira_em, revogado_em, created_at — revogação persistida do refresh |
| `user` | `identidade_user_papel` | uuid, nome (único), descricao — papéis seed globais (**tabela SEM escopo** — exceção documentada, criada na F1) |
| `user` | `identidade_user_papel_permissao` | papel_uuid, permissao (string `dominio:subdominio:acao`) — **global, SEM escopo** (exceção documentada, criada na F1) |
| `user` | `identidade_user_atribuicao` | uuid, organization_uuid, workspace_uuid, user_uuid, papel_uuid — criada na F1 |

Regras de escopo por tabela — **duas variantes, ambas fail-closed**:

- **`orgctx.Scope`** filtra por `organization_uuid` **e** `workspace_uuid` — tabelas da vida dentro do workspace (ex.: `atribuicao`).
- **`orgctx.ScopeOrganization`** filtra só por `organization_uuid` — tabelas **acima** do workspace (`workspace`, `user`, `refresh_token`, `apikey`). Administração de users usa esta variante; listagem de users **por workspace** é via `atribuicao`, porque `identidade_user_user` não tem `workspace_uuid`.
- `organization` é a **raiz** — não tem escopo acima (exceção documentada, sem `orgctx.Scope`); `papel` e `papel_permissao` são **globais da plataforma** — sem coluna de escopo (exceção documentada: papéis seed globais).

### O campo `dominio` da organization (white-label)

A organization pode registrar um **domínio próprio** (ex.: `parceiro.com`): opcional, **único global**, formato validado. A validação (`ParseDominio`) **rejeita**: igual ao `base_domain` da plataforma, **ascendente ou descendente** dele, e **public suffix** — exige eTLD+1 no mínimo. Com ele, `*.{dominio-da-organization}` resolve os workspaces **daquela organization** — white-label para parceiros. Inativar a organization suspende seus workspaces e desativa o domínio custom — a **cascata organization→workspace vai por interface declarada no `contratos.go` da organization**, ligada no `cmd/bootstrap`, nunca por chamada direta ao subdomínio irmão. DNS/TLS wildcard do domínio do parceiro são **configuração do parceiro**, documentados no README do template — não entram no código.

## Papéis seed (5, globais)

| Papel | Permissões | Quem é |
|---|---|---|
| `super_admin` | `*:*` | Admin da plataforma: atravessa qualquer exigência, entra em qualquer workspace de qualquer organization. |
| `admin_organization` | `identidade:workspace:*` + `identidade:user:*` + `identidade:organization:gerenciar_apikeys` | Dono do contrato: administra workspaces, usuários, papéis e as chaves de API da organization; entra como admin em qualquer workspace **da própria organization** (acesso de suporte). O curinga **não** cobre `identidade:organization:*` — administrar organizations (listar/criar/editar) é `super_admin`/suporte auditado. |
| `admin_workspace` | `identidade:workspace:editar` + `identidade:user:*` no workspace | Administrador de um workspace: tudo dentro dos workspaces em que exerce o papel, menos mudar a estrutura do contrato. |
| `usuario_workspace` | ações operacionais do workspace, sem administração de identidade | Operador: trabalha no workspace, não cria usuário nem mexe em papéis. |
| `somente_leitura` | só as ações `:ler` | Consulta sem escrita. |

Os conjuntos exatos de permissões de cada papel são definidos no **seed (F1)** como strings estáveis — os subdomínios declaram as mesmas constantes `PermX` nas fases seguintes; divergência entre seed e `PermX` é bug de contrato. Papéis customizados por organization são evolução possível — o template entrega os 5 seed.

## Autenticação — dois mecanismos

### 1. JWT (usuário humano)

Servido pela aplicação **`internal/identidade/application/auth`** — o login cruza `user` com a resolução da organization pelo Host, então é orquestração, não regra de um subdomínio só.

- `POST /api/application/identidade/auth/login` → `access_token` (curto — `security.jwt_ttl_min`) + `refresh_token` (longo — `security.jwt_refresh_ttl_hours`).
- **O login exige Host que resolva a organization** (subdomínio de workspace ou domínio custom dela); as rotas de auth rodam **só com a resolução da organization**, sem vínculo nem permissão. Host sem organization resolvível = **o mesmo 401 genérico** de credenciais inválidas.
- Claims do access token: `sub` (user uuid), `org` (organization uuid), `wks` (workspace uuid ativo, quando houver), `name`, `email`, `typ=access`. Refresh: `typ=refresh`, `jti` único.
- `POST /api/application/identidade/auth/refresh` → novo par de tokens.
- `POST /api/application/identidade/auth/logout` → revoga o refresh.
- **O refresh token é persistido no Postgres** (`identidade_user_refresh_token`, uma linha por `jti`): o logout **revoga no banco** (marca `revogado_em`). A denylist Redis é **evolução futura** e vira só **cache dessa revogação** — a interface já é declarada no `infra/jwt`.
- Resposta de autenticação **não distingue "usuário não existe" de "senha errada"** — nem no erro nem no tempo gasto.

### 2. X-Api-Key (integrações/server-to-server)

- Header `X-Api-Key: <chave>`. A chave é um **token aleatório**; o banco guarda só o **SHA-256** dele (`key_hash`), e o token é exibido **uma única vez** na criação.
- Escopo: a **organization inteira** OU uma **lista de workspaces** dela. Permissões explícitas gravadas na chave.
- **O "vínculo" da chave é o escopo dela**: o workspace resolvido precisa estar na lista da chave (ou a chave ter escopo organization) — falha = **403**. Acesso de suporte **nunca** via apikey.
- Criada/gerenciada em **`/api/domain/identidade/organizations/{uuid}/api-keys`**, com a permissão `identidade:organization:gerenciar_apikeys`.

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

Regras do `ResolveWorkspace` (fail-closed):

1. **O Host é casado por sufixo** contra o `base_domain` da plataforma **e** a lista de domínios custom registrados — comparação **case-insensitive** e **sem a porta** (dev usa `localhost:8080`). Sem casamento = **404**: nunca fallback aberto para Host desconhecido.
2. **O slug é o rótulo à esquerda do sufixo casado**; Host sem rótulo (`base_domain` nu, `api.`, `painel.`) **não tem workspace** — segue sem escopo de workspace (console master ou acesso direto).
3. **Resolve o workspace validando o pertencimento**: o workspace de `{slug}` TEM que pertencer àquela organization — **workspace alheio no domínio do parceiro = 404** (não vaza existência).
4. Workspace inexistente ou inativo = **404**.
5. **`X-Workspace-Id` só como fallback**: aceito apenas quando o Host NÃO tem subdomínio de workspace (`api.{base_domain}`, `localhost`, IP) — integrações server-to-server e dev local.
6. Ambos presentes e divergentes → **subdomínio vence** (previne spoofing de workspace por header no navegador).
7. **Vínculo user↔workspace obrigatório**: subdomínio correto sem atribuição = **403** (exceção: acesso de suporte, abaixo). Para **X-Api-Key**, o "vínculo" é o escopo da chave: workspace resolvido fora da lista = **403**.
8. Slug: `^[a-z0-9]([a-z0-9-]{1,61}[a-z0-9])$`, único global, fora da **lista de reservados**: `www`, `api`, `app`, `admin`, `docs`, `status`, `mail`, `suporte`, `painel`.

## CORS

- `AllowOriginFunc` faz **parse do Origin e casa o HOST** por igualdade ou sufixo `.`+domínio contra o `base_domain` da plataforma **e** os domínios custom registrados pelas organizations — **nunca `HasSuffix` em string crua** (`evil-{base_domain}` não passa). Workspace novo e parceiro novo não exigem mudança de config.
- Origens exatas extras vêm de `server.http.cors.allowed_origins`.

## Autorização granular

- Toda rota declara a permissão exata que a libera — `RequirePermission("identidade:workspace:editar")` **rota a rota, nunca no grupo**.
- Permissão é string **`dominio:subdominio:acao`** (ex.: `identidade:user:criar`).
- **Curinga só para papel admin**: `identidade:workspace:*` + `identidade:user:*` (admin_organization) e `*:*` (super_admin).
- O `permissions.go` de cada subdomínio tem as constantes `PermX` **e** o `Catalogo()` com os metadados de cada permissão: `{permissao, descricao PT-BR, rotas []RotaMeta{rota, metodo}, grupo_menu}` — **um par rota+método por entrada**; a árvore do endpoint emite uma ação por par (template no doc 05).

## Catálogo consultável

- Os **erros se auto-registram**: o `init()` de cada subdomínio inscreve o catálogo dele no registro global do `rest_err` — **code duplicado = panic no boot** (nunca sobrescrita).
- As **permissões são agregadas no bootstrap**: o `Catalogo()` de todos os subdomínios num **registro único**, entregue à aplicação `internal/identidade/application/catalogo` — o bootstrap garante o import de todos os subdomínios **antes** de montá-la.
- `GET /api/application/identidade/catalogo/permissoes/minhas` devolve ao usuário autenticado **só o que ele pode acessar**, já em árvore `dominio → subdominio → ações` com rota/método/descrição (contrato completo no doc 04).
- O front-end monta menu e botões a partir desse endpoint — **sem hardcode de regra de acesso**.

## Acesso de suporte

- `admin_organization` entra como admin em **qualquer workspace da própria organization**, mesmo sem atribuição lá — a identidade continua sendo a dele e o acesso carrega as permissões de admin.
- `super_admin` entra em qualquer workspace de qualquer organization com `*:*`.
- Toda concessão de suporte é **auditada** (quem entrou, onde, com que papel).
- Chave de API **nunca** ganha suporte.
- O console master da plataforma vive em `painel.{base_domain}` (slug reservado).

## Escopo de dados (fail-closed)

- Toda query de tabela de negócio passa por `orgctx.Scope(db, ctx)` (organization **e** workspace) ou `orgctx.ScopeOrganization(db, ctx)` (só organization), conforme a tabela — variantes na seção de entidades acima.
- Sem escopo no ctx, a query **falha** — nunca roda aberta.
- Exceções documentadas: `organization` é a raiz (sem escopo acima); `papel`/`papel_permissao` são globais; `FindBySlug` de workspace é global (resolução pelo Host). Cada exceção tem o motivo escrito no `AGENTS.md` do pacote.
- Nenhum endpoint expõe dado de outra organization, mesmo com UUID correto.

## Auditoria de acesso

Login, refresh, logout e login falho auditam com `dominio=identidade`. No núcleo do template a trilha sai por **log estruturado** (slog); o destino assíncrono (ClickHouse) é evolução futura (doc 02). Payload de auditoria montado à mão com identificadores e vocabulário fechado — nunca texto livre (doc 04).
