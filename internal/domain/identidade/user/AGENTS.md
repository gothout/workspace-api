# AGENTS.md — `internal/domain/identidade/user`

Usuário da plataforma, com papéis atribuídos **por workspace**. Tabelas
`identidade_user_user` e `identidade_user_atribuicao`; rotas
`/api/domain/identidade/users` + auth (`login`/`refresh`/`logout`).

## Convenções

- **Escopo por workspace** (`orgctx.Scope`) em todas as queries de
  administração — sem escopo a query falha.
- **A credencial NÃO sai do subdomínio**: hash com `json:"-"`, nunca entra
  em DTO de resposta, e a comparação de senha é **método do service** —
  controller nenhum toca em hash.
- **A resposta de autenticação não distingue "não existe" de "senha
  errada"** — nem no erro (mesmo `code`, mesma mensagem, mesmo 401) nem no
  **tempo** (comparação de hash roda também quando o user não existe, contra
  hash de mentira, para não vazar existência por timing).
- **Papéis são POR workspace**: a tabela de atribuição liga user → papel →
  workspace. `atribuir_papel` é permissão separada de `editar`: dar poder a
  alguém não é "editar um campo". O conjunto de papéis seed está em
  `agents/03` (`super_admin`, `admin_organization`, `admin_workspace`,
  `usuario_workspace`, `somente_leitura`).
- Rotas de auth (`login`/`refresh`/`logout`) são as únicas **sem** a cadeia
  completa de autorização — login é como se consegue a identidade.
  Rate-limit e lockout são evolução futura; registrar a ausência no log de
  iterações (`agents/06`).

## Permissões (`permissions.go` + `Catalogo()`)

`identidade:user:{criar, ler, editar, remover, atribuir_papel}` — rota a
rota.

## Definição de pronto

- Checklist do `agents/05`; testes provam que o hash não vaza em nenhuma
  serialização e que login com user inexistente e com senha errada devolvem
  resposta indistinguível.
