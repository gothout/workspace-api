# AGENTS.md — `internal/identidade/application/logs`

Leitura das trilhas de log gravadas no ClickHouse (evoluções #9 → E5): as
trilhas de **auditoria** (escritas), **acesso** (requisições HTTP) e
**erros** (errobserve) são catalogadas (E3/E4); esta aplicação é a porta de
CONSULTA delas para o front-end.

## Rotas

`GET /api/application/identidade/logs/{auditoria,acesso,erros}` — cadeia
completa rota a rota (`SetContextAuthorization → ResolveWorkspace →
RequirePermission(identidade:logs:ler)`).

## Modelo de escopo em 3 recortes (decisão da issue #27)

Derivado SEMPRE do ctx — o query param nunca escolhe escopo:

| Recorte | Quem | Alcance |
|---|---|---|
| **Plataforma** | possui `*:*` (super_admin) | QUALQUER organization; filtros opcionais honrados como vieram |
| **Organization** | atende `identidade:logs:ler_organization` (seed: admin_organization) | preso à organization do ctx — qualquer workspace dela |
| **Workspace** | demais com `identidade:logs:ler` (seed: admin_workspace, somente_leitura) | preso ao par (organization, workspace) resolvido |

- Posse do curinga global é conferida por PERTENCIMENTO EXATO (`*:*` tem 2
  segmentos, fora da gramática do `middleware.Atende`) — mesma exceção da
  validação de posse de API keys (R1).
- Filtro apontando FORA do recorte = `ErrForaDoEscopo` (**404**, não 403):
  uuid alheio exista ou não recebe a mesma resposta — não confirma
  existência. Sem organization/workspace exigido no ctx = também fail-closed.
- Chamador sem permissão nenhuma nem chega ao service: `RequirePermission`
  nega na rota (403).

## Regras

- **Degradável por desenho**: ClickHouse ausente ⇒ o adaptador do bootstrap
  (`consultorLogs`, em `cmd/bootstrap/logs.go`) devolve `ErrIndisponivel`,
  que vira **503 padronizado** (`identidade.logs.indisponivel`) — nunca 500
  nem lista vazia silenciosa. O consultor mora no `infra/clickhouse`; aqui
  só a interface `ConsultaTrilhas` o conhece.
- **Sem `model.go` nem `repository.go`** — aplicação não persiste nada. Os
  tipos das linhas vêm das folhas `pkg/log/{audit_log,access_log}` e
  `pkg/errobserve`; o filtro (`clickhouse.FiltroTrilha`) é tipo do próprio
  infra (application→infra é permitido pela regra 5).
- **Filtros**: `organization_uuid` (só tem efeito para a plataforma),
  `workspace_uuid`, `user_uuid`, `acao` (= ação estável na auditoria; =
  código estável nos erros; ignorado no acesso), `ray_trace`, janela
  `inicio`/`fim` em RFC3339 UTC. UUID/timestamp malformado ou janela
  invertida = `ErrFiltroInvalido` (400).
- **Paginação padrão** (`pagination.Pagination`) + ordenação `instante DESC,
  ray_trace DESC`; resposta no envelope `{items, page, page_size, total}`
  com total SEM paginação.
- **Leitura não audita** (doc 04) — nenhum `events.go` aqui.
- **Observação de erros** (errobserve): `NewService` devolve o service
  DECORADO; severidades no `singleton.go` (`fora_do_escopo` = **error**
  — tentativa de alcançar dados alheios é sinal de segurança; demais = warn).
- A trilha de acesso só é recortável porque o E5 adicionou as colunas de
  tenancy a `log_acesso` (`db/logs/0004`) — o middleware global preenche
  DEPOIS da cadeia rodar.
- **Enriquecimento de usuário (UX2, issue #29)**: os três DTOs carregam
  `user_nome`/`user_email`, resolvidos EM LOTE por uuids DISTINTOS da página
  via contrato opcional `ResolvedorUsuarios` (`Dependencias.Usuarios`),
  implementado no bootstrap sobre o repositório do subdomínio user
  (`identidade_user_user` mora no Postgres; a trilha no ClickHouse — join
  entre bancos não existe, lote sim). O resolvedor ESCOPA pela organization
  do ctx quando ela existe (recortes org/workspace); ctx sem organization é
  o caminho da plataforma. Falha do resolvedor NUNCA derruba a consulta —
  linhas seguem com os campos vazios (log warn). Linhas sem usuário
  (sistema/anônimo/removido) saem com `user_nome`/`user_email` vazios.

## Definição de pronto

- Testes table-driven cobrem os três recortes, filtros inválidos, fail-closed
  sem escopo e paginação propagada (dublê de `ConsultaTrilhas`).
- Integração real (postgres+clickhouse efêmeros) prova escrita→leitura com
  recorte, filtro, ordenação e o caminho degradado 503.
- Enriquecimento: unitários provam lote distinto, fantasma vazio, degradação;
  integração real prova o join com Postgres e o recorte do resolvedor.
