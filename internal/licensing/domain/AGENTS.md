# AGENTS.md — `internal/licensing/domain`

Subdomínios do domínio **licensing**: `modulo`, `licenca`, `ativacao`.

## Regras

As mesmas do `internal/identidade/domain` (anatomia de 8 arquivos +
`permissions.go`, modelo em `internal/licensing/model/{subdominio}`, auth
rota a rota, auditoria em toda escrita com `events.go`, catálogo de erros,
errobserve, singleton `New/Use/MustUse`).

Especificidades do domínio:

- **Contratos entre irmãos** em cada `contratos.go`; adaptadores no
  `cmd/bootstrap/licensing.go` resolvem NA CHAMADA. Sentinela nova de contrato
  (ex.: `ErrModuloNaoEncontrado`) pertence ao CONSUMIDOR — o adaptador traduz.
- **Peça de contrato nil = fail-closed**: ativação sem tripé completo recusa;
  remoção de módulo sem verificador recusa. Nunca "seguir sem a peça" em
  caminho que CONCEDE acesso ou REMOVE recurso referenciado.
- Consultas cruas via `.Table()` BYPASSAM o soft-delete automático do gorm —
  condição `deleted_at IS NULL` explícita em todo JOIN/WHERE (ver os
  repositórios da licenca e da ativação).

## Definição de pronto

Checklist de subdomínio do `agents/05` completo, build/vet/test verdes.
