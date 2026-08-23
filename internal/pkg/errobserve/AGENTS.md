# AGENTS.md — `internal/pkg/errobserve`

Observador de **ERROS por subdomínio** (evolução errobserve): todo retorno de
erro do service passa por `obs.Observe(ctx, err)` — que devolve o erro
**INTACTO** — e vira evento estruturado de telemetria entregue aos sinks
ligados no `cmd/bootstrap`. Telemetria nunca muda a resposta ao cliente.

## Regras

- **A fonte dos códigos é o catálogo do `errors.go`** (doc 05, registrado no
  `rest_err`): o singleton do subdomínio monta as entradas com
  `DoCatalogo(errorCatalog, severidades)` e declara SÓ a severidade de cada
  sentinela (`severidadesErros`, no `singleton.go`). Sentinela catalogada sem
  severidade PANICA no boot/import — observação parcial é mapping pela metade.
- **Severidade é definida POR CATÁLOGO**, nunca inferida em runtime:
  `warn` = recusa esperada de negócio (4xx); `error` = sinal operacional
  relevante (segurança — escalação/lockout; 5xx catalogado); `critical` =
  sentinela DESCONHECIDA ou falha grave.
- **Sentinela fora do catálogo do subdomínio vira evento desconhecido**
  (`CodigoDesconhecido`) e sobe como critical — o pior caso até prova em
  contrário. O erro original segue intacto ao chamador.
- **Namespace reservado `sistema.*`**: eventos de PLATAFORMA (boot,
  migrations.up, shutdown, degradação) são exclusividade do bootstrap via
  `ObservadorPlataforma`. Subdomínio de negócio que tentar dominio `sistema`
  ou código com prefixo `sistema.` reprova em boot/teste (pânico do
  `For`/`Novo`). O vocabulário fixo da plataforma mora AQUI
  (`CatalogoSistema()`) — visível nos catálogos mesmo sem emissão.
- **Registro global com auto-registro**: `For(dominio, subdominio, entradas)`
  no singleton inscreve o observador (duplicado com OUTRA instância =
  pânico). O agregado sai de `CatalogoGlobal()` em ordem determinística para
  GET /api/system/eventos e para a CLI `workspace-api errors`.
- **Sinks plugáveis, slog SEMPRE ativo**: `DefinirSinks(extras...)` mantém o
  `SlogPadrao()` como primeiro destino estruturalmente (degradação nunca fica
  sem saída). ClickHouse da E2 entra como sink quando ligado (terceira trilha,
  adaptador fino em `cmd/bootstrap/logs.go`). Sink de ALERTA (`Alerta(janela)`)
  agrega críticos do mesmo código por janela (`logs.alerta_janela_seg`,
  default 60s): incidente grita UMA vez por janela, contagem sai no próximo.
- **Contrato do sink**: `Registrar` NUNCA bloqueia além do custo de
  enfileirar e NUNCA devolve erro — e precisa ser seguro para uso
  concorrente. O despachante ainda recupera pânico de cada entrega (falha
  contada em `FalhasSink()`), última linha de defesa.
- Pacote-FOLHA (regra 1 de `agents/01`): importa apenas outros pacotes de
  `internal/pkg` (`orgctx`, `rest_err`).

## Definição de pronto

- Testes cobrem validação (namespace reservado nos dois sentidos), casamento
  por `errors.Is` com wrapping, desconhecido/critical, nil sem emissão,
  pânico de sink contado, agregação/janela do alerta e disputa `-race`.
- Cobertura ponta a ponta da DECORAÇÃO (service real → observador → sink) no
  `cmd/bootstrap/errobserve_cobertura_test.go`.
