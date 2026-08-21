# AGENTS.md — `internal`

Todo o código da aplicação. Mapa das camadas e as regras de dependência que
o arch-go (`arch-go.yml`, coverage 100) torna executáveis — especificação
completa em `agents/01`.

## Camadas

| Camada | Papel |
|---|---|
| `pkg` | utilidades transversais, **folha** do grafo |
| `infra` | adaptadores técnicos (Postgres, JWT, migrations) |
| `middleware` | cadeia de autenticação/autorização/resolução |
| `identidade/` | **domínio** do negócio (bounded context) — pasta direta de `internal/` |
| `identidade/domain/` | subdomínios do domínio (`organization`, `workspace`, `user`) |
| `identidade/application/` | orquestrações entre os subdomínios do domínio (`catalogo`) |

Fluxo permitido: `pkg ← infra ← {dominio}/domain ← {dominio}/application ← cmd`.

## As 8 regras de dependência (mesma numeração de `agents/01` — fonte única)

1. `pkg` **não importa** nada de `internal/` fora de `pkg` — é folha.
2. `infra` importa só `pkg` + libs externas — **nunca outro `infra`**, nem
   em testes.
3. `internal/{dominio}/domain` importa `pkg`, `infra` e `middleware` —
   **nunca** `application` nem `cmd`.
4. Um subdomínio **não importa irmão**: dependência entra por interface
   declarada no consumidor e ligada no bootstrap.
5. `application` importa `pkg`, `infra` e `middleware` — **nunca os pacotes
   de `domain/` do próprio domínio**: orquestra os subdomínios por
   interfaces estreitas em `contratos.go`, não persiste nada próprio.
6. Dentro do subdomínio: `controller → service → repository`, nunca o
   contrário.
7. Controller **nunca toca `*gorm.DB`** — recebe o `Service` pela interface.
8. Todo método de repository recebe `ctx` e aplica `orgctx.Scope` ou
   `ScopeOrganization` (fail-closed).

Fora da numeração, duas notas estruturais: `middleware` **não importa
nenhum `internal/{dominio}/domain` nem `application`** (seria ciclo: os
controllers importam ele — dependências por `contratos.go`); e só `cmd`
importa todas as camadas — toda ligação concreta acontece lá.

## Conferência

Pacote novo sem regra no `arch-go.yml` reprova o `go test ./...`. Ao fechar
cada fase, conferir o grafo no Dependency Graph do Go Architect (ver
`agents/01`).
