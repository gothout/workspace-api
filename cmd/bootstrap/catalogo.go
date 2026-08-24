// Adaptador da aplicação catalogo: AGREGA os Catalogo() de permissões e os
// CatalogoEventos() dos subdomínios num registro único (doc 03) e entrega
// pelas interfaces do contratos.go dela.
//
// O bootstrap garante a ORDEM: os imports dos três subdomínios (e da
// aplicação auth) já estão neste pacote antes desta montagem — subdomínio
// novo entra AQUI, e aparece nas rotas sem tocar na aplicação.
package bootstrap

import (
	organizacaomodel "workspace-api/internal/identidade/model/organization"
	usermodel "workspace-api/internal/identidade/model/user"
	workspacemodel "workspace-api/internal/identidade/model/workspace"

	aplicacaoauth "workspace-api/internal/identidade/application/auth"
	aplicacaocatalogo "workspace-api/internal/identidade/application/catalogo"
	aplicacaologs "workspace-api/internal/identidade/application/logs"
	aplicacaoprovisionamento "workspace-api/internal/identidade/application/provisionamento"
	dominioOrganizacao "workspace-api/internal/identidade/domain/organization"
	dominioUsuario "workspace-api/internal/identidade/domain/user"
	dominioWorkspace "workspace-api/internal/identidade/domain/workspace"

	"workspace-api/internal/pkg/errobserve"
)

// agregadorPermissoes é o ProvedorPermissoes da aplicação catalogo.
type agregadorPermissoes struct {
	itens []aplicacaocatalogo.PermissaoMeta
}

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
	// Aplicação logs (E5): permissões próprias (recorte workspace ×
	// organization), mesma forma dos subdomínios.
	for _, meta := range aplicacaologs.Catalogo() {
		rotas := make([]aplicacaocatalogo.RotaMeta, 0, len(meta.Rotas))
		for _, rota := range meta.Rotas {
			rotas = append(rotas, aplicacaocatalogo.RotaMeta{Rota: rota.Rota, Metodo: rota.Metodo})
		}
		itens = append(itens, aplicacaocatalogo.PermissaoMeta{
			Permissao: meta.Permissao, Descricao: meta.Descricao,
			Rotas: rotas, GrupoMenu: meta.GrupoMenu,
		})
	}
	// Aplicação provisionamento (UX5): permissão da plataforma, mesma forma.
	for _, meta := range aplicacaoprovisionamento.Catalogo() {
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

// agregadorEventos é o ProvedorEventos da aplicação catalogo.
type agregadorEventos struct {
	itens []aplicacaocatalogo.EventoMeta
}

func (a agregadorEventos) CatalogoEventos() []aplicacaocatalogo.EventoMeta { return a.itens }

// novoAgregadorEventos converte os catálogos de eventos NATIVOS de cada
// subdomínio e da aplicação auth (tipos homônimos, um por pacote) para a
// forma única do contrato — preenchendo dominio/subdominio na conversão — e
// acrescenta o vocabulário da observação de ERROS (evolução errobserve): os
// códigos dos observadores de cada subdomínio (acao = código estável, campos
// = [severidade]) e o namespace RESERVADO sistema.plataforma. A ordem do
// agregado é irrelevante: o mapa sai ordenado da aplicação.
func novoAgregadorEventos() agregadorEventos {
	itens := make([]aplicacaocatalogo.EventoMeta, 0)
	for _, meta := range dominioOrganizacao.CatalogoEventos() {
		itens = append(itens, aplicacaocatalogo.EventoMeta{
			Dominio:    organizacaomodel.Dominio,
			Subdominio: organizacaomodel.Subdominio,
			Acao:       meta.Acao, Descricao: meta.Descricao,
			Campos: copiarCampos(meta.Campos),
		})
	}
	for _, meta := range dominioWorkspace.CatalogoEventos() {
		itens = append(itens, aplicacaocatalogo.EventoMeta{
			Dominio:    workspacemodel.Dominio,
			Subdominio: workspacemodel.Subdominio,
			Acao:       meta.Acao, Descricao: meta.Descricao,
			Campos: copiarCampos(meta.Campos),
		})
	}
	for _, meta := range dominioUsuario.CatalogoEventos() {
		itens = append(itens, aplicacaocatalogo.EventoMeta{
			Dominio:    usermodel.Dominio,
			Subdominio: usermodel.Subdominio,
			Acao:       meta.Acao, Descricao: meta.Descricao,
			Campos: copiarCampos(meta.Campos),
		})
	}
	for _, meta := range aplicacaoauth.CatalogoEventos() {
		itens = append(itens, aplicacaocatalogo.EventoMeta{
			Dominio:    aplicacaoauth.Dominio,
			Subdominio: aplicacaoauth.Subdominio,
			Acao:       meta.Acao, Descricao: meta.Descricao,
			Campos: copiarCampos(meta.Campos),
		})
	}
	// Aplicação provisionamento (UX5): o evento da ORQUESTRAÇÃO (as escritas
	// componentes já são auditadas pelos subdomínios).
	for _, meta := range aplicacaoprovisionamento.CatalogoEventos() {
		itens = append(itens, aplicacaocatalogo.EventoMeta{
			Dominio:    aplicacaoprovisionamento.Dominio,
			Subdominio: aplicacaoprovisionamento.Subdominio,
			Acao:       meta.Acao, Descricao: meta.Descricao,
			Campos: copiarCampos(meta.Campos),
		})
	}
	// Vocabulário de ERROS observados (errobserve): um evento por código de
	// erro do subdomínio, no grupo dona dele, com a severidade declarada.
	itens = append(itens, eventosDeErrosObservados()...)
	return agregadorEventos{itens: itens}
}

// eventosDeErrosObservados converte o catálogo global do errobserve + o
// vocabulário fixo da plataforma para a forma única dos eventos. O campo
// severidade viaja em Campos — é a chave extra que o evento carrega quando
// dispara.
func eventosDeErrosObservados() []aplicacaocatalogo.EventoMeta {
	itens := make([]aplicacaocatalogo.EventoMeta, 0)
	for _, meta := range errobserve.CatalogoSistema() {
		itens = append(itens, aplicacaocatalogo.EventoMeta{
			Dominio:    errobserve.NamespaceReservado,
			Subdominio: "plataforma",
			Acao:       meta.Codigo,
			Descricao:  meta.Descricao,
			Campos:     []string{"severidade"},
		})
	}
	for _, grupo := range errobserve.CatalogoGlobal() {
		for _, meta := range grupo.Erros {
			itens = append(itens, aplicacaocatalogo.EventoMeta{
				Dominio:    grupo.Dominio,
				Subdominio: grupo.Subdominio,
				Acao:       meta.Codigo,
				Descricao:  meta.Descricao,
				Campos:     []string{"severidade"},
			})
		}
	}
	return itens
}

// copiarCampos isola o agregado das fatias nativas dos subdomínios
// (mutação num lado nunca vaza para o outro).
func copiarCampos(campos []string) []string {
	if campos == nil {
		return nil
	}
	copia := make([]string, len(campos))
	copy(copia, campos)
	return copia
}
