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
// Chamado UMA vez pelo cmd/bootstrap com os contratos ligados: trilhas e
// opções de filtro são EXIGIDOS (faltar é erro de boot — leitura pela metade
// não sobe); o enriquecimento de usuários (UX2) é OPCIONAL: sem ele as
// linhas saem com os campos vazios.
func New(deps Dependencias) (Controller, error) {
	once.Do(func() {
		if deps.Trilhas == nil {
			initErr = errors.New("contrato ausente na montagem da aplicação logs (consulta de trilhas)")
			return
		}
		if deps.Opcoes == nil {
			initErr = errors.New("contrato ausente na montagem da aplicação logs (opções de filtro)")
			return
		}
		opcoes := []OpcaoServico{ComProvedorOpcoes(deps.Opcoes)}
		if deps.Usuarios != nil {
			opcoes = append(opcoes, ComResolvedorUsuarios(deps.Usuarios))
		}
		serviceInstance = NewService(deps.Trilhas, opcoes...)
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
