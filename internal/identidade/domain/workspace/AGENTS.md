# AGENTS.md — `internal/identidade/domain/workspace`

Filho da organization, dono do endereço público `{slug}.{base_domain}`.
Tabela `identidade_workspace_workspace`, rotas em
`/api/domain/identidade/workspaces`.

## Convenções

- **Escopo por organization**: o repository usa `orgctx.ScopeOrganization`,
  nunca `Scope` — esta tabela está *acima* do workspace que ela define.
  Consulta sem organization no contexto **falha** (fail-closed).
- **`organization_uuid` vem do contexto, nunca do corpo** — aceitar do
  cliente deixaria um admin criar workspace na organization alheia.
- **Slug único GLOBAL** — necessário porque `{slug}.{base_domain}` é o
  endereço da plataforma inteira (e `*.{dominio-custom}` o do parceiro).
  Regex de formato + **lista de reservados**: `www`, `api`, `app`, `admin`,
  `docs`, `status`, `mail`, `suporte`, `painel`. A lista mora aqui — o
  middleware pergunta a este subdomínio, não o contrário. Unicidade por
  índice único **TOTAL**: slug removido (soft delete) **não** se libera —
  evita takeover de endereço por outro tenant (exceção documentada ao
  `agents/02`, mesma política do `dominio` custom da organization).
- **`FindBySlug`/`SlugOcupado` são a exceção de escopo — obrigatória.** A
  resolução parte do `Host`, que não diz de qual organization o slug é:
  o `ResolveWorkspace` precisa descobrir isso *antes* de existir escopo, e
  então **valida que o workspace pertence à organization dona do domínio**
  (plataforma ou custom). Workspace alheio no domínio do parceiro = **404**.
  O resultado dessas consultas nunca é exposto em rota de administração.
- **Cache de slug futuro entra por interface** declarada aqui
  (implementação Redis na evolução, ligada no bootstrap) — ausência de cache
  é operação normal, só mais cara. Cache com **TTL curto obrigatório** e
  **invalidação ativa** em inativação de organization/workspace e troca de
  `dominio`; a invariante "filho nunca mais vivo que o pai" ganha teste na
  evolução (ver `agents/02`).

## Permissões (`permissions.go` + `Catalogo()`)

`identidade:workspace:{criar, ler, editar, remover}` — rota a rota.

## Definição de pronto

- Checklist do `agents/05`; testes de unicidade global do slug (regex +
  reservados, nos dois sentidos) e do isolamento por organization.
