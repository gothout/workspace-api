package modulo

import (
	"errors"
	"sync"

	"gorm.io/gorm"

	modelmodulo "workspace-api/internal/licensing/model/modulo"
	"workspace-api/internal/pkg/errobserve"
)

var (
	controllerInstance Controller
	serviceInstance    Service
	repositoryInstance Repository
	once               sync.Once
	initErr            error
	ErrNotInitialized  = errors.New("controller módulo não inicializado")
)

// severidadesErros classifica cada sentinela catalogada no errors.go para a
// observação de erros (evolução errobserve): recusas esperadas de negócio
// (4xx) = warn. Sentinela nova sem entrada AQUI reprova no boot
// (errobserve.DoCatalogo panica). Código e mensagem vêm do errorCatalog do
// errors.go — fonte única.
var severidadesErros = map[error]errobserve.Severidade{
	ErrNotFound:     errobserve.SeveridadeWarn,
	ErrInvalidInput: errobserve.SeveridadeWarn,
	ErrSlugEmUso:    errobserve.SeveridadeWarn,
	ErrModuloEmUso:  errobserve.SeveridadeWarn,

	modelmodulo.ErrSlugInvalido: errobserve.SeveridadeWarn,
	modelmodulo.ErrNomeInvalido: errobserve.SeveridadeWarn,
	modelmodulo.ErrJaAtivo:      errobserve.SeveridadeWarn,
	modelmodulo.ErrJaInativo:    errobserve.SeveridadeWarn,
}

// observadorErros observa TODO erro devolvido pelo service deste subdomínio.
var observadorErros = errobserve.For(modelmodulo.Dominio, modelmodulo.Subdominio,
	errobserve.DoCatalogo(errorCatalog, severidadesErros))

// UseModulo agrupa todas as camadas (Repository, Service, Controller).
type UseModulo struct {
	Repository Repository
	Service    Service
	Controller Controller
}

// New inicializa o singleton do subdomínio montando repo → service →
// controller. Chamado UMA vez pelo cmd/bootstrap; o verificador de licenças
// (contrato VerificadorLicencas, implementado pelo irmão licenca e ligado no
// bootstrap) e a trilha de auditoria assíncrona (#9) entram aqui — nil é
// operação normal para a trilha; verificador nil recusa remoção (fail-closed).
func New(db *gorm.DB, licencas VerificadorLicencas, opcoes ...OpcaoServico) (Controller, error) {
	once.Do(func() {
		if db == nil {
			initErr = errors.New("conexão com o banco não pode ser nula")
			return
		}
		repositoryInstance = NewRepository(db)
		serviceInstance = NewService(repositoryInstance, licencas, opcoes...)
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
func MustUse() *UseModulo {
	if controllerInstance == nil || serviceInstance == nil || repositoryInstance == nil {
		panic(ErrNotInitialized)
	}
	return &UseModulo{
		Repository: repositoryInstance,
		Service:    serviceInstance,
		Controller: controllerInstance,
	}
}
