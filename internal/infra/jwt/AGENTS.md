# AGENTS.md — `internal/infra/jwt`

Emissão e validação de tokens JWT (golang-jwt, stack em `agents/02`).

## Regras

- **`Connect(cfg)` puro e testável** + singleton do processo com
  `sync.Once`. JWT é **FATAL**: chave ausente/fraca no boot derruba o
  processo — API que assina token sem chave confiável não pode subir.
  **Mínimo de 32 bytes no segredo** (R6): HS256 exige chave de 256 bits —
  abaixo disso a assinatura é força-brutável; `Connect` recusa com erro
  claro citando o mínimo.
- **Claims**: `sub` (user_uuid), `org` (organization_uuid), `wks`
  (workspace_uuid), `name`, `email`, `typ` (access/refresh) e `jti`
  (**único por refresh** — é a chave da revogação persistida). Claim nova
  entra com motivo — o token atravessa a fronteira e tudo nele é público
  para quem o carrega (assinado, não cifrado).
- **TTL vem do config** (access e refresh separados), nunca constante no
  código.
- **Revogação persistida no Postgres**: cada refresh tem `jti` único
  gravado em `identidade_user_refresh_token` (subdomínio `user`); o logout
  marca `revogado_em` e o validador confere a revogação via **interface
  declarada AQUI** (ausência da implementação = "nada revogado"). A
  implementação Redis é **evolução futura**, ligada no `cmd/bootstrap`, e
  vira só **cache dessa revogação** — nunca a fonte da verdade.
  `Validar` é a porta de quem CONCEDE acesso (consulta o revogador);
  `ValidarAssinatura` é a porta **EXCLUSIVA do logout idempotente** (R5) —
  lê as claims sem a denylist para o token já revogado ter sua revogação
  confirmada em vez de recusada. Nunca usar em caminho que concede acesso.
- A chave secreta nunca aparece em log, erro ou resposta.
- Erros de validação distinguem internamente (expirado, assinatura,
  formato) para log, mas a resposta ao cliente é o 401 genérico do
  `rest_err` — detalhe de por que o token falhou é informação para atacante.

## Definição de pronto

- Testes de emissão/validação: token válido, expirado, assinatura errada,
  claim ausente; denylist exercitada com implementação falsa.
