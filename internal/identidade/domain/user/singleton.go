package user

import (
	"errors"
	"sync"

	"gorm.io/gorm"

	modeluser "workspace-api/internal/identidade/model/user"

	"workspace-api/internal/pkg/errobserve"
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

// severidadesErros classifica cada sentinela catalogada no errors.go para a
// observação de erros (evolução errobserve): recusas esperadas de negócio
// (4xx) = warn; falha interna catalogada (hash não processado, 500) =
// error. Sentinela nova sem entrada AQUI reprova no boot (errobserve.
// DoCatalogo panica). Código e mensagem vêm do errorCatalog do errors.go —
// fonte única; todo retorno de erro do service passa pelo observador via
// service_observado.go, com o erro devolvido intacto.
var severidadesErros = map[error]errobserve.Severidade{
	ErrNotFound:                     errobserve.SeveridadeWarn,
	ErrInvalidInput:                 errobserve.SeveridadeWarn,
	ErrEmailEmUso:                   errobserve.SeveridadeWarn,
	ErrCredenciaisInvalidas:         errobserve.SeveridadeWarn,
	ErrPapelNaoEncontrado:           errobserve.SeveridadeWarn,
	ErrAtribuicaoDuplicada:          errobserve.SeveridadeWarn,
	ErrAtribuicaoNaoEncontrada:      errobserve.SeveridadeWarn,
	ErrWorkspaceInvalido:            errobserve.SeveridadeWarn,
	ErrRefreshTokenInvalido:         errobserve.SeveridadeWarn,
	ErrHierarquiaInsufficiente:      errobserve.SeveridadeWarn,
	ErrOperadorNaoIdentificado:      errobserve.SeveridadeWarn,
	modeluser.ErrEmailInvalido:      errobserve.SeveridadeWarn,
	modeluser.ErrNomeInvalido:       errobserve.SeveridadeWarn,
	modeluser.ErrSenhaInvalida:      errobserve.SeveridadeWarn,
	modeluser.ErrHashAusente:        errobserve.SeveridadeError, // credencial não processada é falha interna (500)
	modeluser.ErrJaInativo:          errobserve.SeveridadeWarn,
	modeluser.ErrJaAtivo:            errobserve.SeveridadeWarn,
	modeluser.ErrAtribuicaoInvalida: errobserve.SeveridadeWarn,
	modeluser.ErrRefreshInvalido:    errobserve.SeveridadeWarn,
}

// observadorErros observa TODO erro devolvido pelo service deste subdomínio.
var observadorErros = errobserve.For(modeluser.Dominio, modeluser.Subdominio,
	errobserve.DoCatalogo(errorCatalog, severidadesErros))

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
// (contrato com o irmão, ligado no bootstrap) e a credencial bcrypt entram
// aqui. Opções opcionais (ex.: observador de invalidação de cache da
// evolução Redis #8) passam retas ao service.
func New(db *gorm.DB, validador ValidadorWorkspaces, opcoes ...OpcaoServico) (Controller, error) {
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
		serviceInstance = NewService(repositoryInstance, atribuicoesInstance, validador, NovasCredenciaisBcrypt(), opcoes...)
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
