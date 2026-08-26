package ativacao

import (
	"errors"
	"sync"

	"gorm.io/gorm"

	modelativacao "workspace-api/internal/licensing/model/ativacao"
	"workspace-api/internal/pkg/errobserve"
)

var (
	controllerInstance Controller
	serviceInstance    Service
	repositoryInstance Repository
	once               sync.Once
	initErr            error
	ErrNotInitialized  = errors.New("controller ativação não inicializado")
)

// severidadesErros classifica cada sentinela catalogada no errors.go para a
// observação de erros (evolução errobserve): recusas esperadas de negócio
// (4xx) = warn. Sentinela nova sem entrada AQUI reprova no boot.
var severidadesErros = map[error]errobserve.Severidade{
	ErrNotFound:          errobserve.SeveridadeWarn,
	ErrInvalidInput:      errobserve.SeveridadeWarn,
	ErrJaAtivada:         errobserve.SeveridadeWarn,
	ErrSemLicenca:        errobserve.SeveridadeWarn,
	ErrWorkspaceInvalido: errobserve.SeveridadeWarn,
	ErrModuloInvalido:    errobserve.SeveridadeWarn,

	ErrModuloNaoEncontrado:    errobserve.SeveridadeWarn,
	ErrWorkspaceNaoEncontrado: errobserve.SeveridadeWarn,
}

// observadorErros observa TODO erro devolvido pelo service deste subdomínio.
var observadorErros = errobserve.For(modelativacao.Dominio, modelativacao.Subdominio,
	errobserve.DoCatalogo(errorCatalog, severidadesErros))

// UseAtivacao agrupa todas as camadas (Repository, Service, Controller).
type UseAtivacao struct {
	Repository Repository
	Service    Service
	Controller Controller
}

// New inicializa o singleton do subdomínio montando repo → service →
// controller. Chamado UMA vez pelo cmd/bootstrap; os TRÊS contratos com os
// irmãos (módulo, licença, workspace — ligados no bootstrap por adaptadores
// que resolvem NA CHAMADA) e a trilha assíncrona (#9) entram aqui. Contrato
// nil recusa a ativação (fail-closed), nunca abre.
func New(db *gorm.DB, modulos BuscadorModulos, licencas VerificadorLicencas, workspaces ValidadorWorkspaces, opcoes ...OpcaoServico) (Controller, error) {
	once.Do(func() {
		if db == nil {
			initErr = errors.New("conexão com o banco não pode ser nula")
			return
		}
		repositoryInstance = NewRepository(db)
		serviceInstance = NewService(repositoryInstance, modulos, licencas, workspaces, opcoes...)
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
func MustUse() *UseAtivacao {
	if controllerInstance == nil || serviceInstance == nil || repositoryInstance == nil {
		panic(ErrNotInitialized)
	}
	return &UseAtivacao{
		Repository: repositoryInstance,
		Service:    serviceInstance,
		Controller: controllerInstance,
	}
}
