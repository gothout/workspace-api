package provisionamento

import (
	"errors"
	"sync"

	modelworkspace "workspace-api/internal/identidade/model/workspace"
	"workspace-api/internal/pkg/errobserve"
)

var (
	controllerInstance Controller
	serviceInstance    Service
	once               sync.Once
	initErr            error
	ErrNotInitialized  = errors.New("controller provisionamento não inicializado")
)

// severidadesErros classifica cada sentinela catalogada no errors.go para a
// observação de erros (evolução errobserve): recusas esperadas do fluxo =
// warn; tentativa de provisionar fora do poder de plataforma e papel ausente
// = error (sinal de configuração/segurança). Sentinela nova sem entrada AQUI
// reprova no boot. Código e mensagem vêm do errorCatalog do errors.go —
// fonte única; todo retorno de erro do service passa pelo observador via
// service_observado.go, com o erro devolvido intacto.
var severidadesErros = map[error]errobserve.Severidade{
	ErrInvalidInput:             errobserve.SeveridadeWarn,
	ErrOrganizacaoNaoEncontrada: errobserve.SeveridadeWarn,
	ErrOrganizacaoInativa:       errobserve.SeveridadeWarn,
	ErrJaProvisionado:           errobserve.SeveridadeWarn,
	ErrSlugIndisponivel:         errobserve.SeveridadeWarn,
	ErrEmailEmUso:               errobserve.SeveridadeWarn,
	ErrSemPoderPlataforma:       errobserve.SeveridadeError,
	ErrPapelAusente:             errobserve.SeveridadeError,

	modelworkspace.ErrSlugInvalido: errobserve.SeveridadeWarn,
	modelworkspace.ErrNomeInvalido: errobserve.SeveridadeWarn,
}

// observadorErros observa TODO erro devolvido pelo service da aplicação.
var observadorErros = errobserve.For(Dominio, Subdominio,
	errobserve.DoCatalogo(errorCatalog, severidadesErros))

// UseProvisionamento agrupa as camadas da aplicação (sem repository/model —
// aplicação não persiste nada próprio).
type UseProvisionamento struct {
	Service    Service
	Controller Controller
}

// New inicializa o singleton da aplicação montando service → controller.
// Chamado UMA vez pelo cmd/bootstrap com os contratos ligados; faltar
// qualquer um é erro de boot (provisionamento pela metade não sobe).
func New(deps Dependencias) (Controller, error) {
	once.Do(func() {
		if deps.Organizacoes == nil || deps.Workspaces == nil || deps.Usuarios == nil || deps.Papeis == nil {
			initErr = errors.New("contratos ausentes na montagem da aplicação provisionamento (organizações/workspaces/usuários/papéis)")
			return
		}
		serviceInstance = NewService(deps)
		controllerInstance = NewController(serviceInstance)
	})
	return controllerInstance, initErr
}

// Use devolve o controller singleton; erro se não inicializado.
func Use() (Controller, error) {
	if controllerInstance == nil {
		return nil, ErrNotInitialized
	}
	return controllerInstance, nil
}
