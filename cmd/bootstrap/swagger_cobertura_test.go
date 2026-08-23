package bootstrap

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"workspace-api/cmd/server/routes"
	aplicacaoauth "workspace-api/internal/identidade/application/auth"
	aplicacaocatalogo "workspace-api/internal/identidade/application/catalogo"
	aplicacaologs "workspace-api/internal/identidade/application/logs"
	dominioOrganizacao "workspace-api/internal/identidade/domain/organization"
	dominioUsuario "workspace-api/internal/identidade/domain/user"
	dominioWorkspace "workspace-api/internal/identidade/domain/workspace"
	"workspace-api/internal/infra/clickhouse"
	"workspace-api/internal/pkg/config"
	"workspace-api/internal/pkg/errobserve"
	"workspace-api/internal/pkg/log/access_log"
	"workspace-api/internal/pkg/log/audit_log"
)

// --- Cobertura Swagger (fase F6) --------------------------------------------
//
// Contrato executável entre as rotas REGISTRADAS e o docs/swagger.json
// VERSIONADO, nos DOIS sentidos:
//
//  1. toda rota de negócio montada no engine existe no swagger (mesmo método,
//     mesmo caminho — wildcards do gin normalizados para a sintaxe OpenAPI);
//  2. todo path+método do swagger existe no engine (annotation sem rota é
//     contrato mentiroso para o front).
//
// É o ÚNICO teste deste pacote que boota os SINGLETONS dos subdomínios
// (sync.Once é por processo): o engine só registra as rotas de negócio via
// Use() deles. Sem docker, o teste pula como os demais de integração.

// caminhoOpenAPI converte o wildcard do gin (`:uuid`) para a sintaxe OpenAPI
// (`{uuid}`) — mesma rota, dialetos diferentes.
func caminhoOpenAPI(caminhoGin string) string {
	segmentos := strings.Split(caminhoGin, "/")
	for i, seg := range segmentos {
		if strings.HasPrefix(seg, ":") {
			segmentos[i] = "{" + seg[1:] + "}"
		}
	}
	return strings.Join(segmentos, "/")
}

type swaggerDocumento struct {
	Paths map[string]map[string]json.RawMessage `json:"paths"`
}

var metodosHTTP = map[string]bool{
	"get": true, "post": true, "put": true, "patch": true, "delete": true,
}

func TestSwaggerCobreExatamenteAsRotasRegistradas(t *testing.T) {
	if !dockerDisponivel(t) {
		t.Skip("docker indisponível — teste de integração pulado")
	}
	amb := subirAmbiente(t)

	// Boot na MESMA ordem e com os MESMOS adaptadores do Serve(): o engine
	// abaixo registra exatamente as rotas do processo real.
	_, err := dominioOrganizacao.New(amb.db, suspendedorWorkspaces{}, encerradorSessoesUsuario{})
	require.NoError(t, err)
	_, err = dominioWorkspace.New(amb.db, nil)
	require.NoError(t, err)
	_, err = dominioUsuario.New(amb.db, validadorSemprePertence{})
	require.NoError(t, err)
	_, err = aplicacaoauth.New(aplicacaoauth.Dependencias{
		Usuarios:     usuariosAuth{},
		Emissor:      emissorToken{},
		Organizacoes: resolvedorOrganizacao{},
		Vitalidade:   vitalidadeOrganizacao{},
	})
	require.NoError(t, err)
	_, err = aplicacaocatalogo.New(aplicacaocatalogo.Dependencias{
		Permissoes: novoAgregadorPermissoes(),
		Eventos:    novoAgregadorEventos(),
	})
	require.NoError(t, err)
	// Aplicação logs (E5): rotas só precisam do Use() não-nil — o consultor
	// nunca é tocado pelo registro; dublê local basta.
	_, err = aplicacaologs.New(aplicacaologs.Dependencias{Trilhas: consultorSwaggerFake{}})
	require.NoError(t, err)

	engine, err := routes.Montar(routes.Opcoes{
		App: config.AppConfig{Name: "cobertura-swagger", Env: "teste", Version: "0.0.0-teste", BaseDomain: "localhost"},
	})
	require.NoError(t, err)

	// Rotas montadas pelo próprio routes FORA das famílias documentadas:
	// sonda de sistema e UI do Swagger (exceção de prefixo decidida no doc 01).
	foraDoContrato := map[string]bool{
		metodoECaminho("GET", routes.RotaStatus):           true,
		metodoECaminho("GET", routes.RotaDoc):              true,
		metodoECaminho("GET", routes.RotaDoc+"/*qualquer"): true,
	}

	registradas := map[string]bool{}
	for _, r := range engine.Routes() {
		if foraDoContrato[r.Method+" "+r.Path] {
			continue
		}
		registradas[metodoECaminho(r.Method, caminhoOpenAPI(r.Path))] = true
	}
	require.NotEmpty(t, registradas, "engine sem rotas de negócio — boot dos subdomínios falhou")

	bruto, err := os.ReadFile("../../docs/swagger.json")
	require.NoError(t, err, "docs/swagger.json versionado deve existir junto do código")
	var doc swaggerDocumento
	require.NoError(t, json.Unmarshal(bruto, &doc))
	require.NotEmpty(t, doc.Paths, "swagger sem paths — regenere com swag init")

	documentadas := map[string]bool{}
	for caminho, metodos := range doc.Paths {
		for metodo := range metodos {
			if !metodosHTTP[metodo] {
				continue // chaves de metadados da spec, não operações
			}
			documentadas[metodoECaminho(strings.ToUpper(metodo), caminho)] = true
		}
	}

	// Sentido 1: rota registrada SEM annotation no swagger = contrato ausente
	// para o front que consome o /doc.
	var faltandoNoSwagger []string
	for rota := range registradas {
		if !documentadas[rota] {
			faltandoNoSwagger = append(faltandoNoSwagger, rota)
		}
	}
	assert.Empty(t, faltandoNoSwagger,
		"rotas registradas ausentes no docs/swagger.json (regenere com swag init): %v", faltandoNoSwagger)

	// Sentido 2: annotation SEM rota registrada = documento mentiroso (a rota
	// foi renomeada/removida e o swagger ficou para trás — ou nunca existiu).
	var sobrandoNoSwagger []string
	for rota := range documentadas {
		if !registradas[rota] {
			sobrandoNoSwagger = append(sobrandoNoSwagger, rota)
		}
	}
	assert.Empty(t, sobrandoNoSwagger,
		"paths do docs/swagger.json sem rota correspondente no engine: %v", sobrandoNoSwagger)
}

func metodoECaminho(metodo, caminho string) string {
	return metodo + " " + caminho
}

// consultorSwaggerFake satisfaz o contrato da aplicação logs sem tocar as
// trilhas — o teste de cobertura registra rotas, não consulta logs.
type consultorSwaggerFake struct{}

func (consultorSwaggerFake) Auditoria(ctx context.Context, filtro clickhouse.FiltroTrilha) ([]audit_log.Evento, int64, error) {
	return nil, 0, nil
}

func (consultorSwaggerFake) Acesso(ctx context.Context, filtro clickhouse.FiltroTrilha) ([]access_log.Evento, int64, error) {
	return nil, 0, nil
}

func (consultorSwaggerFake) Erros(ctx context.Context, filtro clickhouse.FiltroTrilha) ([]errobserve.Evento, int64, error) {
	return nil, 0, nil
}
