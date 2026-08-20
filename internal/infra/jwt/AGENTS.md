# AGENTS.md — `internal/infra/jwt`

Emissão e validação de tokens JWT (golang-jwt, stack em `agents/02`).

## Regras

- **`Connect(cfg)` puro e testável** + singleton do processo com
  `sync.Once`. JWT é **FATAL**: chave ausente/fraca no boot derruba o
  processo — API que assina token sem chave confiável não pode subir.
- **Claims**: `sub` (user_uuid), `org` (organization_uuid), `wks`
  (workspace_uuid), `name`, `email`, `typ` (access/refresh). Claim nova
  entra com motivo — o token atravessa a fronteira e tudo nele é público
  para quem o carrega (assinado, não cifrado).
- **TTL vem do config** (access e refresh separados), nunca constante no
  código.
- **Denylist por interface declarada AQUI** (logout/revogação consultam a
  interface; o validador trata ausência da implementação como "nada
  revogado"). A implementação Redis é **evolução futura**, ligada no
  `cmd/bootstrap` — mesmo desenho dos caches de slug do atila.
- A chave secreta nunca aparece em log, erro ou resposta.
- Erros de validação distinguem internamente (expirado, assinatura,
  formato) para log, mas a resposta ao cliente é o 401 genérico do
  `rest_err` — detalhe de por que o token falhou é informação para atacante.

## Definição de pronto

- Testes de emissão/validação: token válido, expirado, assinatura errada,
  claim ausente; denylist exercitada com implementação falsa.
