# AGENTS.md — `internal/infra/redis`

Adaptador da dependência **DEGRADÁVEL** de cache/lockout distribuído
(evolução #8). Redis é a única dependência que NUNCA derruba o processo:
`Connect` não errа — cliente vivo quando o servidor responde, **cliente nil
com log `[DEGRADADO]`** quando desabilitado/inacessível; o consumidor é
obrigado a tratar a ausência.

## Regras

- Importa só `internal/pkg` + libs externas — nunca outro `infra`, nem em
  teste (regra 2 de `agents/01`). A conformidade com contratos de outros
  pacotes (ex.: `jwt.RevogadorDeRefresh`) é **estrutural** — sem import; a
  ligação acontece no `cmd/bootstrap`.
- **Fonte da verdade é sempre o Postgres.** Nada aqui guarda dado que não
  exista lá: denylist é cache da revogação persistida, caches de leitura
  são cópias com TTL curto obrigatório + invalidação ativa.
- Par função pura + singleton: `Connect(cfg)` puro (testes usam só ele);
  `InitRedis`/`Get`/`Close`/`Disponivel` do processo — mutex cobre o
  `once.Do` INTEIRO (lição R7: reset de teste disputa o Once).
- **Prefixos de chave fixos** (`chaves.go`) — dono, TTL e invalidação
  documentados em cada um:

| Prefixo | Formato | Dono / TTL |
|---|---|---|
| `perm:` | `perm:{org}:{user}:{wks}` | cache de permissões efetivas (bootstrap); TTL `cache.ttl_permissoes_seg`; invalidado pelo contrato `ObservadorAtribuicoes` do user |
| `workspace:slug:` | `workspace:slug:{slug}` | cache de resolução do Host (contrato `CacheResolucao` do workspace); TTL `cache.ttl_resolucao_seg`; invalidação ativa nas escritas |
| `lock:` | `lock:c:{hash}` / `lock:b:{hash}` | contador de falhas e marcador de bloqueio do lockout de login; e-mail+IP hasheados (PII não repousa crua) |
| `idempot:` | `idempot:{chave}` | RESERVADO — nenhum código grava hoje |
| `jwt:deny:` | `jwt:deny:{jti}` | denylist do refresh; só POSITIVO é cacheado (revogação é permanente), negativos seguem ao Postgres — logout/rotação valem na hora |

## Peças

- `Denylist` (`denylist.go`): implementa estruturalmente
  `RevogadorDeRefresh` do `infra/jwt`. O bootstrap compõe cache + fonte:
  hit positivo responde na hora; miss/falha cai ao Postgres e, se lá for
  revogado, grava o positivo.
- `LimitadorLogin` (`lockout.go`): contador de falhas por (e-mail, IP) na
  janela; teto estourado aplica bloqueio temporário. Sem Redis = sem
  lockout (no-op), nunca login quebrado.

## Definição de pronto

- Teste unitário do `Connect` cobre os três estados (desabilitado,
  inacessível, disponível via integração efêmera) SEM singleton.
- UM teste único exercita o ciclo do singleton (`sync.Once` não se desfaz
  entre casos; quem re-boota chama `ResetarParaTeste` antes).
- Integração com Redis efêmero (testcontainers, skip sem docker) cobre
  denylist, lockout e os caches ligados no bootstrap.
