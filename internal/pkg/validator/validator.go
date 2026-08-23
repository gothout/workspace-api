// Package validator registra as tags de validação customizadas do binding do
// gin UMA única vez no boot (logo após config.Init). Formato vive aqui; regra
// de negócio (unicidade, reservados) fica no service do subdomínio.
package validator

import (
	"fmt"
	"regexp"
	"sync"

	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
)

// slugDNSRegex é o formato canônico do slug (label DNS): minúsculas,
// dígitos e hífen interno, 3 a 63 caracteres. Fonte ÚNICA da regra de
// formato (R7): o pkg é folha — alcançável pelo binding E pelo VO do
// subdomínio dono (model/workspace delega nele); regra de NEGÓCIO
// (unicidade, reservados) segue no service.
var slugDNSRegex = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{1,61}[a-z0-9])$`)

var (
	once       sync.Once
	registrado bool
)

// Registrar instala as tags customizadas no engine de validação do gin.
// Idempotente: chamar duas vezes não duplica registro. Erro aqui é fatal no
// boot — binding com tag ausente falharia de forma confusa em runtime.
func Registrar() error {
	var errRegistro error
	once.Do(func() {
		engine, ok := binding.Validator.Engine().(*validator.Validate)
		if !ok {
			errRegistro = fmt.Errorf("validator: engine de validação do gin não é *validator.Validate")
			return
		}
		if err := engine.RegisterValidation("slugdns", validarSlugDNS); err != nil {
			errRegistro = fmt.Errorf("validator: falha ao registrar tag slugdns: %w", err)
			return
		}
		registrado = true
	})
	return errRegistro
}

// Registrado informa se o registro já rodou (uso diagnóstico do bootstrap).
func Registrado() bool {
	return registrado
}

// validarSlugDNS executa a regex canônica de slug sobre o campo anotado.
func validarSlugDNS(fl validator.FieldLevel) bool {
	return slugDNSRegex.MatchString(fl.Field().String())
}

// SlugValido expõe a mesma regra para uso fora do binding (testes, seeds).
func SlugValido(slug string) bool {
	return slugDNSRegex.MatchString(slug)
}
