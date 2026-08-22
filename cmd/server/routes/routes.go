// Package routes monta o gin.Engine: middlewares globais (access log →
// recovery → CORS), NoRoute padronizado, rotas de sistema (/api/status,
// Swagger em /doc) e os grupos /api/domain e /api/application onde cada
// controller pendura suas rotas via Use() do subdomínio.
package routes

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	organizacao "workspace-api/internal/identidade/domain/organization"
	"workspace-api/internal/pkg/config"
	"workspace-api/internal/pkg/rest_err"
)

// Prefixos das famílias de rota e rotas de sistema (exceção de prefixo
// decidida no doc 01/04).
const (
	PrefixoDominio   = "/api/domain"
	PrefixoAplicacao = "/api/application"

	RotaStatus = "/api/status"
	RotaDoc    = "/doc"
)

// Opcoes carregam o que o engine precisa do boot — sondas e domínios custom
// injetados pelo bootstrap, nunca importados daqui.
type Opcoes struct {
	App        config.AppConfig
	Cors       config.CorsConfig
	SondaBanco func(context.Context) error
	// DominiosCustom devolve os domínios white-label registrados pelas
	// organizations (lowercase, sem porta); nil = só o domínio-base. Erro na
	// consulta recusa a origem (fail-closed), nunca abre.
	DominiosCustom func(context.Context) ([]string, error)
}

// Controlador é o que todo subdomínio expõe para pendurar rotas.
type Controlador interface {
	Routes(gin.IRouter)
}

// registrarRotas usa o helper com Use(): subdomínio fora do boot = rota não
// sobe, motivo no log — nunca pânico, nunca rota aberta.
func registrarRotas(nome string, grupo *gin.RouterGroup, usar func() (Controlador, error)) {
	ctrl, err := usar()
	if err != nil {
		slog.Warn("rotas não registradas: subdomínio não inicializado",
			"subdominio", nome, "motivo", err.Error())
		return
	}
	ctrl.Routes(grupo)
}

// Montar devolve o engine completo, pronto para o cmd/server servir.
func Montar(opcoes Opcoes) *gin.Engine {
	if opcoes.App.Env == "producao" {
		gin.SetMode(gin.ReleaseMode)
	} else {
		gin.SetMode(gin.DebugMode)
	}
	engine := gin.New()

	// Middlewares globais, nesta ordem: access log (slot reservado — a
	// observabilidade assíncrona é evolução futura; slog básico por ora) →
	// recovery → CORS.
	engine.Use(middlewareAccessLog())
	engine.Use(gin.Recovery())
	engine.Use(politicaCors(opcoes))

	engine.NoRoute(func(c *gin.Context) {
		rest_err.WriteError(c, rest_err.NewNotFoundError("Rota não encontrada."))
	})

	registrarRotasSistema(engine, opcoes)
	registrarSwagger(engine)

	// Grupos base das duas famílias de rota; os subdomínios se registram
	// aqui nas fases seguintes (F1+), um registrarRotas por controller — a
	// auth é declarada DENTRO de Routes(), rota a rota, nunca no grupo.
	dominio := engine.Group(PrefixoDominio)
	aplicacao := engine.Group(PrefixoAplicacao)
	registrarConhecidos(dominio, aplicacao)

	return engine
}

// registrarConhecidos pendura os controllers já inicializados nos grupos.
// Engine montado por teste não passa pelo boot: subdomínio ausente = rota
// não sobe, com motivo no log (nunca pânico, nunca rota aberta).
func registrarConhecidos(dominio, aplicacao *gin.RouterGroup) {
	registrarRotas("identidade.organization", dominio, func() (Controlador, error) {
		return organizacao.Use()
	})
	// F3+: workspace/user no grupo dominio; F4+: auth; F5+: catalogo na aplicacao.
	_ = aplicacao
}

// middlewareAccessLog ocupa o slot do access log: linha estruturada por
// requisição até a evolução ClickHouse assumir o destino.
func middlewareAccessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		inicio := time.Now()
		c.Next()
		slog.InfoContext(c.Request.Context(), "requisicao",
			"metodo", c.Request.Method, "path", c.Request.URL.Path,
			"status", c.Writer.Status(), "duracao_ms", time.Since(inicio).Milliseconds(),
			"ray_trace", c.GetHeader("X-Request-Id"))
	}
}

// politicaCors aceita origens do domínio-base da plataforma (inclusive
// subdomínios *.{base_domain}), as exatas extras da config e os domínios
// custom registrados pelas organizations (white-label) — casamento por host
// PARSEADO, nunca HasSuffix em string crua (`evil-{base_domain}` não passa).
func politicaCors(opcoes Opcoes) gin.HandlerFunc {
	baseDomain := strings.ToLower(strings.TrimSpace(opcoes.App.BaseDomain))
	extras := map[string]bool{}
	for _, extra := range opcoes.Cors.AllowedOrigins {
		extras[strings.ToLower(extra)] = true
	}
	configurado := cors.Config{
		AllowOriginWithContextFunc: func(c *gin.Context, origem string) bool {
			u, err := url.Parse(strings.ToLower(origem))
			if err != nil || u.Hostname() == "" {
				return false
			}
			host := u.Hostname()
			if baseDomain != "" && (host == baseDomain || strings.HasSuffix(host, "."+baseDomain)) {
				return true
			}
			if extras[origem] || extras[host] {
				return true
			}
			return origemEmDominioCustom(opcoes, c.Request.Context(), host)
		},
		AllowMethods:     []string{http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete, http.MethodOptions},
		AllowHeaders:     []string{"Authorization", "X-Api-Key", "X-Workspace-Id", "X-Request-Id", "Content-Type"},
		ExposeHeaders:    []string{"X-Request-Id"},
		AllowCredentials: false,
		MaxAge:           12 * time.Hour,
	}
	return cors.New(configurado)
}

// origemEmDominioCustom casa o host da origem contra os domínios custom
// registrados; falha de consulta recusa (fail-closed) com log.
func origemEmDominioCustom(opcoes Opcoes, ctx context.Context, host string) bool {
	if opcoes.DominiosCustom == nil {
		return false
	}
	dominios, err := opcoes.DominiosCustom(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "cors.dominios_custom_falharam", "causa", err.Error())
		return false
	}
	for _, dominio := range dominios {
		dominio = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(dominio), "."))
		if dominio != "" && (host == dominio || strings.HasSuffix(host, "."+dominio)) {
			return true
		}
	}
	return false
}

// registrarRotasSistema: GET /api/status para sondas (montada AQUI, fora das
// famílias domain/application).
func registrarRotasSistema(engine *gin.Engine, opcoes Opcoes) {
	engine.GET(RotaStatus, func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()

		bancoOk := false
		if opcoes.SondaBanco != nil {
			bancoOk = opcoes.SondaBanco(ctx) == nil
		}
		status := http.StatusOK
		situacao := "ok"
		if !bancoOk {
			status = http.StatusServiceUnavailable
			situacao = "degradado"
		}
		c.JSON(status, gin.H{
			"status":  situacao,
			"banco":   bancoOk,
			"app":     opcoes.App.Name,
			"env":     opcoes.App.Env,
			"versao":  opcoes.App.Version,
			"horario": time.Now().UTC().Format(time.RFC3339),
		})
	})
}

// registrarSwagger publica a UI gerada em /doc/index.html (docs/ versionado).
func registrarSwagger(engine *gin.Engine) {
	engine.GET("/doc/*qualquer", ginSwagger.WrapHandler(swaggerFiles.Handler))
	engine.GET("/doc", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/doc/index.html")
	})
}
