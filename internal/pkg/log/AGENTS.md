# AGENTS.md — `internal/pkg/log`

Trilhas de log do sistema (evolução #9): **`audit_log`** (uma linha por
escrita dos subdomínios, payload montado à mão) e **`access_log`** (uma linha
por requisição HTTP, emitida pelo middleware global). Ambos são pacotes-FOLHA
(regra 1 de `agents/01`) — conhecem só a forma do evento e a interface.

## Regras

- **A interface é declarada aqui, o destino é ligado no `cmd/bootstrap`**:
  writer assíncrono em lote do ClickHouse (`internal/infra/clickhouse`,
  conformidade estrutural via adaptadores finos de `logs.go`) quando existe;
  `SlogPadrao()` (stdout, mesmo formato do slog legado) quando a dependência
  está degradada. O log `[DEGRADADO]` correspondente sai do Connect do infra.
- **Registrar NUNCA bloqueia nem erra**: telemetria não pode mudar a resposta
  ao cliente. A fila e o descarte contado moram no writer do infra — as folhas
  só definem o contrato e o fallback stdout.
- Payload de auditoria é vocabulário FECHADO (doc 04): identificadores,
  ação estável e detalhes k→v normalizados (`audit_log.Detalhes`) — nunca
  texto livre, nunca segredo. PII que por exceção entra em detalhe (e-mail de
  login falho) chega JÁ mascarada pelo `pkg/pii`.
- Falha de auditoria vira nível WARN no destino stdout; sucesso, INFO.

## Definição de pronto

- Cada folha tem teste próprio sem boot do processo; o destino stdout tem
  saída determinística (detalhes ordenados por chave).
