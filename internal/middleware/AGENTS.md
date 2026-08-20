# AGENTS.md — `internal/middleware`

Cadeia de autenticação, resolução de workspace e autorização da plataforma
(especificação em `agents/03`). É o único pacote fora de
`domain`/`application` que decide acesso.

## A cadeia obrigatória

| Middleware | Pergunta | Falha |
|---|---|---|
| `SetContextAuthorization` | quem está falando? (JWT Bearer ou `X-Api-Key`) | 401 |
| `ResolveWorkspace` | qual workspace? (Host; `X-Workspace-Id` fora de subdomínio) | 400 / 404 / 403 |
| `RequirePermission` | pode fazer ISTO? (permissão granular) | 403 |

A ordem é sempre essa: `ResolveWorkspace` precisa da identidade que o
`SetContextAuthorization` injetou, e a exigência de permissão precisa das
duas. Declarada **rota a rota** pelos controllers, nunca no grupo.

## Regra que define o desenho: NÃO importa `internal/domain`

Os controllers de `domain/...` importam **este** pacote para montar rotas —
um import de volta fecharia ciclo. Então:

- toda dependência entra por **interface declarada em `contratos.go`**;
- quem implementa é `cmd/bootstrap/middleware.go`, resolvendo os singletons
  **no momento da chamada** e traduzindo o erro do vizinho;
- os tipos deste pacote são o mínimo da autorização — sem hash, sem senha.

## Decisões que não devem ser "simplificadas"

- **Cadeia não inicializada = rota FECHADA.** Sem `middleware.New` no boot,
  toda rota protegida responde **403** — nunca aberta. Peça faltando é erro
  de boot, não 501 em runtime.
- **`RequirePermission` rota a rota** com a string exata
  `dominio:subdominio:acao` (ex.: `identidade:workspace:editar`). Exigência
  vazia **nega** — exigência que não sabe o que exigir nunca vira liberação.
- **`ResolveWorkspace` pelo Host**: extrai o slug do subdomínio e o
  domínio-base, aceitando `{slug}.{base_domain}` da plataforma **E**
  `{slug}.{dominio-custom}` da organization (white-label) — e nesse caso
  **valida que o workspace pertence à organization dona do domínio**.
  Workspace alheio no domínio do parceiro = **404**: confirmar que ele
  existe em outra organization entregaria a carteira de clientes.
- **`X-Workspace-Id` só vale fora de subdomínio**; quando diverge do
  subdomínio, **o subdomínio vence** e o header é ignorado com log — é o que
  impede mover uma sessão entre workspaces por header forjado.
- **Acesso de suporte é a exceção auditada ao vínculo**:
  `admin_organization` entra em qualquer workspace da PRÓPRIA organization,
  `super_admin` em qualquer um — sempre com concessão registrada em
  auditoria (quem, onde, com que papel). A consulta roda só nos caminhos de
  falha do vínculo normal, nunca no caminho quente.
- **Falha de infraestrutura não vira negativa**: erro de banco ao resolver
  acesso sobe intacto e vira 500. Um 403 mandaria o usuário procurar o
  administrador para resolver uma indisponibilidade.

## Definição de pronto

- Testes com dublês que reproduzem o CONTRATO de cada interface; um
  subdomínio exercita a cadeia real ponta a ponta, incluindo o estado
  "cadeia não inicializada → 403 em toda rota".
