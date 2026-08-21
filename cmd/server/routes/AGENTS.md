# AGENTS.md — `cmd/server/routes`

Monta o `gin.Engine`: middlewares globais, rotas de sistema e o registro das
rotas de cada subdomínio. Convenções de API em `agents/04`.

## Regras

- **Middlewares globais, nesta ordem**: access log (evolução futura — o slot
  já fica reservado na cadeia) → `recovery` → CORS. O CORS aceita
  `*.{base_domain}` da plataforma **e** os domínios custom registrados pelas
  organizations (white-label) — origem fora dessa lista é recusada. Origens
  exatas extras vêm de `server.http.cors.allowed_origins` (casamento por
  host parseado — regra no `agents/03`).
- **`NoRoute`** responde o 404 padronizado do `rest_err` — rota desconhecida
  nunca devolve o corpo padrão do gin.
- Rotas de sistema: `GET /api/status` (sondas injetadas pelo bootstrap) e
  Swagger em `/doc`. `GET /api/system/errors` é registrada pela aplicação
  `identidade/application/catalogo`, não aqui.
- Grupos base: `/api/domain` (subdomínios de negócio) e `/api/application`
  (orquestrações). O controller de cada subdomínio pendura suas rotas no
  grupo certo.
- **Registro via helper** `registrarRotas(nome, subdominio.Use)`: usa `Use()`
  (não `MustUse()`); se o subdomínio não foi inicializado pelo
  `InitDomains`, a rota **não sobe** e o motivo sai no log — nunca pânico.
  Engine montado por teste não passa pelo boot e precisa continuar
  construível.
- **Autorização NUNCA no grupo**: cada rota declara sua cadeia
  (`RequirePermission("dominio:subdominio:acao")`) no `Routes()` do
  controller. Auth no grupo esconderia a exigência de quem lê a rota.

## Definição de pronto

- Engine sobe sem nenhum subdomínio inicializado (só rotas de sistema) e o
  log lista o que ficou de fora e por quê.
