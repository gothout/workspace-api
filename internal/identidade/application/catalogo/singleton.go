package catalogo

import (
	"errors"
	"sync"
)

var (
	controllerInstance Controller
	serviceInstance    Service
	once               sync.Once
	initErr            error
	ErrNotInitialized  = errors.New("controller catalogo não inicializado")
)

// UseCatalogo agrupa as camadas da aplicação (sem repository/model —
// aplicação não persiste nada próprio).
type UseCatalogo struct {
	Service    Service
	Controller Controller
}

// New inicializa o singleton da aplicação montando service → controller.
// Chamado UMA vez pelo cmd/bootstrap DEPOIS de todos os subdomínios — os
// catálogos agregados entregues nos contratos precisam dos imports deles
// já feitos.
func New(deps Dependencias) (Controller, error) {
	once.Do(func() {
		if deps.Permissoes == nil {
			initErr = errors.New("contrato ausente na montagem da aplicação catalogo (permissões agregadas)")
			return
		}
		if deps.Eventos == nil {
			initErr = errors.New("contrato ausente na montagem da aplicação catalogo (eventos agregados)")
			return
		}
		serviceInstance = NewService(deps.Permissoes, deps.Eventos)
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
func MustUse() *UseCatalogo {
	if controllerInstance == nil || serviceInstance == nil {
		panic(ErrNotInitialized)
	}
	return &UseCatalogo{Service: serviceInstance, Controller: controllerInstance}
}
