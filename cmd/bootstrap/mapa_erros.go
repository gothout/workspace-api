// Frente do MAPA GLOBAL DE ERROS para a CLI (`workspace-api errors`): junta
// o registro global do rest_err (código + mensagem + status, alimentado pelo
// init() de cada subdomínio) com as severidades declaradas na evolução
// errobserve e o vocabulário reservado da plataforma. Dado puro de registro —
// roda SEM conexão e sem configs.json.
package bootstrap

import (
	"workspace-api/internal/pkg/errobserve"
	"workspace-api/internal/pkg/rest_err"
)

// LinhaMapaErros é uma entrada do mapa global impresso pela CLI.
type LinhaMapaErros struct {
	Dominio    string
	Subdominio string
	Codigo     string
	Severidade string // warn | error | critical; plataforma carrega a dela
	Mensagem   string
	Status     int // 0 nos eventos de plataforma (mapping, não resposta HTTP)
}

// MapaErrosObservados devolve o mapa completo em ordem determinística:
// vocabulário de plataforma primeiro, depois os grupos do rest_err
// (domínio/subdomínio/código ordenados — garantia do rest_err.MapaErros).
func MapaErrosObservados() []LinhaMapaErros {
	severidadePorCodigo := map[string]string{}
	for _, grupo := range errobserve.CatalogoGlobal() {
		for _, meta := range grupo.Erros {
			severidadePorCodigo[meta.Codigo] = meta.Severidade
		}
	}

	linhas := make([]LinhaMapaErros, 0)
	for _, meta := range errobserve.CatalogoSistema() {
		linhas = append(linhas, LinhaMapaErros{
			Dominio:    errobserve.NamespaceReservado,
			Subdominio: "plataforma",
			Codigo:     meta.Codigo,
			Severidade: meta.Severidade,
			Mensagem:   meta.Descricao,
		})
	}
	for _, grupo := range rest_err.MapaErros() {
		for _, entrada := range grupo.Erros {
			severidade := severidadePorCodigo[entrada.Code]
			if severidade == "" {
				// Catálogo sem observador não deve existir (o DoCatalogo
				// cobre todo errorCatalog) — impresso honesto como "-".
				severidade = "-"
			}
			linhas = append(linhas, LinhaMapaErros{
				Dominio:    grupo.Dominio,
				Subdominio: grupo.Subdominio,
				Codigo:     entrada.Code,
				Severidade: severidade,
				Mensagem:   entrada.Message,
				Status:     entrada.Status,
			})
		}
	}
	return linhas
}
