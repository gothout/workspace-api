# AGENTS.md — `internal/domain/identidade/organization`

Raiz da hierarquia: a dona do contrato. Tabela `identidade_organization_organization`,
rotas em `/api/domain/identidade/organizations`.

## Convenções

- **Tabela SEM escopo acima — exceção documentada.** É ela quem define o
  escopo de todo o resto; não há `organization_uuid` para filtrar. Motivo
  registrado aqui e em `internal/domain/identidade/AGENTS.md`. Consequência:
  toda rota daqui exige permissão própria e a listagem é restrita a
  `super_admin`/suporte auditado — nunca aberta a admin de workspace.
- **Campo `dominio` custom OPCIONAL e único global** (white-label): quando
  preenchido (ex.: `parceiro.com`), `*.{dominio}` resolve os workspaces
  **dela** — o `ResolveWorkspace` valida o pertencimento antes de resolver
  o slug. Formato validado; domínio removido (soft delete) **não** libera o
  valor para outra organization enquanto houver linha — índice único
  **parcial** (`WHERE deleted_at IS NULL`) onde houver `deleted_at`, salvo
  motivo escrito em contrário (ver a discussão de slug em
  `identidade/workspace/AGENTS.md`).
- **Inativar suspende os workspaces e desativa a resolução do domínio**:
  operação em cascata dentro do service, auditada, e o efeito no middleware
  é imediato na próxima requisição.
- `gerenciar_dominio` é permissão **separada** de `editar`: apontar um
  domínio muda onde a plataforma responde — não é "editar um campo".

## Permissões (`permissions.go` + `Catalogo()`)

`identidade:organization:{criar, ler, editar, remover, gerenciar_dominio}` —
declaradas rota a rota, nunca no grupo.

## Definição de pronto

- Checklist do `agents/05` completo; migration `up → down → up` verde;
  cascata de inativação e unicidade do `dominio` cobertas por teste.
