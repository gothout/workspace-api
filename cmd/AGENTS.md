# AGENTS.md — `cmd`

Composição do processo. É o **único** lugar onde pacotes de `infra` e
subdomínios irmãos se encontram: fora daqui, nenhum pacote conhece o concreto
do outro (regras em `agents/01`).

## Regras

- `cmd/cli` define os comandos (cobra), `cmd/bootstrap` monta e liga tudo,
  `cmd/server` sobe o HTTP. O `main.go` da raiz só chama o CLI.
- Aqui se **pode** importar todas as camadas
  (`pkg ← infra ← {dominio}/domain ← {dominio}/application ← cmd`); e aqui se
  **deve** fazer toda ligação de interfaces entre pacotes — sempre por
  adaptadores, nunca ensinando um pacote a importar o outro.
- Nenhuma regra de negócio mora em `cmd`: só montagem, ordem de boot e
  encerramento. Se um `if` de negócio aparecer aqui, ele está no pacote
  errado.

## Definição de pronto

- `go run . serve --config configs.json` sobe o processo completo e drena no
  SIGTERM.
- Nenhum pacote fora de `cmd` importa outro pacote que as regras de camada
  proíbem (conferido pelo arch-go e pelo grafo do Go Architect).
