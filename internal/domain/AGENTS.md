# AGENTS.md — `internal/domain`

Subdomínios de negócio. Hoje: `identidade` (organization, workspace, user).

## Regras

- Importa `pkg`, `infra` e `middleware` — **NUNCA** `application`, `cmd` nem
  **subdomínio irmão** (regras 4 e 5 de `internal/AGENTS.md`). Dependência
  entre subdomínios entra por interface declarada no consumidor, ligada no
  `cmd/bootstrap`.
- Todo subdomínio segue a anatomia de **8 arquivos + `permissions.go`**
  (templates em `agents/05`): `model.go`, `dto.go`, `repository.go`,
  `service.go`, `controller.go`, `routes.go`, `errors.go`, `singleton.go`,
  `permissions.go`.
- **Auth declarada rota a rota** no `Routes()` do controller
  (`RequirePermission("dominio:subdominio:acao")`), nunca no grupo.
- **Auditoria em toda escrita**: quem, quando, o quê, de onde. Leitura não
  audita; escrita sem auditoria não fecha checklist.
- **Catálogo de erros**: toda sentinela do `errors.go` tem entrada no
  catálogo do subdomínio (code estável + mensagem + status), registrada no
  `rest_err` — é o que alimenta `GET /api/system/errors`.
- **`Catalogo()` de permissões**: `permissions.go` declara as constantes e o
  catálogo com metadados (descrição PT-BR, rota, método, grupo de menu) —
  sem ele o subdomínio não aparece no endpoint de permissões e o checklist
  não fecha.
- Padrão singleton `New/Use/MustUse` conforme `agents/05`.

## Definição de pronto

Checklist de subdomínio do `agents/05` completo, build/vet/test verdes.
