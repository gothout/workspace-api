package licenca

import (
	"time"

	"github.com/google/uuid"

	modellicenca "workspace-api/internal/licensing/model/licenca"
)

// LicencaResponseDto — saída única do subdomínio; já carrega o resumo do
// módulo (join de leitura) para o painel não fazer N chamadas.
type LicencaResponseDto struct {
	UUID             uuid.UUID `json:"uuid"`
	OrganizationUUID uuid.UUID `json:"organization_uuid"`
	ModuloUUID       uuid.UUID `json:"modulo_uuid"`
	ModuloSlug       string    `json:"modulo_slug"`
	ModuloNome       string    `json:"modulo_nome"`
	ConcedidaPor     string    `json:"concedida_por"`
	CreatedAt        time.Time `json:"created_at"`
}

func NovoLicencaResponseDto(l *modellicenca.LicencaComModulo) LicencaResponseDto {
	concedidaPor := ""
	if l.ConcedidaPor != nil {
		concedidaPor = l.ConcedidaPor.String()
	}
	return LicencaResponseDto{
		UUID:             l.UUID,
		OrganizationUUID: l.OrganizationUUID,
		ModuloUUID:       l.ModuloUUID,
		ModuloSlug:       l.ModuloSlug,
		ModuloNome:       l.ModuloNome,
		ConcedidaPor:     concedidaPor,
		CreatedAt:        l.CreatedAt,
	}
}

func NovoLicencaListaResponseDto(items []modellicenca.LicencaComModulo) []LicencaResponseDto {
	dtos := make([]LicencaResponseDto, 0, len(items))
	for i := range items {
		dtos = append(dtos, NovoLicencaResponseDto(&items[i]))
	}
	return dtos
}
