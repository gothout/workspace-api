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
| `identidade/`, `licensing/` | **domínios** do negócio (bounded contexts) — pastas diretas de `internal/` |
| `{dominio}/model/` | **modelos expostos** do domínio (entidades, VOs, invariantes) — **folha**, importável por todas as camadas |
| `{dominio}/domain/` | subdomínios de cada domínio |
| `{dominio}/application/` | orquestrações entre subdomínios do domínio |

Fluxo permitido: `pkg ← infra ← {dominio}/model ← {dominio}/domain ← {dominio}/application ← cmd`.

## As 8 regras de dependência (mesma numeração de `agents/01` — fonte única)

1. `pkg` **não importa** nada de `internal/` fora de `pkg` — é folha.
2. `infra` importa só `pkg` + libs externas — **nunca outro `infra`**, nem
   em testes.
3. `internal/{dominio}/domain` importa `pkg`, `infra` e `middleware` —
   **nunca** `application` nem `cmd`.
4. Um subdomínio **não importa o `domain/` de irmão**: dependência entra por
   interface declarada no consumidor e ligada no bootstrap. Importar o
   **`model/` de irmão é permitido e incentivado para leituras** — ele é folha
   (regra 9); escrita em agregado alheio continua só por interface/application.
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
