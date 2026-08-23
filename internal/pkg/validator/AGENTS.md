# AGENTS.md — `internal/pkg/validator`

Tags de validação customizadas do `binding` do gin, registradas **no boot**
(logo após o `config.Init`, antes de qualquer engine aceitar requisição).

## Regras

- Toda tag nova (ex.: documento, slug, domínio) é registrada aqui uma única
  vez no boot — registrar por request ou por controller duplicaria registro
  e divergiria entre rotas.
- Tag customizada tem nome estável, erro de binding traduzido para PT-BR na
  resposta (`rest_err` 400) e teste dos dois lados: o que aceita e o que
  recusa.
- Validação de **formato** vive aqui; validação de **regra de negócio**
  (slug reservado, unicidade) vive no service do subdomínio. A fronteira é:
  o validator não consulta banco nem config de negócio.
- Nada de regex "temporária": a expressão fica nomeada e testada. Desde a
  R7 a regex do slug (`slugdns`) tem fonte ÚNICA **aqui** — pkg é folha, o
  único lugar alcançável tanto pelo binding quanto pelo VO do subdomínio
  (model/workspace delega em `SlugValido`); nunca recriar o literal em
  outro pacote.

## Definição de pronto

- Boot sem o registro faz o binding falhar de forma explícita, não
  silenciosa.
