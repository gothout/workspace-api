package logs

import (
	"errors"
	"sync"

	"workspace-api/internal/pkg/errobserve"
)

var (
	controllerInstance Controller
	serviceInstance    Service
	once               sync.Once
	initErr            error
	ErrNotInitialized  = errors.New("controller logs não inicializado")
)

// severidadesErros classifica cada sentinela catalogada no errors.go para a
// observação de erros (evolução errobserve): filtro inválido e destino
// ausente = warn (esperados); pedido FORA do recorte = error — tentativa de
// alcançar dados alheios é sinal de segurança. Sentinela nova sem entrada
// AQUI reprova no boot. Código e mensagem vêm do errorCatalog — fonte única.
var severidadesErros = map[error]errobserve.Severidade{
	ErrFiltroInvalido: errobserve.SeveridadeWarn,
	ErrIndisponivel:   errobserve.SeveridadeWarn,
	ErrForaDoEscopo:   errobserve.SeveridadeError,
}

// observadorErros observa TODO erro devolvido pelo service da aplicação.
var observadorErros = errobserve.For(Dominio, Subdominio,
	errobserve.DoCatalogo(errorCatalog, severidadesErros))

// UseLogs agrupa as camadas da aplicação (sem repository/model — aplicação
// não persiste nada próprio).
type UseLogs struct {
	Service    Service
	Controller Controller
}

// New inicializa o singleton da aplicação montando service → controller.
// Chamado UMA vez pelo cmd/bootstrap com o contrato das trilhas ligado;
// faltar é erro de boot — leitura pela metade não sobe.
func New(deps Dependencias) (Controller, error) {
	once.Do(func() {
		if deps.Trilhas == nil {
			initErr = errors.New("contrato ausente na montagem da aplicação logs (consulta de trilhas)")
			return
		}
		serviceInstance = NewService(deps.Trilhas)
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
func MustUse() *UseLogs {
	if controllerInstance == nil || serviceInstance == nil {
		panic(ErrNotInitialized)
	}
	return &UseLogs{Service: serviceInstance, Controller: controllerInstance}
}
