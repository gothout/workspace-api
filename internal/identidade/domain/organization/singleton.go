package organization

import (
	"errors"
	"log/slog"
	"sync"

	"gorm.io/gorm"

	"workspace-api/internal/pkg/config"
)

var (
	controllerInstance Controller
	serviceInstance    Service
	repositoryInstance Repository
	chavesInstance     RepositorioApiKeys
	once               sync.Once
	initErr            error
	ErrNotInitialized  = errors.New("controller organization não inicializado")
)

// UseOrganization agrupa todas as camadas (Repository, Service, Controller).
type UseOrganization struct {
	Repository         Repository
	RepositorioApiKeys RepositorioApiKeys
	Service            Service
	Controller         Controller
}

// New inicializa o singleton do subdomínio montando repo → service →
// controller. Chamado UMA vez pelo cmd/bootstrap; os dois lados da cascata
// (suspensor de workspaces desde a F3 e encerrador de sessões dos usuários
// desde a R4 — contratos ligados no bootstrap), a leitura do base_domain e
// a trilha de auditoria assíncrona (#9, opção variadic) entram aqui.
func New(db *gorm.DB, suspensore SuspendedorWorkspaces, encerradorSessoes EncerradorSessoesUsuarios, opcoes ...OpcaoServico) (Controller, error) {
	once.Do(func() {
		if db == nil {
			initErr = errors.New("conexão com o banco não pode ser nula")
			return
		}
		if suspensore == nil {
			initErr = errors.New("suspensor de workspaces ausente na montagem do organization")
			return
		}
		if encerradorSessoes == nil {
			initErr = errors.New("encerrador de sessões ausente na montagem do organization")
			return
		}
		repositoryInstance = NewRepository(db)
		chavesInstance = NewRepositorioApiKeys(db)
		serviceInstance = NewService(repositoryInstance, chavesInstance, suspensore, encerradorSessoes, lerBaseDomain, opcoes...)
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
func MustUse() *UseOrganization {
	if controllerInstance == nil || serviceInstance == nil || repositoryInstance == nil || chavesInstance == nil {
		panic(ErrNotInitialized)
	}
	return &UseOrganization{
		Repository:         repositoryInstance,
		RepositorioApiKeys: chavesInstance,
		Service:            serviceInstance,
		Controller:         controllerInstance,
	}
}

// lerBaseDomain injeta a leitura do base_domain no service sem acoplá-lo ao
// singleton de config — testes passam stub pelo NewService.
func lerBaseDomain() (string, error) {
	cfg, err := config.Use()
	if err != nil {
		slog.Error("organization: config indisponível para validar domínio custom", "causa", err.Error())
		return "", err
	}
	return cfg.App.BaseDomain, nil
}
