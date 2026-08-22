package middleware

import (
	"errors"
	"sync"

	"github.com/gin-gonic/gin"

	"workspace-api/internal/infra/jwt"
)

// Sentinelas internas da cadeia — nunca vazam na resposta (o cliente recebe
// o corpo padronizado do rest_err); servem aos testes e ao log.
var (
	ErrNaoInicializada = errors.New("cadeia de middleware não inicializada")
)

// Dependencias carrega o que a cadeia precisa, ligado UMA vez no boot pelo
// cmd/bootstrap. JWT é infra técnica (permitida pela regra de camada); o
// resto é contrato com o negócio — adaptadores que resolvem NA CHAMADA.
//
// Peça nil NÃO desabilita a verificação correspondente: o handler responde
// 403 fechado (peça faltando é erro de boot — fail-closed).
type Dependencias struct {
	JWT            *jwt.Manager           // validação do Bearer
	Workspaces     ResolvedorWorkspaces   // resolução por Host/Fallback
	DominiosCustom ProvedorDominiosCustom // white-label (F2 liga)
	Permissoes     ResolvedorPermissoes   // vínculo + permissões efetivas
	ApiKeys        ResolvedorApiKeys      // X-Api-Key (F2 liga; até lá 401)
}

var (
	instance *Cadeia
	once     sync.Once
	initErr  error

	fechada *Cadeia // cadeia FECHADA compartilhada quando não há New no boot
)

// Cadeia é o conjunto inicializado dos três middlewares obrigatórios.
type Cadeia struct {
	deps Dependencias
}

// New inicializa a cadeia do processo — chamada UMA vez pelo bootstrap,
// antes do registro de rotas (o Routes() dos controllers consome as funções
// de pacote). Erro aqui é fatal para o boot.
func New(deps Dependencias) error {
	once.Do(func() {
		if deps.JWT == nil {
			initErr = errors.New("middleware: jwt ausente na montagem da cadeia")
			return
		}
		if deps.Workspaces == nil {
			initErr = errors.New("middleware: resolvedor de workspaces ausente na montagem da cadeia")
			return
		}
		if deps.Permissoes == nil {
			initErr = errors.New("middleware: resolvedor de permissões ausente na montagem da cadeia")
			return
		}
		instance = &Cadeia{deps: deps}
	})
	return initErr
}

// Use devolve a cadeia do processo; sem New no boot devolve uma CADEIA
// FECHADA — nunca erro nem pânico: toda rota protegida responde 403.
func Use() *Cadeia {
	if instance != nil {
		return instance
	}
	if fechada == nil {
		fechada = &Cadeia{}
	}
	return fechada
}

// MustUse devolve a cadeia real e entra em pânico se não inicializada —
// restrito ao cmd/bootstrap.
func MustUse() *Cadeia {
	if instance == nil {
		panic(ErrNaoInicializada)
	}
	return instance
}

// ResetarParaTeste restaura o estado do singleton — uso EXCLUSIVO dos testes.
func ResetarParaTeste() {
	instance = nil
	initErr = nil
	once = sync.Once{}
}

// --- Funções de pacote consumidas pelos Routes() dos controllers ----------

// SetContextAuthorization valida JWT Bearer ou X-Api-Key e injeta a identidade.
func SetContextAuthorization() gin.HandlerFunc { return Use().SetContextAuthorization() }

// ResolveWorkspace resolve o workspace pelo Host (ou X-Workspace-Id fora dele)
// e injeta o escopo + permissões efetivas.
func ResolveWorkspace() gin.HandlerFunc { return Use().ResolveWorkspace() }

// RequirePermission exige a permissão granular exata da rota.
func RequirePermission(permissao string) gin.HandlerFunc { return Use().RequirePermission(permissao) }
