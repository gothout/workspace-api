# AGENTS.md — `internal/identidade/domain/user`

Usuário da plataforma, com papéis atribuídos **por workspace**. Tabelas
`identidade_user_user`, `identidade_user_refresh_token`,
`identidade_user_atribuicao` (+ as globais `identidade_user_papel` e
`identidade_user_papel_permissao`); rotas `/api/domain/identidade/users`.
A autenticação (`login`/`refresh`/`logout`) **não mora aqui**: é aplicação
irmã em `internal/identidade/application/auth`.

## Convenções

- **Escopo por organization** (`orgctx.ScopeOrganization`) nas queries de
  administração — `identidade_user_user` não tem `workspace_uuid`; listagem
  POR workspace é via `atribuicao`. Sem escopo a query falha.
- **A credencial NÃO sai do subdomínio**: hash com `json:"-"`, nunca entra
  em DTO de resposta, e a comparação de senha é **método do service** —
  exposta à aplicação `auth` por **interface** (contratos ligados no
  bootstrap), nunca pelo pacote. Controller nenhum toca em hash.
- **Refresh token persistido** (`identidade_user_refresh_token`): uma linha
  por `jti` com `expira_em` e `revogado_em`; o logout revoga marcando
  `revogado_em`. A denylist Redis da evolução é só cache dessa revogação.
- **A resposta de autenticação não distingue "não existe" de "senha
  errada"** — nem no erro (mesmo `code`, mesma mensagem, mesmo 401) nem no
  **tempo** (comparação de hash roda também quando o user não existe, contra
  hash de mentira, para não vazar existência por timing). A regra é
  garantida pelo service e consumida pela aplicação `auth`.
- **Papéis são POR workspace**: a tabela de atribuição liga user → papel →
  workspace. `atribuir_papel` é permissão separada de `editar`: dar poder a
  alguém não é "editar um campo". O conjunto de papéis seed está em
  `agents/03` (`super_admin`, `admin_organization`, `admin_workspace`,
  `usuario_workspace`, `somente_leitura`). As tabelas `papel`/
  `papel_permissao` são **globais da plataforma** — sem coluna de escopo
  (exceção documentada: papéis seed globais, criadas na F1).
- As rotas de auth (`login`/`refresh`/`logout`) moram na aplicação irmã e
  rodam **fora da cadeia completa** — só com a resolução da organization
  pelo Host. Rate-limit e lockout são evolução futura; registrar a ausência
  no log de iterações (`agents/06`).

## Permissões (`permissions.go` + `Catalogo()`)

`identidade:user:{criar, ler, editar, remover, atribuir_papel}` — rota a
rota.

## Definição de pronto

- Checklist do `agents/05`; testes provam que o hash não vaza em nenhuma
  serialização e que login com user inexistente e com senha errada devolvem
  resposta indistinguível.
