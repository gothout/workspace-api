# agents/ — Especificação do `workspace-api`

Esta pasta contém **toda a especificação** necessária para um agente de IA construir, em loop, o **workspace-api**: template de API gerenciadora em Go com hierarquia **organization → workspace → user**, arquitetura por domínio (DDD) e padrão singleton.

## Projeto

- **Nome:** workspace-api — template de API gerenciadora (base de identidade, tenancy e autorização para produtos SaaS B2B white-label).
- **Linguagem:** Go **1.25** (ver `go.mod`; não subir o `go` directive sem necessidade real).
- **Módulo Go:** `workspace-api`.
- **Backlog vivo:** issues do GitHub (`gh issue list`), uma por fase, na ordem F0 → F6. O doc `06` espelha as fases e guarda o log de iterações.

## Ordem de leitura obrigatória

| # | Documento | Conteúdo |
|---|-----------|----------|
| 00 | [00-visao-produto.md](./00-visao-produto.md) | O que é o template, a hierarquia, decisões já tomadas |
| 01 | [01-arquitetura-ddd.md](./01-arquitetura-ddd.md) | Camadas, estrutura de pastas, regras de dependência invioláveis, bootstrap DI, validação da arquitetura |
| 02 | [02-stack-e-infra.md](./02-stack-e-infra.md) | Stack Go, modelo do `configs.json`, Postgres, convenção de tabelas, **migrations (especificação completa)**, ferramentas de mapeamento, evoluções futuras |
| 03 | [03-identidade-tenancy.md](./03-identidade-tenancy.md) | organization → workspace → user, JWT + `X-Api-Key`, cadeia de middlewares, resolução por Host com white-label, RBAC granular, catálogo consultável, acesso de suporte |
| 04 | [04-convencoes-api.md](./04-convencoes-api.md) | Rotas `/api/domain` e `/api/application`, verbos REST, Swagger, `rest_err`, paginação, contratos do endpoint de permissões e do mapa de erros |
| 05 | [05-padroes-codigo.md](./05-padroes-codigo.md) | Templates canônicos dos arquivos do subdomínio, padrão singleton, convenções Go, checklist de pronto |
| 06 | [06-plano-execucao.md](./06-plano-execucao.md) | Fases F0–F6 espelhando as issues, definição de pronto por fase, log de iterações |

Além destes, **todo diretório do projeto tem seu próprio `AGENTS.md`** com as regras locais — ler o de cada pasta que a issue toca antes de codar.

## Protocolo do loop de execução

O trabalho é dirigido por **issues do GitHub**. Cada iteração segue exatamente este protocolo:

1. **Abrir a issue aberta mais antiga** (`gh issue list --state open --sort created`, ignorando as de label `evolucao`): as fases têm ordem e dependência — a mais antiga aberta é sempre a próxima.
2. **Reler antes de codar:** `01-arquitetura-ddd.md`, `04-convencoes-api.md`, `05-padroes-codigo.md` e o `AGENTS.md` de cada pasta tocada. Se a issue cria tabelas, reler também a seção de migrations do `02`.
3. **Implementar** seguindo os templates do `05` e o checklist da issue (que espelha o checklist de pronto do `05`).
4. **Validar:** `go build ./... && go vet ./... && go test ./...` — tudo verde. Mexeu em invariante disputada (slug, documento, estado, unicidade)? Rodar também `go test -race ./...`.
5. **Fechar a issue** (`gh issue close`) e registrar a iteração no **log do `06`** (data, issue, arquivos criados/alterados, resultado, observações).

## Regras invioláveis do loop

- **Uma issue por vez.** Nunca abrir duas frentes na mesma iteração.
- **Nunca pular fase.** F3 não começa com F2 aberta, mesmo que pareça independente.
- **Nunca deixar o build quebrado** ao fim da iteração — build vermelho é prioridade absoluta sobre qualquer item novo.
- Issue fechada sem checks verdes ou sem o checklist cumprido **não conta**: reabrir e completar.
- As issues de label `evolucao` (Redis, ClickHouse, errobserve) **não entram no loop** até o F6 fechar — ver "Evoluções futuras" no `02`.
