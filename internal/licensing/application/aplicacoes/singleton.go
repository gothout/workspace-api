package aplicacoes

import (
	"errors"
	"sync"

	"workspace-api/internal/pkg/errobserve"
)

var (
	controllerInstance Controller
	serviceInstance    Service
	once               sync.Once
	initErr            error
	ErrNotInitialized  = errors.New("controller aplicações não inicializado")
)

// severidadesErros classifica cada sentinela catalogada para a observação de
// erros (errobserve): recusa esperada = warn.
var severidadesErros = map[error]errobserve.Severidade{
	ErrInvalidInput: errobserve.SeveridadeWarn,
}

// observadorErros observa TODO erro devolvido pelo service da aplicação.
var observadorErros = errobserve.For(Dominio, Subdominio,
	errobserve.DoCatalogo(errorCatalog, severidadesErros))

// UseAplicacoes agrupa as camadas da aplicação (sem repository/model —
// aplicação não persiste nada próprio).
type UseAplicacoes struct {
	Service    Service
	Controller Controller
}

// New inicializa o singleton da aplicação montando service → controller.
func New(deps Dependencias) (Controller, error) {
	once.Do(func() {
		if deps.Provedor == nil {
			initErr = errors.New("contrato ausente na montagem da aplicação aplicacoes (provedor de acessos)")
			return
		}
		serviceInstance = NewService(deps.Provedor)
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

// Dependencias carrega os contratos ligados no cmd/bootstrap.
type Dependencias struct {
	Provedor ProvedorAcessos
}
