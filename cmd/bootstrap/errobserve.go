// Frente do observador de erros da PLATAFORMA (evolução errobserve): o
// namespace reservado "sistema.*" é de uso exclusivo do bootstrap — eventos
// de migrations, boot, shutdown e degradação de dependência. Subdomínio de
// negócio NUNCA registra nele (o For/Novo do pacote reprova).
package bootstrap

import (
	"workspace-api/internal/pkg/errobserve"
)

// observadorPlataformaInstancia observa os erros de plataforma com as
// sentinelas próprias do namespace reservado. O vocabulário FIXO (incluindo
// boot e shutdown, hoje só mapping) mora no errobserve.CatalogoSistema — é
// o que aparece em GET /api/system/eventos sem depender de emissão.
var observadorPlataformaInstancia = errobserve.ObservadorPlataforma([]errobserve.Entrada{
	{
		Erro:       errobserve.ErrDegradacao,
		Codigo:     "sistema.degradacao_dependencia",
		Mensagem:   "Dependência opcional indisponível — processo sobe degradado.",
		Severidade: errobserve.SeveridadeWarn,
	},
	{
		Erro:       errobserve.ErrMigracao,
		Codigo:     "sistema.migrations.up",
		Mensagem:   "Falha aplicando migrations no boot — processo não sobe.",
		Severidade: errobserve.SeveridadeCritical,
	},
})

// observadorPlataforma devolve o observador do namespace reservado.
func observadorPlataforma() *errobserve.Observador {
	return observadorPlataformaInstancia
}
