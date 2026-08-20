# AGENTS.md — `internal/pkg/config`

Configuração do processo, carregada de `configs.json` no boot.

## Regras

- **`config.Init(path)` no boot** (primeira coisa, antes de qualquer infra);
  leitura depois disso é **`config.Use()`** (erro se não inicializado) ou
  **`config.MustUse()`** (pânico — restrito ao bootstrap).
- `configs_example.json` é o modelo **versionado** no git; `configs.json` é
  local e está no `.gitignore`. Toda chave nova entra primeiro no exemplo.
- As structs espelham o modelo definido em `agents/02` (servidor, Postgres,
  pool, JWT, migrations, CORS/`base_domain`...). Campo novo sem uso real não
  entra.
- Segredo (senha do banco, chave do JWT) **nunca** é logado nem devolvido em
  erro. Mensagem de erro de config diz o campo e o motivo, em PT-BR, sem
  despejar valores.
- Config inválida falha no `Init` com erro descritivo — não existe "valor
  padrão silencioso" para chave de segurança.

## Definição de pronto

- `Init` com arquivo ausente/malformado/incompleto falha com mensagem clara.
- `Use()` antes do `Init` devolve erro; teste cobre os dois estados.
