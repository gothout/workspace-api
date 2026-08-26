# AGENTS.md — `internal/identidade/domain/workspace`

Filho da organization, dono do endereço público `{slug}.{base_domain}`.
Tabela `identidade_workspace_workspace`, rotas em
`/api/domain/identidade/workspaces`.

## Convenções

- **Escopo por organization**: o repository usa `orgctx.ScopeOrganization`,
  nunca `Scope` — esta tabela está *acima* do workspace que ela define.
  Consulta sem organization no contexto **falha** (fail-closed).
- **`organization_uuid` vem do contexto** — com UMA exceção controlada
  (UX4, gestão cross-tenant da plataforma): quem possui `*:*` por
  PERTENCIMENTO EXATO (super_admin) pode apontar `organization_uuid`
  explícito em `POST /workspaces` (criar o primeiro workspace de uma
  organization nova) e filtrar `GET /workspaces?organization_uuid=` para
  listar os workspaces de qualquer organization. A autorização mora no
  service (`ehPlataforma`); a criação confere existência/vitalidade do pai
  pelo contrato `ResolvedorEstadoOrganization` (ligado no bootstrap — filho
  nunca fica mais vivo que o pai), reescopo o ctx na ALVO e audita com
  `cross_tenant=true`. Não-plataforma apontando alheia = `fora_do_escopo`
  (404, sem vazar existência); apontando a própria é aceito (equivalente ao
  escopo do ctx). Sem o contrato ligado, a criação cross-tenant falha
  FECHADA. Sem filtro/pedido, o comportamento é exatamente o de antes.
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
- **Cache de slug entra por interface** declarada aqui — desde a evolução
  Redis (#8) o bootstrap liga a implementação `workspace:slug:{slug}` com TTL
  curto (`cache.ttl_resolucao_seg`) e **invalidação ativa** em inativação/
  reativação/remoção e cascata da organization (`InvalidarOrganization`
  seletivo); ausência de cache (Redis degradado) segue sendo operação normal,
  só mais cara. A invariante "filho nunca mais vivo que o pai" tem teste
  ponta a ponta no bootstrap.

## Permissões (`permissions.go` + `Catalogo()`)

`identidade:workspace:{criar, ler, editar, remover}` — rota a rota.

## Definição de pronto

- Checklist do `agents/05`; testes de unicidade global do slug (regex +
  reservados, nos dois sentidos) e do isolamento por organization.
