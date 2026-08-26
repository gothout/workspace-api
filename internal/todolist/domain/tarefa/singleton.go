package tarefa

import (
	"errors"
	"sync"

	"gorm.io/gorm"

	modeltarefa "workspace-api/internal/todolist/model/tarefa"
	"workspace-api/internal/pkg/errobserve"
)

var (
	controllerInstance Controller
	serviceInstance    Service
	repositoryInstance Repository
	once               sync.Once
	initErr            error
	ErrNotInitialized  = errors.New("controller tarefa não inicializado")
)

// severidadesErros classifica cada sentinela catalogada para a observação
// de erros (errobserve): recusas esperadas de negócio (4xx) = warn.
var severidadesErros = map[error]errobserve.Severidade{
	ErrNotFound:     errobserve.SeveridadeWarn,
	ErrInvalidInput: errobserve.SeveridadeWarn,

	modeltarefa.ErrTituloInvalido: errobserve.SeveridadeWarn,
	modeltarefa.ErrJaConcluida:    errobserve.SeveridadeWarn,
	modeltarefa.ErrJaPendente:     errobserve.SeveridadeWarn,
}

// observadorErros observa TODO erro devolvido pelo service deste subdomínio.
var observadorErros = errobserve.For(modeltarefa.Dominio, modeltarefa.Subdominio,
	errobserve.DoCatalogo(errorCatalog, severidadesErros))

// UseTarefa agrupa todas as camadas (Repository, Service, Controller).
type UseTarefa struct {
	Repository Repository
	Service    Service
	Controller Controller
}

// New inicializa o singleton do subdomínio montando repo → service →
// controller. Chamado UMA vez pelo cmd/bootstrap.
func New(db *gorm.DB, opcoes ...OpcaoServico) (Controller, error) {
	once.Do(func() {
		if db == nil {
			initErr = errors.New("conexão com o banco não pode ser nula")
			return
		}
		repositoryInstance = NewRepository(db)
		serviceInstance = NewService(repositoryInstance, opcoes...)
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
func MustUse() *UseTarefa {
	if controllerInstance == nil || serviceInstance == nil || repositoryInstance == nil {
		panic(ErrNotInitialized)
	}
	return &UseTarefa{
		Repository: repositoryInstance,
		Service:    serviceInstance,
		Controller: controllerInstance,
	}
}
