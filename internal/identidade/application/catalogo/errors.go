package catalogo

import modeluser "workspace-api/internal/identidade/model/user"

// Dominio e Subdominio identificam esta aplicação nos catálogos e logs —
// rotas em /api/application/identidade/catalogo/... e /api/system/errors.
const (
	Dominio    = modeluser.Dominio // "identidade"
	Subdominio = "catalogo"
)

// Esta aplicação NÃO registra catálogo no rest_err: não introduz sentinelas
// próprias — leitura pura do ctx (permissões) e do registro global (erros).
// Suas únicas falhas possíveis são de sistema (500 por rest_err.Interno) ou
// de autorização (401/403 da cadeia de middleware, codes de sistema).
// Registrar um catálogo vazio emitiria um grupo vazio em GET /api/system/errors.
