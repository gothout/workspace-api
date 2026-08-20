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
| `domain` | subdomínios de negócio (`identidade/...`) |
| `application` | orquestrações entre subdomínios (`identidade/catalogo`) |

Fluxo permitido: `pkg ← infra ← domain ← application ← cmd`.

## As 8 regras de dependência (espelhadas no arch-go.yml)

1. `pkg` **não importa** nada de `internal/` fora de `pkg` — é folha.
2. `infra` importa só `pkg` + libs externas — **nunca outro `infra`**, nem
   em testes.
3. `middleware` **não importa `domain` nem `application`** (seria ciclo: os
   controllers importam ele): dependências por interfaces em `contratos.go`,
   ligadas no `cmd/bootstrap`.
4. `domain` importa `pkg`, `infra` e `middleware` — **nunca** `application`
   nem `cmd`.
5. Um subdomínio **não importa irmão**: dependência entra por interface
   declarada no consumidor e ligada no bootstrap.
6. Dentro do subdomínio: `controller → service → repository`, nunca o
   contrário.
7. `application` orquestra subdomínios por interfaces estreitas em
   `contratos.go`; não persiste nada próprio e não é importada por `domain`.
8. Só `cmd` importa todas as camadas — toda ligação concreta acontece lá.

## Conferência

Pacote novo sem regra no `arch-go.yml` reprova o `go test ./...`. Ao fechar
cada fase, conferir o grafo no Dependency Graph do Go Architect (ver
`agents/01`).
