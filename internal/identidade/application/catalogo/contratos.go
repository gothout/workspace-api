// Contratos com o mundo externo (regra 5 de agents/01): a aplicação catálogo
// NÃO importa nenhum subdomínio de domain — as permissões saem do Catalogo()
// de cada um e os eventos de auditoria do CatalogoEventos() de cada um,
// agregados no cmd/bootstrap e entregues por AQUI.
//
// Os tipos PermissaoMeta/RotaMeta/EventoMeta são a FORMA ÚNICA dos registros
// agregados: os tipos homônimos de cada subdomínio (declarados nos
// permissions.go/events.go deles) são convertidos pelo bootstrap na montagem.
// Subdomínio novo entra no agregador do bootstrap — este pacote não muda.
package catalogo

// Dependencias carrega o que a aplicação precisa, ligado UMA vez no boot
// pelo cmd/bootstrap.
type Dependencias struct {
	Permissoes ProvedorPermissoes
	Eventos    ProvedorEventos
}

// ProvedorPermissoes entrega o catálogo AGREGADO das permissões de todos os
// subdomínios. O bootstrap garante que TODOS eles já foram importados antes
// de montar esta aplicação; subdomínio fora do agregador simplesmente não
// aparece nas duas rotas.
type ProvedorPermissoes interface {
	// Catalogo devolve todas as permissões da plataforma com metadados.
	Catalogo() []PermissaoMeta
}

// ProvedorEventos entrega o catálogo AGREGADO dos eventos de auditoria de
// todos os subdomínios (e aplicações que auditam) — mesma disciplina do
// agregador de permissões, alimentando GET /api/system/eventos.
type ProvedorEventos interface {
	// CatalogoEventos devolve todos os eventos da plataforma com metadados.
	CatalogoEventos() []EventoMeta
}

// PermissaoMeta — metadados de UMA permissão granular no formato único do
// registro agregado (mesma forma dos permissions.go dos subdomínios).
type PermissaoMeta struct {
	Permissao string     // valor exato exigido pela rota (dominio:subdominio:acao)
	Descricao string     // PT-BR: o que a permissão libera
	Rotas     []RotaMeta // pares rota+método que exigem esta permissão
	GrupoMenu string     // agrupamento sugerido para o menu do front-end
}

// RotaMeta — UM par rota+método; a árvore do endpoint emite uma ação por par.
type RotaMeta struct {
	Rota   string // path com {uuid} onde couber
	Metodo string // método HTTP
}

// EventoMeta — metadados de UM evento de auditoria no formato único do
// registro agregado (mesma forma dos events.go dos subdomínios). O bootstrap
// preenche Dominio/Subdominio na conversão dos tipos nativos.
type EventoMeta struct {
	Dominio    string   // dona do vocabulário (ex.: "identidade")
	Subdominio string   // emissor do evento (ex.: "workspace", "auth")
	Acao      string   // valor estável emitido no campo acao
	Descricao string   // PT-BR: o que o evento significa
	Campos    []string // chaves extras do payload (além das de identidade); vazias = só identidade
}
