# AGENTS.md — `cmd/bootstrap`

Montagem do processo: ordem de boot, injeção de dependência entre pacotes e
encerramento limpo. Detalhes do desenho em `agents/01` e `agents/05`.

## Ordem de boot (inviolável)

1. `config.Init(path)` — sem config nada mais sobe.
2. Registro das tags do `validator` no gin.
3. Postgres (`InitPostgres`) — **fatal**, erro derruba o processo.
4. JWT (`jwt.Init`) — **fatal** também: API que assina token sem chave
   confiável não pode subir.
5. Redis (`redis.InitRedis`) — **DEGRADÁVEL** (evolução #8): desabilitado ou
   inacessível NUNCA derruba o boot (log `[DEGRADADO]`); os adaptadores de
   `cache_redis.go` tratam a ausência como no-op honesto.
6. ClickHouse + trilhas de log (`logs.go`) — **DEGRADÁVEL** também (evolução
   #9): sem banco, auditoria e acesso saem pelo stdout; com banco, o writer em
   lote consome as TRÊS trilhas fora do caminho síncrono do request
   (auditoria, acesso e ERROS observados — evolução errobserve). Os três
   subdomínios e o auth recebem a trilha de auditoria por `ComTrilha`; o
   engine recebe a de acesso em `routes.Opcoes.AcessoLog`. O mesmo arquivo
   define os SINKS do observador de erros (slog sempre ativo + alerta
   agregado + writer ClickHouse) e observa a degradação como evento de
   plataforma. O `Close` DRENA o writer no fechamento LIFO.
7. Migrations: `up` automático quando `migrations.auto_run` (advisory lock do
   Postgres impede réplicas de correrem juntas; arquivos `-- manual` são
   ignorados aqui, com log de alerta); falha aqui também é fatal — e vira
   EVENTO DE PLATAFORMA critical (`sistema.migrations.up`, via
   `errobserve.go`).
8. `middleware.New(...)`: liga os contratos via **adaptadores que resolvem na
   chamada** — sobe **antes do registro de rotas** porque o `Routes()` dos
   controllers consome a cadeia, e antes dos domínios porque não conhece o
   concreto deles.
9. `InitDomains`: `New(deps...)` de cada subdomínio em ordem de dependência
   explícita, uma linha de log `[BOOTSTRAP-DI]` por subdomínio. O workspace
   recebe o cache de resolução Redis; o user, o observador de invalidação
   de permissões (ambos de `cache_redis.go`, no-op sem Redis); os três e o
   auth recebem a trilha de auditoria (#9). As aplicações auth (contratos
   resolvidos na chamada), logs (E5 — leitura das trilhas; o adaptador
   `consultorLogs` em `logs.go` resolve o consultor do ClickHouse NA
   CHAMADA e devolve 503 padronizado quando degradado) e catalogo
   (agregadores dos catálogos) fecham a montagem.
10. Engine HTTP (`cmd/server/routes` + `cmd/server`) e subida do servidor.

## Regras

- **DI por adaptadores que resolvem `MustUse()` NA CHAMADA**, não na montagem:
  o adaptador guarda a função de resolução e só a invoca quando a dependência
  é usada. Resolver na montagem congelaria a ordem de boot e esconderia
  dependência não declarada.
- Toda interface entre pacotes (`contratos.go` de middleware e das
  applications, revogação composta do JWT, caches Redis, limitador de login,
  destinos das trilhas de log, sinks do observador de erros, agregadores dos
  catálogos de permissões e eventos da aplicação `catalogo`) é ligada
  **aqui** — um arquivo por frente (`middleware.go`, `organizacao.go`,
  `workspace.go`, `usuario.go`, `catalogo.go`, `seed.go`, `cache_redis.go`,
  `logs.go`, `errobserve.go`, `mapa_erros.go`, `provisionamento_app.go`).
- **Namespace reservado da plataforma**: só este pacote registra eventos em
  `sistema.*` (observador de plataforma em `errobserve.go`) — migrations
  falha e dependência degradada viram eventos; subdomínio de negócio NUNCA.
- Fechamento **LIFO**: o que subiu por último desce primeiro (servidor →
  domínios → pool do Postgres). Cada `Init` registra seu `Close`.
- `MustUse()` é restrito a este pacote; fora daqui usa-se `Use()` com erro
  tratado (rotas fecham, nunca abrem).

## Definição de pronto

- Boot completo loga a ordem acima; derrubar qualquer dependência fatal
  impede o boot com mensagem clara.
- `InitDomains` fora de ordem é evidente no log (`[BOOTSTRAP-DI]`).
- Sem `middleware.New`, toda rota protegida responde **403** (fail-closed) —
  nunca pânico, nunca rota aberta.
