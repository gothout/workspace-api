package ativacao

// AtivarModuloRequestDto — entrada de POST .../workspaces/{ws}/modulos. O
// workspace alvo vem do PATH; quem ativou sai do ctx — nunca do corpo.
type AtivarModuloRequestDto struct {
	ModuloSlug string `json:"modulo_slug" binding:"required,slugdns"`
}
