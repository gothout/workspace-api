# AGENTS.md — `internal/identidade/application`

Orquestrações do domínio **identidade** que atravessam **2+ subdomínios**
(ou agregam registros de todos eles). Hoje: **`catalogo`** (agrega o
`Catalogo()` de permissões, os eventos de auditoria e o mapa de erros para
o front-end), **`auth`** (login/refresh/logout — orquestra `user` + resolução
da organization pelo Host), **`logs`** (leitura das trilhas de log do
ClickHouse com recorte plataforma/organization/workspace) e
**`provisionamento`** (UX5: admin inicial + workspace inicial de uma
organization pela plataforma — orquestra organization → workspace → user).

## Regras

- **SEM `model.go` nem `repository.go`** — aplicação **não persiste nada
  próprio**. Se aparecer uma tabela "da aplicação", ou ela é de um
  subdomínio novo ou o desenho está errado.
- Dependências dos subdomínios de `internal/identidade/domain/...` entram
  por **`contratos.go`**: interfaces estreitas declaradas **aqui** (o
  consumidor dita o contrato), ligadas no `cmd/bootstrap` por **adaptadores
  que resolvem o singleton NA CHAMADA** — mesmo desenho do
  `internal/middleware`.
- **Sentinelas de irmão NUNCA são recatalogadas** (code duplicado = pânico no
  boot do `rest_err`) e nunca importadas: o adaptador do bootstrap TRADUZ a
  recusa do subdomínio para o vocabulário da própria aplicação (ex.: a
  duplicata de atribuição vira `ErrAtribuicaoExistente` interno de
  idempotência no provisionamento); as sentinelas de INVARIANTE dos VOs já
  têm catálogo no subdomínio dono — o `traduzir()` as resolve pelo registro
  global via `errors.Is`.
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
