package licenca

import (
	"errors"
	"sync"

	"gorm.io/gorm"

	modellicenca "workspace-api/internal/licensing/model/licenca"
	"workspace-api/internal/pkg/errobserve"
)

var (
	controllerInstance Controller
	serviceInstance    Service
	repositoryInstance Repository
	once               sync.Once
	initErr            error
	ErrNotInitialized  = errors.New("controller licença não inicializado")
)

// severidadesErros classifica cada sentinela catalogada no errors.go para a
// observação de erros (evolução errobserve): recusas esperadas de negócio
// (4xx) = warn. Sentinela nova sem entrada AQUI reprova no boot.
var severidadesErros = map[error]errobserve.Severidade{
	ErrNotFound:       errobserve.SeveridadeWarn,
	ErrInvalidInput:   errobserve.SeveridadeWarn,
	ErrJaConcedida:    errobserve.SeveridadeWarn,
	ErrSemAcesso:      errobserve.SeveridadeWarn,
	ErrModuloInvalido: errobserve.SeveridadeWarn,

	ErrModuloNaoEncontrado: errobserve.SeveridadeWarn,
}

// observadorErros observa TODO erro devolvido pelo service deste subdomínio.
var observadorErros = errobserve.For(modellicenca.Dominio, modellicenca.Subdominio,
	errobserve.DoCatalogo(errorCatalog, severidadesErros))

// UseLicenca agrupa todas as camadas (Repository, Service, Controller).
type UseLicenca struct {
	Repository Repository
	Service    Service
	Controller Controller
}

// New inicializa o singleton do subdomínio montando repo → service →
// controller. Chamado UMA vez pelo cmd/bootstrap; o buscador do catálogo
// (contrato BuscadorModulos, implementado pelo irmão módulo e ligado no
// bootstrap) e a trilha de auditoria assíncrona (#9) entram aqui.
func New(db *gorm.DB, modulos BuscadorModulos, opcoes ...OpcaoServico) (Controller, error) {
	once.Do(func() {
		if db == nil {
			initErr = errors.New("conexão com o banco não pode ser nula")
			return
		}
		repositoryInstance = NewRepository(db)
		serviceInstance = NewService(repositoryInstance, modulos, opcoes...)
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
func MustUse() *UseLicenca {
	if controllerInstance == nil || serviceInstance == nil || repositoryInstance == nil {
		panic(ErrNotInitialized)
	}
	return &UseLicenca{
		Repository: repositoryInstance,
		Service:    serviceInstance,
		Controller: controllerInstance,
	}
}
