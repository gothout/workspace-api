# AGENTS.md — `internal/application`

Orquestrações que atravessam **2+ subdomínios** (ou agregam registros de
todos eles). Hoje: `identidade/catalogo`.

## Regras

- **SEM `model.go` nem `repository.go`** — aplicação **não persiste nada
  próprio**. Se aparecer uma tabela "da aplicação", ou ela é de um
  subdomínio novo ou o desenho está errado.
- As dependências dos subdomínios entram por **`contratos.go`**: interfaces
  estreitas declaradas **aqui** (o consumidor dita o contrato), ligadas no
  `cmd/bootstrap` por **adaptadores que resolvem o singleton NA CHAMADA** —
  mesmo desenho do `internal/middleware`.
- Importa `pkg`, `infra`, `middleware` — nunca é importada por `domain`
  (regra 7 de `internal/AGENTS.md`).
- Rotas em `/api/application/...`, com a mesma regra de auth rota a rota dos
  subdomínios de domain.
- Orquestração de UM subdomínio só não é aplicação — é sinal de que a lógica
  está na camada errada.

## Definição de pronto

- Nenhuma tabela, nenhum import de `domain`; os contratos são testáveis com
  dublês que reproduzem o comportamento dos adaptadores reais.
