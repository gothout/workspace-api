package licenca

// AtribuirLicencaRequestDto — entrada de POST .../organizations/{org}/licencas.
// A organization alvo vem do PATH (operação do super_admin que cruza
// organizations); quem concede sai do ctx — nunca do corpo.
type AtribuirLicencaRequestDto struct {
	ModuloSlug string `json:"modulo_slug" binding:"required,slugdns"`
}
