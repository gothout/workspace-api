// Contratos com o mundo externo (regra 5 de agents/01): a aplicação catálogo
// NÃO importa nenhum subdomínio de domain — as permissões saem do Catalogo()
// de cada um, agregadas no cmd/bootstrap e entregues por AQUI.
//
// Os tipos PermissaoMeta/RotaMeta são a FORMA ÚNICA do registro agregado: os
// tipos homônimos de cada subdomínio (declarados nos permissions.go deles)
// são convertidos pelo bootstrap na montagem. Subdomínio novo entra no
// agregador do bootstrap — este pacote não muda.
package catalogo

// Dependencias carrega o que a aplicação precisa, ligado UMA vez no boot
// pelo cmd/bootstrap.
type Dependencias struct {
	Permissoes ProvedorPermissoes
}

// ProvedorPermissoes entrega o catálogo AGREGADO das permissões de todos os
// subdomínios. O bootstrap garante que TODOS eles já foram importados antes
// de montar esta aplicação; subdomínio fora do agregador simplesmente não
// aparece nas duas rotas.
type ProvedorPermissoes interface {
	// Catalogo devolve todas as permissões da plataforma com metadados.
	Catalogo() []PermissaoMeta
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
