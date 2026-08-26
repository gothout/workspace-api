# AGENTS.md — `internal/licensing`

Domínio de **licensing**: o gerenciamento de módulos (aplicações SaaS) do
monolito — catálogo global, licenças por organization e ativações por
workspace. Domínio é **pasta direta de `internal/`** (irmão de `identidade`):
modelos expostos em `model/`, subdomínios em `domain/`, orquestrações em
`application/`.

## Subdomínios

- `modulo` — catálogo GLOBAL das aplicações (ex.: `todolist`). O slug é o
  valor aceito no header **`Application`** e o rótulo amarrado ao código
  (`middleware.RequireAplicacao("slug")`) — unicidade TOTAL, removido não se
  libera.
- `licenca` — concessão super_admin → organization. BINÁRIA por decisão de
  produto: linha viva = licenciada; remoção lógica = revogada.
- `ativacao` — a ponte licença → uso: organization aplica módulo licenciado
  num workspace dela. É a tabela que a resolução do acesso consulta.

## Invariantes

- Ativar módulo no workspace valida O TRIPÉ: workspace vivo na organization
  (`ValidadorWorkspaces`) + módulo ATIVO no catálogo (`BuscadorModulos`) +
  licença viva na organization (`VerificadorLicencas`). Sem licença NÃO há
  ativação; revogar a licença deixa ativações órfãs INERTES (resolução nega).
- Módulo só é REMOVÍVEL sem nenhuma licença viva (`ErrModuloEmUso`); o
  caminho de retirada com histórico é Desativar (`ativo=false`), que tira o
  app da resolução na hora.
- Slug de módulo segue formato DNS (`validator.SlugValido`) e é imutável na
  prática: amarra rotas de código.

## Exceções de escopo (documentadas com motivo)

- **modulo**: tabela GLOBAL da plataforma, SEM `organization_uuid` — precede
  o tenancy (precedente `identidade_user_papel`). Fail-closed aqui é a
  permissão rota a rota: escrita exclusiva do super_admin
  (`licensing:modulo:{criar,editar,remover}`), leitura para administração
  (`licensing:modulo:ler`).
- **licenca**: escrita CRUZA organizations por natureza (super_admin atribui/
  revoga em qualquer org) — consultas de administração levam a organization
  ALVO como parâmetro explícito validado no service; leitura da própria org
  usa `orgctx.ScopeOrganization`; leitura alheia exige
  `licensing:licenca:ler_plataforma` (matcher curinga do middleware sobre as
  efetivas do ctx).
- **ativacao**: painel da organization administra QUALQUER workspace dela —
  par (organization, workspace) explícito validado pelo tripé;
  `ListarSlugsLiberados` é a consulta da resolução do acesso (F8) e nunca
  expõe nada além de slug+nome.
- Exceção nova sem motivo escrito aqui e no `AGENTS.md` do subdomínio é
  reprovada.

## Contratos entre irmãos (ligados no `cmd/bootstrap/licensing.go`)

`modulo.VerificadorLicencas` ← licenca · `licenca.BuscadorModulos` ← modulo ·
`ativacao.{BuscadorModulos, VerificadorLicencas, ValidadorWorkspaces}` ←
modulo / licenca / workspace. Adaptadores resolvem os singletons NA CHAMADA e
traduzem os `ErrNotFound` para as sentinelas DO CONTRATO — nenhum import
entre irmãos.

## Definição de pronto

Checklist do `agents/05` por subdomínio; invariantes do tripé e da guarda de
remoção com teste; build/vet/test verdes (+ `-race` nas unicidades).
