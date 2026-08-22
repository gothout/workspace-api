// Adaptador da aplicação catalogo: AGREGA os Catalogo() dos subdomínios num
// registro único (doc 03) e entrega pela interface do contratos.go dela.
//
// O bootstrap garante a ORDEM: os imports dos três subdomínios já estão
// neste pacote (bootstrap.go) antes desta montagem — subdomínio novo entra
// AQUI, e aparece nas duas rotas sem tocar na aplicação.
package bootstrap

import (
	dominioOrganizacao "workspace-api/internal/identidade/domain/organization"
	dominioUsuario "workspace-api/internal/identidade/domain/user"
	dominioWorkspace "workspace-api/internal/identidade/domain/workspace"
	aplicacaocatalogo "workspace-api/internal/identidade/application/catalogo"
)

// agregadorPermissoes é o ProvedorPermissoes da aplicação catalogo.
type agregadorPermissoes struct{ itens []aplicacaocatalogo.PermissaoMeta }

func (a agregadorPermissoes) Catalogo() []aplicacaocatalogo.PermissaoMeta { return a.itens }

// novoAgregadorPermissoes converte os catálogos NATIVOS de cada subdomínio
// (tipos homônimos, um por pacote) para a forma única do contrato. Ordem do
// agregado é irrelevante: a árvore sai ordenada da aplicação.
func novoAgregadorPermissoes() agregadorPermissoes {
	itens := make([]aplicacaocatalogo.PermissaoMeta, 0)
	for _, meta := range dominioOrganizacao.Catalogo() {
		rotas := make([]aplicacaocatalogo.RotaMeta, 0, len(meta.Rotas))
		for _, rota := range meta.Rotas {
			rotas = append(rotas, aplicacaocatalogo.RotaMeta{Rota: rota.Rota, Metodo: rota.Metodo})
		}
		itens = append(itens, aplicacaocatalogo.PermissaoMeta{
			Permissao: meta.Permissao, Descricao: meta.Descricao,
			Rotas: rotas, GrupoMenu: meta.GrupoMenu,
		})
	}
	for _, meta := range dominioWorkspace.Catalogo() {
		rotas := make([]aplicacaocatalogo.RotaMeta, 0, len(meta.Rotas))
		for _, rota := range meta.Rotas {
			rotas = append(rotas, aplicacaocatalogo.RotaMeta{Rota: rota.Rota, Metodo: rota.Metodo})
		}
		itens = append(itens, aplicacaocatalogo.PermissaoMeta{
			Permissao: meta.Permissao, Descricao: meta.Descricao,
			Rotas: rotas, GrupoMenu: meta.GrupoMenu,
		})
	}
	for _, meta := range dominioUsuario.Catalogo() {
		rotas := make([]aplicacaocatalogo.RotaMeta, 0, len(meta.Rotas))
		for _, rota := range meta.Rotas {
			rotas = append(rotas, aplicacaocatalogo.RotaMeta{Rota: rota.Rota, Metodo: rota.Metodo})
		}
		itens = append(itens, aplicacaocatalogo.PermissaoMeta{
			Permissao: meta.Permissao, Descricao: meta.Descricao,
			Rotas: rotas, GrupoMenu: meta.GrupoMenu,
		})
	}
	return agregadorPermissoes{itens: itens}
}
