# AGENTS.md — `internal/application/identidade`

Orquestrações do domínio identidade. Hoje: **`catalogo`** (agrega permissões
e o mapa de erros para o front-end).

## Regras

- Uma aplicação por pasta, com `contratos.go` declarando o que ela precisa
  dos subdomínios de `domain/identidade/...` — interfaces estreitas, ligadas
  no `cmd/bootstrap`.
- Vale tudo de `internal/application/AGENTS.md`: sem persistência própria,
  sem import de subdomínio, rotas em `/api/application/identidade/...`.
- Aplicação nova aqui precisa atravessar de fato 2+ subdomínios (ou agregar
  todos); caso contrário a lógica pertence a um subdomínio.

## Definição de pronto

- Cada aplicação tem seus contratos cobertos por teste com dublês e as rotas
  registradas via `Use()` (ausência de boot = rota fora do ar com log,
  nunca pânico).
