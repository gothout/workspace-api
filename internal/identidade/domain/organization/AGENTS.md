# AGENTS.md — `internal/identidade/domain/organization`

Raiz da hierarquia: a dona do contrato. Tabelas
`identidade_organization_organization` e `identidade_organization_apikey`,
rotas em `/api/domain/identidade/organizations`.

## Convenções

- **Tabela SEM escopo acima — exceção documentada.** É ela quem define o
  escopo de todo o resto; não há `organization_uuid` para filtrar. Motivo
  registrado aqui e em `internal/identidade/AGENTS.md`. Consequência:
  toda rota daqui exige permissão própria e a listagem é restrita a
  `super_admin`/suporte auditado — nunca aberta a admin de workspace. As
  permissões `identidade:organization:*` ficam **fora do seed de
  `admin_organization`** (o curinga dele cobre workspace/user) — única
  exceção: `gerenciar_apikeys`, que ele precisa para gerir as chaves da
  própria organization.
- **Campo `dominio` custom OPCIONAL e único global** (white-label): quando
  preenchido (ex.: `parceiro.com`), `*.{dominio}` resolve os workspaces
  **dela** — o `ResolveWorkspace` valida o pertencimento antes de resolver
  o slug. A validação (`ParseDominio`) **rejeita**: igual ao `base_domain`
  da plataforma, ascendente ou descendente dele, e public suffix — exige
  eTLD+1 no mínimo. Unicidade por índice único **TOTAL**: domínio removido
  (soft delete) **não** libera o valor para outra organization — evita
  takeover de domínio por outro tenant (exceção documentada à regra de
  índice parcial do `agents/02`; o Postgres aceita múltiplos NULLs, então
  o único total funciona com `dominio` opcional). A mesma política vale
  para o slug — ver `internal/identidade/domain/workspace/AGENTS.md`.
- **Chaves de API** (`identidade_organization_apikey`): a chave é um token
  aleatório exibido **uma única vez** na criação; o banco guarda só o
  **SHA-256** dele (`key_hash`). Escopo: a organization inteira ou uma
  lista de workspaces dela, com permissões explícitas. Gestão em
  `/api/domain/identidade/organizations/{uuid}/api-keys` — nunca na raiz do
  domínio. **A chave não concede o que o criador não tem** (R1): cada
  permissão pedida é casada contra as efetivas do ctx com o matcher do
  middleware (`Atende`) — recusa `ErrPermissaoNaoPossuida` (403) antes da
  persistência; `*:*` só vale para quem possui `*:*`.
- **Inativar suspende os workspaces e desativa a resolução do domínio**:
  operação em cascata dentro do service, auditada, e o efeito no middleware
  é imediato na próxima requisição. A cascata vai por **interface declarada
  no `contratos.go` da organization**, ligada no `cmd/bootstrap` — nunca
  chamada direta ao subdomínio irmão (regra 4 de `agents/01`).
- `gerenciar_dominio` é permissão **separada** de `editar`: apontar um
  domínio muda onde a plataforma responde — não é "editar um campo".

## Permissões (`permissions.go` + `Catalogo()`)

`identidade:organization:{criar, ler, editar, remover, gerenciar_dominio, gerenciar_apikeys}` —
declaradas rota a rota, nunca no grupo.

## Definição de pronto

- Checklist do `agents/05` completo; migration `up → down → up` verde;
  cascata de inativação e unicidade do `dominio` cobertas por teste.
