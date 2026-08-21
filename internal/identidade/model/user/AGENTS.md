# AGENTS.md — `internal/identidade/model/user`

Modelo do subdomínio `user` (regras do nível em
`internal/identidade/model/AGENTS.md`).

## Conteúdo

- Entidades `User` (raiz; `identidade_user_user`, `senha_hash` com
  `json:"-"`), `RefreshToken` (`identidade_user_refresh_token`, `jti`
  único), `Atribuicao` (`identidade_user_atribuicao`), `Papel` e
  `PapelPermissao` (globais da plataforma).
- VO `Email` com `ParseEmail` (único por organization — a checagem de
  unicidade é do service, não daqui).
- Construtor `NewUser` validando invariantes; a **comparação de senha é
  método do service do subdomínio**, nunca função deste pacote — credencial
  não recebe comportamento de leitura aqui.
- Sentinelas de invariante do modelo (ex.: `ErrEmailInvalido`).

## O que não entra

DTOs, repository, service (incluindo a comparação de senha), catálogo de
erros, permissões — tudo isso mora em `internal/identidade/domain/user`.
