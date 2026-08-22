# AGENTS.md — `cmd/bootstrap`

Montagem do processo: ordem de boot, injeção de dependência entre pacotes e
encerramento limpo. Detalhes do desenho em `agents/01` e `agents/05`.

## Ordem de boot (inviolável)

1. `config.Init(path)` — sem config nada mais sobe.
2. Registro das tags do `validator` no gin.
3. Postgres (`InitPostgres`) — **fatal**, erro derruba o processo.
4. JWT (`jwt.Init`) — **fatal** também: API que assina token sem chave
   confiável não pode subir.
5. Migrations: `up` automático quando `migrations.auto_run` (advisory lock do
   Postgres impede réplicas de correrem juntas; arquivos `-- manual` são
   ignorados aqui, com log de alerta); falha aqui também é fatal.
6. `middleware.New(...)`: liga os contratos via **adaptadores que resolvem na
   chamada** — sobe **antes do registro de rotas** porque o `Routes()` dos
   controllers consome a cadeia, e antes dos domínios porque não conhece o
   concreto deles.
7. `InitDomains`: `New(deps...)` de cada subdomínio em ordem de dependência
   explícita, uma linha de log `[BOOTSTRAP-DI]` por subdomínio.
8. Engine HTTP (`cmd/server/routes` + `cmd/server`) e subida do servidor.

## Regras

- **DI por adaptadores que resolvem `MustUse()` NA CHAMADA**, não na montagem:
  o adaptador guarda a função de resolução e só a invoca quando a dependência
  é usada. Resolver na montagem congelaria a ordem de boot e esconderia
  dependência não declarada.
- Toda interface entre pacotes (`contratos.go` de middleware e das
  applications, revogação de refresh do JWT, caches futuros) é ligada
  **aqui** — um arquivo por frente (`middleware.go`, `organizacao.go`,
  `workspace.go`, `usuario.go`, `catalogo.go`, `seed.go`).
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
