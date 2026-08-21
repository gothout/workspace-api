# AGENTS.md — `internal/identidade`

Domínio de identidade: a hierarquia **organization → workspace → user**
(especificação completa em `agents/03`). Domínio é **pasta direta de
`internal/`**: os modelos expostos (entidades, VOs, invariantes) moram em
`model/`, os subdomínios em `domain/` e as orquestrações que os atravessam em
`application/` (hoje: `application/catalogo` e `application/auth`).

## Subdomínios

- `organization` — raiz da hierarquia, dona do contrato e do domínio custom.
- `workspace` — filho de organization, dono do slug `{slug}.{base_domain}`.
- `user` — vive no workspace, com papéis atribuídos **por workspace**.

## Invariantes da hierarquia

- Todo workspace **pertence a uma organization** — não existe workspace
  órfão, e mover workspace entre organizations é operação nova, não UPDATE.
- Papéis de user são **por workspace** (tabela de atribuição): o mesmo user
  é admin num workspace e leitor em outro.
- **Inativar a organization suspende os workspaces** dela e **desativa a
  resolução do domínio custom** — filho nunca fica mais vivo que o pai.

## Exceções de escopo (documentadas com motivo)

- **organization**: tabela raiz, SEM escopo acima — é ela quem define o
  escopo. Acesso restrito a `super_admin`/suporte auditado.
- **workspace**: escopo por organization (`orgctx.ScopeOrganization`);
  `FindBySlug`/verificação de slug são globais porque a resolução parte do
  Host, antes de existir escopo — resultado nunca exposto em rota de
  administração.
- **user**: administração escopa por organization (`ScopeOrganization`) —
  `identidade_user_user` não tem `workspace_uuid`; listagem por workspace é
  via `atribuicao`. As tabelas `papel`/`papel_permissao` são **globais da
  plataforma** (sem coluna de escopo — exceção documentada: papéis seed
  globais, criados na F1).
- Detalhes e demais exceções no `AGENTS.md` de cada subdomínio — exceção
  nova sem motivo escrito aqui e lá é reprovada.

## Definição de pronto

- Cada subdomínio fecha o checklist do `agents/05`; as invariantes acima têm
  teste, inclusive a cascata de inativação.
