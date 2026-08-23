# AGENTS.md — `internal/infra/clickhouse`

Adaptador da dependência **DEGRADÁVEL** de logs assíncronos (evolução #9):
trilhas de auditoria, acesso e ERROS observados (evolução errobserve) escritas
em lote no ClickHouse, FORA do caminho síncrono do request.

## Regras

- Importa só `internal/pkg` + libs externas — nunca outro `infra` (regra 2 de
  `agents/01`). Os tipos de evento vêm de
  `pkg/log/{audit_log,access_log}` e do `pkg/errobserve` (folhas); a
  conformidade com as interfaces `Destino`/`Sink` delas é **estrutural**
  na cabeça do writer, e os adaptadores finos que implementam cada interface
  moram no `cmd/bootstrap/logs.go` (Go não sobrecarrega métodos: um adaptador
  por trilha).
- **ClickHouse NUNCA derruba o processo**: `Connect(cfg)` puro não erra —
  conexão viva quando o servidor responde, **NIL com log `[DEGRADADO]`**
  quando desabilitado/inacessível. Quem trata a ausência é o bootstrap: trilhas
  caem para o stdout (`SlogPadrao()` das pkg/log).
- Par função pura + singleton: `Connect`/`NovaEscritor` puros (testes usam só
  eles); `InitClickhouse`/`Use`/`Close` com mutex cobrindo o `once.Do` INTEIRO
  (lição R7). `Close` DRENA o writer antes de fechar a conexão.
- **Writer em lote (`escritor.go`) é o contrato de performance** (três filas:
  acesso, auditoria e erros — mesmas regras para todas):
  - fila limitada por trilha; enfileirar é O(1) **não-bloqueante**;
  - flush por **tamanho de lote** OU pela **janela** (`logs.lote_*`);
  - **fila cheia DESCARTA E CONTA** (`Descartes()`) — telemetria perde linha,
    API jamais trava;
  - erro de gravação PERDE o lote COM contagem e log (`FalhasGravacao()`) —
    retry no worker só cresceria a fila (descarte em cascata);
  - `Fechar()` drena filas + lotes parciais dentro de `logs.drain_timeout_sec`
    (estourou: o resto se perde COM LOG — shutdown não fica refém de banco).
- Gravação via INSERT nativo em lote (`PrepareBatch`/`Send`) sobre as tabelas
  do DDL versionado em `db/logs/` — colunas na ordem exata dos `Append`.
- `detalhes` da auditoria vai como JSON string com chaves ordenadas
  (`jsonDeterministico`) — saída comparável linha a linha.
- Credencial nunca aparece em log nem em mensagem de erro.

## Definição de pronto

- Testes unitários do Connect cobrem desabilitado/inacessível SEM singleton;
  testes do escritor cobrem flush por tamanho, por janela, fila cheia contada,
  drain no shutdown, idempotência do Fechar e falha de gravação contada.
- UM teste único exercita o ciclo do singleton; quem re-boota chama
  `ResetarParaTeste` antes.
- Integração com ClickHouse efêmero (testcontainers, skip sem docker) aplica o
  DDL real de `db/logs/` e prova as TRÊS trilhas gravadas após o drain.
