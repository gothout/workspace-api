package workspace

import (
	"errors"
	"sync"

	"gorm.io/gorm"
)

var (
	controllerInstance Controller
	serviceInstance    Service
	repositoryInstance Repository
	once               sync.Once
	initErr            error
	ErrNotInitialized  = errors.New("controller workspace não inicializado")
)

// UseWorkspace agrupa todas as camadas (Repository, Service, Controller).
type UseWorkspace struct {
	Repository Repository
	Service    Service
	Controller Controller
}

// New inicializa o singleton do subdomínio montando repo → service →
// controller. Chamado UMA vez pelo cmd/bootstrap; o cache de resolução
// (contrato CacheResolucao, implementação Redis é evolução) entra aqui —
// nil é operação normal (sem cache, só mais caro).
func New(db *gorm.DB, cache CacheResolucao) (Controller, error) {
	once.Do(func() {
		if db == nil {
			initErr = errors.New("conexão com o banco não pode ser nula")
			return
		}
		repositoryInstance = NewRepository(db)
		serviceInstance = NewService(repositoryInstance, cache)
		controllerInstance = NewController(serviceInstance)
	})
	return controllerInstance, initErr
}

// Use devolve o controller singleton; erro se não inicializado.
// Usado pelo registro de rotas: sem boot, a rota não é registrada e o motivo sai no log.
func Use() (Controller, error) {
	if controllerInstance == nil {
		return nil, ErrNotInitialized
	}
	return controllerInstance, nil
}

// MustUse devolve todas as camadas; entra em pânico se não inicializado.
// Restrito ao cmd/bootstrap — panic fora do boot é proibido.
func MustUse() *UseWorkspace {
	if controllerInstance == nil || serviceInstance == nil || repositoryInstance == nil {
		panic(ErrNotInitialized)
	}
	return &UseWorkspace{
		Repository: repositoryInstance,
		Service:    serviceInstance,
		Controller: controllerInstance,
	}
}
