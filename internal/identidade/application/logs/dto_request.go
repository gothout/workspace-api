package logs

import (
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"workspace-api/internal/infra/clickhouse"
)

// classeDeStatus converte "4xx" (também aceita "4") no dígito da classe;
// fora do conjunto 1–5 devolve 0 = classe inválida.
func classeDeStatus(valor string) int {
	bruto := strings.TrimSuffix(strings.TrimSpace(strings.ToLower(valor)), "xx")
	if len(bruto) != 1 || bruto[0] < '1' || bruto[0] > '5' {
		return 0
	}
	return int(bruto[0] - '0')
}

// LogsFiltroRequestDto — query params das três rotas de leitura. Tudo opcional;
// o ESCOPO nunca vem daqui: organization/workspace efetivos são impostos pelo
// service a partir do ctx (fail-closed), os informados são validados contra ele.
// Metodo e StatusClasse (UX3) só têm efeito na trilha de acesso.
type LogsFiltroRequestDto struct {
	OrganizationUUID string `form:"organization_uuid" binding:"omitempty,uuid"`
	WorkspaceUUID    string `form:"workspace_uuid" binding:"omitempty,uuid"`
	UserUUID         string `form:"user_uuid" binding:"omitempty,uuid"`
	Acao             string `form:"acao" binding:"omitempty,max=120"`
	RayTrace         string `form:"ray_trace" binding:"omitempty,max=64"`
	Metodo           string `form:"metodo" binding:"omitempty,max=10"` // ex.: GET
	StatusClasse     string `form:"status_classe" binding:"omitempty"` // 2xx | 3xx | 4xx | 5xx
	Inicio           string `form:"inicio" binding:"omitempty"`        // RFC3339 (ISO 8601 UTC)
	Fim              string `form:"fim" binding:"omitempty"`           // RFC3339 (ISO 8601 UTC)
}

// ParaFiltro valida formato e converte para o filtro do consultor. UUID ou
// timestamp malformado = ErrFiltroInvalido (400 — doc 04: inválido é 400,
// nunca 500 nem ignorado silenciosamente). A validação mora AQUI e não só no
// binding: service/testes chamam este método sem gin.
func (d LogsFiltroRequestDto) ParaFiltro() (clickhouse.FiltroTrilha, error) {
	filtro := clickhouse.FiltroTrilha{
		Acao:     d.Acao,
		RayTrace: d.RayTrace,
	}
	if d.Metodo != "" {
		metodo := strings.ToUpper(strings.TrimSpace(d.Metodo))
		if !regexp.MustCompile(`^[A-Z]{1,10}$`).MatchString(metodo) {
			return clickhouse.FiltroTrilha{}, ErrFiltroInvalido
		}
		filtro.Metodo = metodo
	}
	if d.StatusClasse != "" {
		classe := classeDeStatus(d.StatusClasse)
		if classe == 0 {
			return clickhouse.FiltroTrilha{}, ErrFiltroInvalido
		}
		filtro.ClasseStatus = classe
	}
	for campo, valor := range map[string]string{
		"organization_uuid": d.OrganizationUUID,
		"workspace_uuid":    d.WorkspaceUUID,
		"user_uuid":         d.UserUUID,
	} {
		if valor == "" {
			continue
		}
		id, err := uuid.Parse(valor)
		if err != nil {
			return clickhouse.FiltroTrilha{}, ErrFiltroInvalido
		}
		switch campo {
		case "organization_uuid":
			filtro.OrganizationUUID = id.String()
		case "workspace_uuid":
			filtro.WorkspaceUUID = id.String()
		case "user_uuid":
			filtro.UserUUID = id.String()
		}
	}
	if d.Inicio != "" {
		inicio, err := time.Parse(time.RFC3339, d.Inicio)
		if err != nil {
			return clickhouse.FiltroTrilha{}, ErrFiltroInvalido
		}
		filtro.InstanteInicio = inicio
	}
	if d.Fim != "" {
		fim, err := time.Parse(time.RFC3339, d.Fim)
		if err != nil {
			return clickhouse.FiltroTrilha{}, ErrFiltroInvalido
		}
		filtro.InstanteFim = fim
	}
	if !filtro.InstanteInicio.IsZero() && !filtro.InstanteFim.IsZero() &&
		filtro.InstanteInicio.After(filtro.InstanteFim) {
		return clickhouse.FiltroTrilha{}, ErrFiltroInvalido
	}
	return filtro, nil
}
