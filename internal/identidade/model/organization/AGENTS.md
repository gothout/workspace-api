# AGENTS.md — `internal/identidade/model/organization`

Modelo do subdomínio `organization` (regras do nível em
`internal/identidade/model/AGENTS.md`).

## Conteúdo

- Entidade `Organization` — raiz do agregado, tabela
  `identidade_organization_organization` — e `ApiKey`
  (`identidade_organization_apikey`, `key_hash` = SHA-256).
- VO `Dominio` com `ParseDominio`: rejeita igual ao `base_domain`,
  ascendente/descendente dele e public suffix (eTLD+1 no mínimo).
- Construtor `NewOrganization` validando invariantes; métodos de
  comportamento (`Inativar` — a cascata sobre workspaces é orquestrada pelo
  service do subdomínio via `contratos.go`, não aqui).
- Sentinelas de invariante do modelo (ex.: `ErrDominioInvalido`,
  `ErrJaInativo`).

## O que não entra

DTOs, repository, service, catálogo de erros, permissões — tudo isso mora em
`internal/identidade/domain/organization`.
