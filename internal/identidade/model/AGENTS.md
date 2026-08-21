# AGENTS.md — `internal/identidade/model`

Modelos **expostos** do domínio identidade: entidades, VOs e invariantes dos
subdomínios — um pacote por subdomínio (`organization`, `workspace`, `user`).
É o nível que viabiliza **buscas complexas sobre os modelos sem o emaranhado
dos subdomínios**: qualquer camada importa daqui, só por pacotes.

## Regras

- **Folha absoluta**: importa só stdlib + libs externas (ex.: `gorm.io/gorm`
  para `DeletedAt`) + `internal/pkg` — **NUNCA** `domain`, `application`,
  `infra` ou `middleware`. Sem essa folha, o objetivo (importar modelo de
  qualquer lugar sem ciclo) não existe.
- Cada pacote se chama como o subdomínio (`package workspace`); quem importa
  junto com o subdomínio de mesmo nome usa **alias** —
  `workspacemodel "workspace-api/internal/identidade/model/workspace"`.
- **Contém**: entidade com `TableName()`, VOs com `ParseX`, construtor
  `NewX` validando invariantes, métodos de comportamento,
  `CreateInput`/`UpdateInput`, `ListFilter`, tipos nomeados de status e as
  **sentinelas de invariante do modelo** (ex.: `ErrSlugInvalido`) — o
  catálogo delas (code estável + mensagem + status) é registrado no
  `errors.go` do subdomínio, que importa este pacote.
- **O que NÃO entra aqui**: DTO de request/response, `repository.go`,
  `service.go`, regra de negócio de serviço, auditoria, permissão. Modelo é
  dado + invariante, nada mais.
- **Quem importa**: `domain` (o próprio modelo e, **em leitura**, o de
  irmãos quando fizer sentido via interface), `application` (livremente),
  `middleware`, `cmd`. Escrita em agregado alheio continua proibida — só por
  interface/aplicação.

## Definição de pronto

- `go list -deps ./internal/identidade/model/...` não contém `domain`,
  `application`, `infra` nem `middleware`.
