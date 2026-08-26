package workspace

import (
	"errors"
	"sync"

	"gorm.io/gorm"

	modelworkspace "workspace-api/internal/identidade/model/workspace"

	"workspace-api/internal/pkg/errobserve"
)

var (
	controllerInstance Controller
	serviceInstance    Service
	repositoryInstance Repository
	once               sync.Once
	initErr            error
	ErrNotInitialized  = errors.New("controller workspace não inicializado")
)

// severidadesErros classifica cada sentinela catalogada no errors.go para a
// observação de erros (evolução errobserve): recusas esperadas de negócio
// (4xx) = warn. Sentinela nova sem entrada AQUI reprova no boot
// (errobserve.DoCatalogo panica). Código e mensagem vêm do errorCatalog do
// errors.go — fonte única; todo retorno de erro do service passa pelo
// observador via service_observado.go, com o erro devolvido intacto.
var severidadesErros = map[error]errobserve.Severidade{
	ErrNotFound:                    errobserve.SeveridadeWarn,
	ErrInvalidInput:                errobserve.SeveridadeWarn,
	ErrSlugEmUso:                   errobserve.SeveridadeWarn,
	ErrSlugReservado:               errobserve.SeveridadeWarn,
	modelworkspace.ErrSlugInvalido: errobserve.SeveridadeWarn,
	modelworkspace.ErrNomeInvalido: errobserve.SeveridadeWarn,
	modelworkspace.ErrJaInativo:    errobserve.SeveridadeWarn,
	modelworkspace.ErrJaAtivo:      errobserve.SeveridadeWarn,
	// UX4: tentativa de alcançar organization alheia é sinal de segurança —
	// mesma classificação do recorte dos logs (E5); alvo inválido da criação
	// da plataforma é recusa esperada.
	ErrForaDoEscopo:             errobserve.SeveridadeError,
	ErrOrganizacaoNaoEncontrada: errobserve.SeveridadeWarn,
	ErrOrganizacaoInativa:       errobserve.SeveridadeWarn,
}

// observadorErros observa TODO erro devolvido pelo service deste subdomínio.
var observadorErros = errobserve.For(modelworkspace.Dominio, modelworkspace.Subdominio,
	errobserve.DoCatalogo(errorCatalog, severidadesErros))

// UseWorkspace agrupa todas as camadas (Repository, Service, Controller).
type UseWorkspace struct {
	Repository Repository
	Service    Service
	Controller Controller
}

// New inicializa o singleton do subdomínio montando repo → service →
// controller. Chamado UMA vez pelo cmd/bootstrap; o cache de resolução
// (contrato CacheResolucao, implementação Redis da #8) e a trilha de
// auditoria assíncrona (#9, opção variadic) entram aqui — nil é operação
// normal (sem cache, só mais caro; sem trilha, slog legado).
func New(db *gorm.DB, cache CacheResolucao, opcoes ...OpcaoServico) (Controller, error) {
	once.Do(func() {
		if db == nil {
			initErr = errors.New("conexão com o banco não pode ser nula")
			return
		}
		repositoryInstance = NewRepository(db)
		serviceInstance = NewService(repositoryInstance, cache, opcoes...)
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
