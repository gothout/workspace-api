// Package errobserve é o observador de ERROS por subdomínio (evolução
// errobserve): todo retorno de erro do service passa por `obs.Observe(ctx,
// err)` — que devolve o erro INTACTO — e vira um evento estruturado de
// telemetria (código estável + severidade + identificadores do ctx) entregue
// aos sinks ligados no cmd/bootstrap.
//
// O catálogo de erros do errors.go de cada subdomínio (doc 05, registrado no
// rest_err) é a FONTE DOS CÓDIGOS: o singleton monta as entradas com
// DoCatalogo(errorCatalog, severidades) e declara só a severidade de cada
// sentinela. Sentinela fora do catálogo do subdomínio vira evento
// DESCONHECIDO e sobe como critical — erro sem nome conhecido é sempre o
// pior caso até prova em contrário.
//
// Namespace reservado: eventos de PLATAFORMA (boot, migrations.up, shutdown,
// degradação de dependência) vivem no dominio "sistema" — subdomínio de
// negócio NÃO pode registrar observador nem código nesse namespace (reprovado
// em boot/teste pelo For/Novo).
//
// Pacote-FOLHA (regra 1 de agents/01): importa apenas outros pacotes de
// internal/pkg. Telemetria nunca muda a resposta ao cliente: Observe nunca
// erra nem bloqueia, e cada sink roda com recuperação de pânico própria.
package errobserve

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"workspace-api/internal/pkg/orgctx"
	"workspace-api/internal/pkg/rest_err"
)

// NamespaceReservado é o dominio dos eventos de PLATAFORMA — reservado para
// boot, migrations, shutdown e degradação de dependência. Subdomínio de
// negócio não registra nada aqui.
const NamespaceReservado = "sistema"

// CodigoDesconhecido identifica o evento cujo erro não casou com nenhuma
// sentinela catáloga do subdomínio (sem namespace de negócio — não é código
// registrado, é a classificação do fallback).
const CodigoDesconhecido = "desconhecido"

// ErrDegradacao é a sentinela da PLATAFORMA para dependência degradada
// (Redis/ClickHouse fora no boot): o bootstrap observa com ela para virar o
// evento sistema.degradacao_dependencia.
var ErrDegradacao = errors.New("dependência degradada")

// ErrMigracao é a sentinela da PLATAFORMA para falha aplicando migrations no
// boot: o bootstrap observa com ela para virar o evento
// sistema.migrations.up (critical — o processo não sobe).
var ErrMigracao = errors.New("migrations falharam")

// Severidade classifica o erro observado — definida POR CATÁLOGO na montagem
// do observador, nunca inferida em runtime.
type Severidade string

const (
	SeveridadeWarn     Severidade = "warn"     // recusa esperada de negócio (4xx)
	SeveridadeError    Severidade = "error"    // sinal operacional relevante (segurança, 5xx catalogado)
	SeveridadeCritical Severidade = "critical" // desconhecido/falha grave — dispara [ALERTA]
)

// Valida confere se o valor está no conjunto fechado.
func (s Severidade) Valida() bool {
	switch s {
	case SeveridadeWarn, SeveridadeError, SeveridadeCritical:
		return true
	}
	return false
}

// Entrada liga UMA sentinela do subdomínio ao código estável dela (o MESMO
// registrado no errors.go/rest_err), à mensagem PT-BR e à severidade com que
// o erro deve ser observado.
type Entrada struct {
	Erro       error      // sentinela; casamento por errors.Is
	Codigo     string     // estável, igual ao do catálogo do rest_err
	Mensagem   string     // PT-BR: descrição do evento (default: mensagem do catálogo)
	Severidade Severidade // warn | error | critical
}

// Evento é uma linha da trilha de ERROS — vocabulário fechado (códigos),
// identificadores do ctx e a causa como texto (para o log; nunca volta ao
// cliente).
type Evento struct {
	Instante         time.Time
	Dominio          string
	Subdominio       string
	Codigo           string // CodigoDesconhecido quando a sentinela não casou
	Mensagem         string // PT-BR do catálogo; vazia quando desconhecido
	Severidade       Severidade
	Desconhecido     bool
	OrganizationUUID string
	WorkspaceUUID    string
	UserUUID         string
	RayTrace         string
	Causa            string // err.Error() do erro original
}

// EventoMeta é a forma pública de UM erro observável no catálogo agregado
// (GET /api/system/eventos e CLI): código + descrição + severidade.
type EventoMeta struct {
	Codigo     string
	Descricao  string
	Severidade string
}

// Sink é a cara de quem consome os eventos de erro (slog sempre ativo,
// ClickHouse da E2 quando ligado, alerta agregado). Contrato do consumidor:
// Registrar NUNCA bloqueia além do custo de enfileirar e NUNCA devolve erro —
// telemetria não pode mudar a resposta ao cliente (o despachante ainda
// recupera pânico como última linha de defesa).
type Sink interface {
	Registrar(Evento)
}

// Observador observa os erros DE UM subdomínio: casa o erro contra as
// sentinelas declaradas e despacha o evento aos sinks globais. Imutável após
// Novo — seguro para uso concorrente.
type Observador struct {
	dominio    string
	subdominio string
	entradas   []Entrada // ordem declarada (DoCatalogo ordena por código) = ordem de casamento
}

// For monta E REGISTRA o observador do subdomínio — a linha que mora no
// singleton.go de cada um (evolução errobserve). Reprova em boot/import:
// validação malformada, namespace reservado invadido ou registro duplicado
// PANICAM (mesmo espírito do RegistrarCatalogo do rest_err).
func For(dominio, subdominio string, entradas []Entrada) *Observador {
	obs, err := Novo(dominio, subdominio, entradas)
	if err != nil {
		panic(fmt.Sprintf("errobserve: %v", err))
	}
	registrar(obs)
	return obs
}

// ObservadorPlataforma monta o observador do namespace RESERVADO — uso
// exclusivo da plataforma (cmd/bootstrap): eventos de migrations, boot,
// shutdown e degradação. NÃO entra no registro global de negócio: o
// vocabulário do sistema é fixo deste pacote (CatalogoSistema) e já aparece
// nas rotas de catálogo sem depender de emissão. Todo código DEVE nascer com
// o prefixo reservado.
func ObservadorPlataforma(entradas []Entrada) *Observador {
	obs, err := novo(NamespaceReservado, "plataforma", entradas, true)
	if err != nil {
		panic(fmt.Sprintf("errobserve: %v", err))
	}
	return obs
}

// Novo é o construtor PURO (valida tudo, não registra): testes de validação
// usam ele direto; o singleton usa For. Regras:
//   - dominio/subdominio obrigatórios;
//   - dominio de negócio NÃO pode ser o namespace reservado;
//   - código de negócio NÃO pode nascer com "sistema." (reservado);
//   - toda entrada precisa de sentinela, código, severidade válida — e
//     códigos duplicados reprova (erro observável pela metade é mapping falso).
func Novo(dominio, subdominio string, entradas []Entrada) (*Observador, error) {
	return novo(dominio, subdominio, entradas, false)
}

// novo é o construtor interno; plataforma=true autoriza o namespace
// reservado e EXIGE o prefixo dele nos códigos.
func novo(dominio, subdominio string, entradas []Entrada, plataforma bool) (*Observador, error) {
	if strings.TrimSpace(dominio) == "" || strings.TrimSpace(subdominio) == "" {
		return nil, errors.New("dominio e subdominio são obrigatórios")
	}
	if !plataforma && dominio == NamespaceReservado {
		return nil, fmt.Errorf("namespace %q é reservado da plataforma — subdomínio de negócio não registra nele", NamespaceReservado)
	}
	vistos := map[string]bool{}
	for _, entrada := range entradas {
		if entrada.Erro == nil {
			return nil, errors.New("entrada sem sentinela (erro nil)")
		}
		if strings.TrimSpace(entrada.Codigo) == "" {
			return nil, fmt.Errorf("entrada da sentinela %v sem código estável", entrada.Erro)
		}
		if !entrada.Severidade.Valida() {
			return nil, fmt.Errorf("código %q com severidade inválida %q (use warn|error|critical)", entrada.Codigo, entrada.Severidade)
		}
		if plataforma && !strings.HasPrefix(entrada.Codigo, NamespaceReservado+".") {
			return nil, fmt.Errorf("código de plataforma fora do namespace reservado: %q", entrada.Codigo)
		}
		if !plataforma && strings.HasPrefix(entrada.Codigo, NamespaceReservado+".") {
			return nil, fmt.Errorf("código %q invade o namespace reservado %q", entrada.Codigo, NamespaceReservado+".")
		}
		if vistos[entrada.Codigo] {
			return nil, fmt.Errorf("código duplicado no catálogo do observador: %q", entrada.Codigo)
		}
		vistos[entrada.Codigo] = true
	}
	copia := make([]Entrada, len(entradas))
	copy(copia, entradas)
	sort.Slice(copia, func(i, j int) bool { return copia[i].Codigo < copia[j].Codigo })
	return &Observador{dominio: dominio, subdominio: subdominio, entradas: copia}, nil
}

// Dominio expõe o dominio dono do vocabulário (catálogos e testes).
func (o *Observador) Dominio() string { return o.dominio }

// Subdominio expõe o subdominio emissor (catálogos e testes).
func (o *Observador) Subdominio() string { return o.subdominio }

// Catalogo devolve o vocabulário de erros do subdomínio com metadados — é o
// que alimenta GET /api/system/eventos e o comando CLI de erros.
func (o *Observador) Catalogo() []EventoMeta {
	metas := make([]EventoMeta, 0, len(o.entradas))
	for _, entrada := range o.entradas {
		metas = append(metas, EventoMeta{
			Codigo:     entrada.Codigo,
			Descricao:  entrada.Mensagem,
			Severidade: string(entrada.Severidade),
		})
	}
	return metas
}

// Observe classifica e despacha o erro DEVOLVENDO-O INTACTO: nil não emite
// nada; sentinela catalogada vira evento com o código dela; qualquer outra
// coisa vira evento DESCONHECIDO com severidade critical. Nunca erra, nunca
// bloqueia o chamador — telemetria não pode mudar a resposta ao cliente.
func (o *Observador) Observe(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	evento := Evento{
		Instante:         time.Now().UTC(),
		Dominio:          o.dominio,
		Subdominio:       o.subdominio,
		OrganizationUUID: orgctx.OrganizationUUID(ctx).String(),
		WorkspaceUUID:    orgctx.WorkspaceUUID(ctx).String(),
		UserUUID:         orgctx.UserUUID(ctx).String(),
		RayTrace:         orgctx.RayTrace(ctx),
		Causa:            err.Error(),
	}
	for _, entrada := range o.entradas {
		if errors.Is(err, entrada.Erro) {
			evento.Codigo = entrada.Codigo
			evento.Mensagem = entrada.Mensagem
			evento.Severidade = entrada.Severidade
			despachar(evento)
			return err
		}
	}
	evento.Codigo = CodigoDesconhecido
	evento.Desconhecido = true
	evento.Severidade = SeveridadeCritical
	despachar(evento)
	return err
}

// DoCatalogo converte o catálogo do errors.go (mapa sentinela →
// rest_err.ErroCatalogado) nas entradas do observador, aplicando a
// severidade declarada para cada sentinela. O CÓDIGO E A MENSAGEM vêm do
// catálogo — fonte única (agents/02); aqui só se declara a severidade.
// Sentinela catalogada SEM severidade reprova em boot/import: observação
// parcial é mapping pela metade.
func DoCatalogo(catalogo map[error]rest_err.ErroCatalogado, severidades map[error]Severidade) []Entrada {
	entradas := make([]Entrada, 0, len(catalogo))
	for sentinela, registro := range catalogo {
		severidade, ok := severidades[sentinela]
		if !ok {
			panic(fmt.Sprintf("errobserve: sentinela catalogada sem severidade declarada: %q (%v)", registro.Codigo, sentinela))
		}
		if !severidade.Valida() {
			panic(fmt.Sprintf("errobserve: severidade inválida para %q: %q", registro.Codigo, severidade))
		}
		entradas = append(entradas, Entrada{
			Erro:       sentinela,
			Codigo:     registro.Codigo,
			Mensagem:   registro.Mensagem,
			Severidade: severidade,
		})
	}
	sort.Slice(entradas, func(i, j int) bool { return entradas[i].Codigo < entradas[j].Codigo })
	return entradas
}

// CatalogoSistema devolve o vocabulário FIXO dos eventos de plataforma — o
// conteúdo do namespace reservado, visível em GET /api/system/eventos mesmo
// sem emissão (é mapping, não telemetria). Só este pacote o declara.
func CatalogoSistema() []EventoMeta {
	return []EventoMeta{
		{Codigo: NamespaceReservado + ".boot", Descricao: "Processo iniciando: config, banco, JWT e dependências opcionais.", Severidade: string(SeveridadeWarn)},
		{Codigo: NamespaceReservado + ".migrations.up", Descricao: "Falha aplicando migrations automáticas no boot — processo não sobe.", Severidade: string(SeveridadeCritical)},
		{Codigo: NamespaceReservado + ".degradacao_dependencia", Descricao: "Dependência opcional (Redis/ClickHouse) indisponível no boot — processo sobe degradado.", Severidade: string(SeveridadeWarn)},
		{Codigo: NamespaceReservado + ".shutdown", Descricao: "Encerramento graceful: dreno de requisições e fechamento LIFO.", Severidade: string(SeveridadeWarn)},
	}
}
