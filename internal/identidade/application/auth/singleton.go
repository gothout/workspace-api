package auth

import (
	"errors"
	"sync"
)

var (
	controllerInstance Controller
	serviceInstance    Service
	once               sync.Once
	initErr            error
	ErrNotInitialized  = errors.New("controller auth não inicializado")
)

// UseAuth agrupa as camadas da aplicação (sem repository/model — aplicação
// não persiste nada próprio).
type UseAuth struct {
	Service    Service
	Controller Controller
}

// New inicializa o singleton da aplicação montando service → controller.
// Chamado UMA vez pelo cmd/bootstrap com os três contratos ligados; faltar
// qualquer um é erro de boot (aplicação de autenticação não sobra pela metade).
func New(deps Dependencias) (Controller, error) {
	once.Do(func() {
		if deps.Usuarios == nil || deps.Emissor == nil || deps.Organizacoes == nil {
			initErr = errors.New("contratos ausentes na montagem da aplicação auth (usuarios/emissor/organizações)")
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

// MustUse devolve todas as camadas; entra em pânico se não inicializado.
// Restrito ao cmd/bootstrap — panic fora do boot é proibido.
func MustUse() *UseAuth {
	if controllerInstance == nil || serviceInstance == nil {
		panic(ErrNotInitialized)
	}
	return &UseAuth{Service: serviceInstance, Controller: controllerInstance}
}
