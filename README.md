# workspace-api

Template de API gerenciadora em **Go** com hierarquia **organization →
workspace → user**, arquitetura por domínio (DDD tático) e padrão singleton.
Pensado para ser clonado como ponto de partida de SaaS multi-tenant com
white-label para parceiros.

> **Status atual:** especificação completa, código ainda não escrito. A
> implementação segue as [issues do plano de
> execução](https://github.com/gothout/workspace-api/issues) (Fase 0 → 6),
> guiadas pela documentação em `agents/`.

## O modelo

```
Organization ── tem N ──▶ Workspace          (slug DNS: {slug}.{base_domain})
     │                         ▲
     │                         │ atribuição (papel por workspace)
     └── tem N ──▶ User ───────┘
```

- **Organization**: dono do contrato. Pode registrar um **domínio custom**
  (white-label): `*.{dominio-da-org}` resolve os workspaces dela, com
  validação de pertencimento — workspace alheio não resolve no domínio do
  parceiro.
- **Workspace**: unidade de tenancy, identificado por slug único global.
- **User**: pertence à organization; recebe **papéis por workspace** (RBAC
  granular `dominio:subdominio:acao`).

## Arquitetura em uma página

- **Camadas** (`internal/`): `pkg` (folha, utilitários) ← `infra` (Postgres,
  JWT — singletons com `Connect` puro) ← `{dominio}/domain` (subdomínios com
  os 9 arquivos canônicos) ← `{dominio}/application` (casos de uso que cruzam
  subdomínios) ← `cmd` (composição do processo). As regras de dependência são
  **executáveis** via arch-go e conferidas visualmente com o Dependency Graph
  do Go Architect.
- **Singleton**: todo pacote de infra e todo subdomínio segue `New(deps...)` →
  `Use()` → `MustUse()` com `sync.Once`. Postgres/JWT são fatais; futuras
  dependências (Redis, ClickHouse) degradam com log `[DEGRADADO]`.
- **Escopo fail-closed**: toda query de tabela de negócio passa por
  `orgctx.Scope`/`ScopeOrganization` — sem escopo, a query falha.
- **Autorização granular rota a rota**: cada rota declara sua permissão exata
  via `RequirePermission`; cadeia não inicializada = 403, nunca aberta.
- **Mapping para o front-end**: `GET .../permissoes/minhas` devolve a árvore
  do que o usuário pode acessar; `GET /api/system/errors` devolve todos os
  erros possíveis do sistema (code estável + mensagem + status). Sem hardcode
  no front.
- **Migrations**: SQL puro em pares `up`/`down`; `up` automático no boot
  (advisory lock), `down` testado (`up → down → up` em banco efêmero), CLI
  `migrate up|down|status|validate|create`.

## Stack

gin · Postgres (gorm + pgx) · golang-jwt · golang-migrate · cobra/viper ·
swaggo (Swagger) · testify · arch-go. Evoluções planejadas (issues
[#8](https://github.com/gothout/workspace-api/issues/8)–[#10](https://github.com/gothout/workspace-api/issues/10)):
Redis, ClickHouse (logs assíncronos) e observador de erros.

## Estrutura

```
agents/          especificação para quem codifica (ler README.md de lá primeiro)
cmd/             cli, bootstrap (DI) e server
internal/
  pkg/           config, rest_err, pagination, orgctx, validator (folha)
  infra/         database (postgres, migrations), jwt
  middleware/    cadeia de auth/autorização (fail-closed)
  identidade/    DOMÍNIO
    model/       modelos expostos: entidades, VOs, invariantes (folha, importável por todas as camadas)
    domain/      subdomínios: organization, workspace, user
    application/ aplicações: auth, catalogo
db/migrations/   SQL puro up/down
```

Cada pasta tem seu `AGENTS.md` com regras específicas e definição de pronto.

## Como contribuir (loop de execução)

1. Abrir `agents/README.md` e seguir o protocolo.
2. Pegar a issue aberta mais antiga — uma por vez, nunca pular fase.
3. Antes de codar, reler `agents/01`, `agents/04`, `agents/05` e o
   `AGENTS.md` das pastas tocadas.
4. Fechar com `go build ./... && go vet ./... && go test ./...` verdes
   (`-race` se tocar invariante disputada) e registrar no log do `agents/06`.

## Fases

| Issue | Fase |
|---|---|
| [#1](https://github.com/gothout/workspace-api/issues/1) | F0 — Fundação (config, postgres, rest_err, migrations, server, arch-go) |
| [#2](https://github.com/gothout/workspace-api/issues/2) | F1 — Autenticação e autorização granular |
| [#3](https://github.com/gothout/workspace-api/issues/3) | F2 — Subdomínio organization (com domínio custom) |
| [#4](https://github.com/gothout/workspace-api/issues/4) | F3 — Subdomínio workspace (slug, white-label) |
| [#5](https://github.com/gothout/workspace-api/issues/5) | F4 — Subdomínio user + aplicação auth |
| [#6](https://github.com/gothout/workspace-api/issues/6) | F5 — Catálogos do sistema (permissões + mapa de erros) |
| [#7](https://github.com/gothout/workspace-api/issues/7) | F6 — Hardening do template |
| [#8](https://github.com/gothout/workspace-api/issues/8)–[#10](https://github.com/gothout/workspace-api/issues/10) | Evoluções futuras (Redis, ClickHouse, errobserve) |

## Convenções

- Docs, comentários e mensagens de erro em **PT-BR**; tipos e APIs técnicas
  em inglês, vocabulário de negócio em PT-BR (ver `agents/05`).
- Migration aplicada nunca é editada; mudança destrutiva é expand-and-contract.
- Termo de negócio novo entra no glossário do `agents/00` no mesmo commit.

## Deploy no servidor de teste

Scripts prontos em `scripts/ops/` para subir/parar/ver logs do `workspace-api`:

```bash
# Setup completo em um comando (instala Go/OpenCode, clona, compila, instala serviço)
curl -fsSL https://raw.githubusercontent.com/gothout/workspace-api/ox-alpha/code/scripts/ops/setup-server.sh | bash

# 1. Compile
go build -o workspace-api ./cmd/server

# 2. Instala binário, usuário, diretórios e unit do systemd
sudo ./scripts/ops/install-service.sh ./workspace-api

# 3. Edite a config antes de subir
sudo nano /etc/workspace-api/configs.json

# 4. Inicie
sudo ./scripts/ops/start.sh

# 5. Logs em tempo real (Ctrl+C sai, não para o serviço)
./scripts/ops/logs.sh

# 6. Pare
sudo ./scripts/ops/stop.sh
```

Se o servidor **não tiver systemd**, os scripts caem automaticamente para
`nohup` + pidfile em `/var/run/workspace-api.pid` e logs em
`/var/log/workspace-api/workspace-api.log`.

> **Segurança:** o unit roda como usuário `workspace-api` sem privilégios,
> com `ProtectSystem=strict` e `ProtectHome=true`.

## Ralph loop (OpenCode)

A automação orientada por PRD está em `scripts/ralph/`:

- `scripts/ralph/AGENT.md` — instruções para agentes de IA.
- `scripts/ralph/prd.json` — backlog de user stories por fase (issues #1 a #7).
- `scripts/ralph/run-loop.sh` — orquestra o loop de implementação.

Para rodar com **OpenCode**:

```bash
# Dentro da pasta do projeto
opencode run scripts/ralph/run-loop.sh
```

O agente lê a fase atual do `prd.json`, implementa **uma story por vez**, roda
`go build/vet/test`, commita e atualiza o progresso em
`scripts/ralph/progress.txt`.

Branch de teste inicial: `ox-alpha/code`.
