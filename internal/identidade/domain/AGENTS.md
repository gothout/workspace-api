# AGENTS.md — `internal/identidade/domain`

Subdomínios do domínio **identidade**: `organization`, `workspace`, `user`.

## Regras

- Importa `pkg`, `infra` e `middleware` — **NUNCA** `application`, `cmd` nem
  **subdomínio irmão** (regras 3 e 4 de `agents/01`). Dependência
  entre subdomínios entra por interface declarada no consumidor, ligada no
  `cmd/bootstrap`.
- Todo subdomínio segue a anatomia de **8 arquivos + `permissions.go`**
  (templates em `agents/05`): `dto_request.go`, `dto_response.go`,
  `errors.go`, `permissions.go`, `repository.go`, `service.go`,
  `controller.go`, `singleton.go`. **Quem audita escritas tem também
  `events.go`** (catálogo de eventos de auditoria — ação estável + descrição
  PT-BR + campos do payload; o `auditar()` reprova ação não catalogada e o
  teste de cobertura de eventos reprova nos dois sentidos). **O modelo
  (entidades, VOs, `NewX`, inputs, `ListFilter`, status) mora em
  `internal/identidade/model/{subdominio}` — pacote-folha importável por
  todas as camadas.**
- **Cada subdomínio hospeda um agregado** (ou poucos, com raiz explícita no
  pacote `model/`): a raiz é a única porta de entrada; referência a outro agregado
  **só por uuid** — nunca join de escrita. Importar o **`model/` de irmão é
  permitido para leituras** (ele é folha); escrita em agregado alheio continua
  só por interface/application.
- **Repositório é por agregado**, com métodos em linguagem de negócio.
- **Invariantes vivem no construtor `NewX` e nos métodos de comportamento**
  da entidade — struct literal de entidade fora do pacote é proibida.
  Padrões táticos completos em `agents/01`, templates em `agents/05`.
- **Auth declarada rota a rota** no `Routes()` do controller
  (`RequirePermission("dominio:subdominio:acao")`), nunca no grupo.
- **Auditoria em toda escrita**: quem, quando, o quê, de onde. Leitura não
  audita; escrita sem auditoria não fecha checklist.
- **Catálogo de erros**: toda sentinela do `errors.go` tem entrada no
  catálogo do subdomínio (code estável + mensagem + status), registrada no
  `rest_err` — é o que alimenta `GET /api/system/errors`.
- **Observação de erros** (evolução errobserve): o `NewService` devolve o
  service DECORADO (`service_observado.go`) — todo erro que sobe ao chamador
  vira evento estruturado com o código do catálogo e a severidade declarada
  em `severidadesErros` no `singleton.go` (sentinela nova sem severidade
  reprova no boot). O erro sai INTACTO; sentinela desconhecida vira evento
  critical. Detalhes no `internal/pkg/errobserve/AGENTS.md`.
- **`Catalogo()` de permissões**: `permissions.go` declara as constantes e o
  catálogo com metadados (descrição PT-BR, rota, método, grupo de menu) —
  sem ele o subdomínio não aparece no endpoint de permissões e o checklist
  não fecha.
- Padrão singleton `New/Use/MustUse` conforme `agents/05`.

## Definição de pronto

Checklist de subdomínio do `agents/05` completo, build/vet/test verdes.
