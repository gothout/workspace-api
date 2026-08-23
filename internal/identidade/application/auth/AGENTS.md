# AGENTS.md — `internal/identidade/application/auth`

Caso de uso de **autenticação**: `login` / `refresh` / `logout`. O login
cruza `user` (credencial) com a **resolução da organization pelo Host** —
é orquestração, não regra de um subdomínio só; por isso mora em
`application/`, não em `domain/user`.

## Rotas

`POST /api/application/identidade/auth/{login,refresh,logout}` — fora da
cadeia completa de autorização: rodam **só com a resolução da organization
pelo Host** (subdomínio de workspace ou domínio custom), **sem exigir
vínculo nem permissão** — login é como se consegue a identidade. Host que
não resolve organization nenhuma = o **mesmo 401 genérico** de credenciais
inválidas.

## Regras

- **Observação de erros** (evolução errobserve): o `NewService` devolve o
  service DECORADO (`service_observado.go`) — todo erro que sobe ao chamador
  vira evento estruturado com o código do `errors.go` e a severidade de
  `severidadesErros` no `singleton.go` (login bloqueado = error; demais
  falhas esperadas = warn). O erro sai INTACTO — a indistinguibilidade do
  login não é tocada. Detalhes no `internal/pkg/errobserve/AGENTS.md`.
- **Sem `model.go` nem `repository.go`** — aplicação não persiste nada
  próprio. O refresh token persistido (`identidade_user_refresh_token`) é
  tabela do subdomínio `user`, acessada por contrato.
- Dependências por **`contratos.go`** com interfaces estreitas, ligadas no
  `cmd/bootstrap` por adaptadores que resolvem o singleton **na chamada**:
  - `Usuarios` — buscar usuário por e-mail **dentro da organization
    resolvida** e comparar senha (a comparação é método do service do
    `user` — a credencial nunca sai do subdomínio);
  - `EmissorToken` — emissão/validação de JWT (`infra/jwt`), incluindo o
    `jti` único do refresh;
  - `ResolvedorOrganization` — a organization dona do domínio (a mesma
    fonte do `ResolveWorkspace` do middleware).
- **Resposta indistinguível**: "usuário não existe", "senha errada" e
  "organization não resolvida" devolvem o mesmo `code`, mensagem e 401 — e
  a comparação de hash roda também quando o usuário não existe (hash de
  mentira), para não vazar existência por timing.
- **Logout revoga no Postgres**: marca `revogado_em` na linha do refresh
  (via contrato `Usuarios`); a denylist Redis (#8) é CACHE dessa revogação —
  composta no bootstrap, só positivo cacheado, negativos sempre à fonte.
- **Logout é IDEMPOTENTE (R5)**: não passa pela mesma porta do refresh —
  confere assinatura/tipo/claims via `ValidarSemRevogacao` e chama o
  `EncerrarSessao` (idempotente no subdomínio) sem exigir jti ativo, conta
  autenticável nem dona viva: destruição nunca concede acesso. Repetir o
  logout do mesmo token é sucesso; token expirado/malformado segue o 401
  genérico.
- **Refresh tem ROTAÇÃO (R5)**: renovação revoga o `jti` anterior ANTES de
  emitir o par novo — reuso de refresh renovado falha fechado; se a emissão
  falhar depois da revogação, a sessão morre (o dono re-loga): direção
  segura, nunca dois refresh válidos coexistem.
- **Rate-limit/lockout de login (evolução #8)**: contrato `LimitadorLogin`
  (opcional nas Dependências — nil = feature desligada). Par (e-mail, IP)
  preso responde 429 (`identidade.auth.login_bloqueado`) ANTES do caminho de
  autenticação; falha de credencial alimenta o contador e login bom limpa o
  histórico. Falha do PRÓPRIO limitador nunca recusa login — segue sem
  lockout com log: cache fora do ar não vira indisponibilidade de autenticação.
  Implementação Redis vive em `internal/infra/redis` + adaptador no bootstrap.

## Definição de pronto

- Testes com dublês dos três contratos provam: login bom emite o par de
  tokens; as três falhas acima são indistinguíveis (corpo e tempo);
  refresh com `jti` revogado falha; logout persiste a revogação.
