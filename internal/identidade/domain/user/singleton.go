package user

import (
	"errors"
	"sync"

	"gorm.io/gorm"
)

var (
	controllerInstance  Controller
	serviceInstance     Service
	repositoryInstance  Repository
	atribuicoesInstance RepositorioAtribuicoes
	once                sync.Once
	initErr             error
	ErrNotInitialized   = errors.New("controller user não inicializado")
)

// UseUser agrupa todas as camadas (Repository, RepositorioAtribuicoes,
// Service, Controller).
type UseUser struct {
	Repository             Repository
	RepositorioAtribuicoes RepositorioAtribuicoes
	Service                Service
	Controller             Controller
}

// New inicializa o singleton do subdomínio montando repo → service →
// controller. Chamado UMA vez pelo cmd/bootstrap; o validador de workspaces
// (contrato com o irmão, ligado no bootstrap) e a credencial bcrypt entram aqui.
func New(db *gorm.DB, validador ValidadorWorkspaces) (Controller, error) {
	once.Do(func() {
		if db == nil {
			initErr = errors.New("conexão com o banco não pode ser nula")
			return
		}
		if validador == nil {
			initErr = errors.New("validador de workspaces ausente na montagem do user")
			return
		}
		repositoryInstance = NewRepository(db)
		atribuicoesInstance = NewRepositorioAtribuicoes(db)
		serviceInstance = NewService(repositoryInstance, atribuicoesInstance, validador, NovasCredenciaisBcrypt())
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
func MustUse() *UseUser {
	if controllerInstance == nil || serviceInstance == nil || repositoryInstance == nil || atribuicoesInstance == nil {
		panic(ErrNotInitialized)
	}
	return &UseUser{
		Repository:             repositoryInstance,
		RepositorioAtribuicoes: atribuicoesInstance,
		Service:                serviceInstance,
		Controller:             controllerInstance,
	}
}
