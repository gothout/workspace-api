# AGENTS.md — `internal/identidade/application`

Orquestrações do domínio **identidade** que atravessam **2+ subdomínios**
(ou agregam registros de todos eles). Hoje: **`catalogo`** (agrega o
`Catalogo()` de permissões e o mapa de erros para o front-end) e **`auth`**
(login/refresh/logout — orquestra `user` + resolução da organization pelo
Host).

## Regras

- **SEM `model.go` nem `repository.go`** — aplicação **não persiste nada
  próprio**. Se aparecer uma tabela "da aplicação", ou ela é de um
  subdomínio novo ou o desenho está errado.
- As dependências dos subdomínios de `internal/identidade/domain/...` entram
  por **`contratos.go`**: interfaces estreitas declaradas **aqui** (o
  consumidor dita o contrato), ligadas no `cmd/bootstrap` por **adaptadores
  que resolvem o singleton NA CHAMADA** — mesmo desenho do
  `internal/middleware`.
- Importa `pkg`, `infra`, `middleware` e os pacotes **`model/`** do domínio
  (para leituras e projeções complexas sobre os agregados) — **nunca importa
  subdomínio de `domain/`** e nunca é importada por ele (regras 3 e 5 de
  `agents/01`).
- Uma aplicação por pasta. Rotas em `/api/application/identidade/...`, com a
  mesma regra de auth rota a rota dos subdomínios de `domain/`.
- Aplicação nova aqui precisa atravessar de fato 2+ subdomínios (ou agregar
  todos): orquestração de UM subdomínio só não é aplicação — é sinal de que
  a lógica está na camada errada.

## Definição de pronto

- Nenhuma tabela, nenhum import de `domain/`; os contratos são cobertos por
  teste com dublês que reproduzem o comportamento dos adaptadores reais, e
  as rotas são registradas via `Use()` (ausência de boot = rota fora do ar
  com log, nunca pânico).
