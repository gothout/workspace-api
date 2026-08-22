// Emissão e verificação de tokens (access + refresh) com as claims do
// template: sub/org/wks/name/email/typ e jti único no refresh.
//
// O token é ASSINADO, não cifrado: tudo nele é público para quem o carrega —
// claim nova só entra com motivo (AGENTS.md do pacote). TTL vem da config,
// nunca de constante. Erros internos distinguem expirado/assinatura/formato
// para o log; a resposta ao cliente é sempre o 401 genérico (traduzida pelo
// middleware).
package jwt

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	ClaimTipoAccess  = "access"
	ClaimTipoRefresh = "refresh"
)

// Sentinelas de validação — comparadas com errors.Is; o middleware traduz as
// duas no MESMO 401 genérico (o motivo exato é informação para atacante).
var (
	ErrTokenExpirado = errors.New("jwt: token expirado")
	ErrTokenInvalido = errors.New("jwt: token inválido")
)

// Claims são as claims do template. sub/org/wks/name/email/typ conforme o
// doc 03; jti entra no refresh e é a chave da revogação persistida (F4).
type Claims struct {
	UserUUID         string `json:"sub"`
	OrganizationUUID string `json:"org,omitempty"`
	WorkspaceUUID    string `json:"wks,omitempty"` // workspace ativo, quando houver
	Nome             string `json:"name,omitempty"`
	Email            string `json:"email,omitempty"`
	Tipo             string `json:"typ"`
	JTI              string `json:"jti,omitempty"` // único por refresh
	jwt.RegisteredClaims
}

// EntradaToken carrega o que vai dentro do token; nada além das claims do
// contrato (claim nova exige revisão do AGENTS.md).
type EntradaToken struct {
	UserUUID         uuid.UUID
	OrganizationUUID uuid.UUID
	WorkspaceUUID    uuid.UUID // uuid.Nil = sem workspace ativo
	Nome             string
	Email            string
}

func (e EntradaToken) validar() error {
	if e.UserUUID == uuid.Nil {
		return fmt.Errorf("jwt: %w", ErrTokenInvalido)
	}
	return nil
}

// EmitirAcesso assina um access token curto (security.jwt_ttl_min).
func (m *Manager) EmitirAcesso(in EntradaToken) (string, error) {
	if err := in.validar(); err != nil {
		return "", err
	}
	agora := time.Now().UTC()
	claims := &Claims{
		UserUUID:         in.UserUUID.String(),
		OrganizationUUID: in.OrganizationUUID.String(),
		Tipo:             ClaimTipoAccess,
		Nome:             in.Nome,
		Email:            in.Email,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(agora),
			ExpiresAt: jwt.NewNumericDate(agora.Add(m.ttlAccess)),
			ID:        uuid.NewString(),
		},
	}
	if in.WorkspaceUUID != uuid.Nil {
		claims.WorkspaceUUID = in.WorkspaceUUID.String()
	}
	return m.assinar(claims)
}

// EmitirRefresh assina um refresh token longo (security.jwt_refresh_ttl_hours)
// e devolve token, jti e expiração — o jti é gravado no Postgres na F4
// (identidade_user_refresh_token) e é a chave do logout/revogação.
func (m *Manager) EmitirRefresh(in EntradaToken) (string, string, time.Time, error) {
	if err := in.validar(); err != nil {
		return "", "", time.Time{}, err
	}
	agora := time.Now().UTC()
	expira := agora.Add(m.ttlRefresh)
	jti := uuid.NewString()
	claims := &Claims{
		UserUUID:         in.UserUUID.String(),
		OrganizationUUID: in.OrganizationUUID.String(),
		WorkspaceUUID:    workspaceOpcional(in.WorkspaceUUID),
		Tipo:             ClaimTipoRefresh,
		JTI:              jti,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(agora),
			ExpiresAt: jwt.NewNumericDate(expira),
			ID:        jti,
		},
	}
	token, err := m.assinar(claims)
	if err != nil {
		return "", "", time.Time{}, err
	}
	return token, jti, expira, nil
}

// Validar verifica assinatura, método, expiração e revogação (quando há
// verificador ligado). Refresh revogado = ErrTokenInvalido.
func (m *Manager) Validar(tokenStr string) (*Claims, error) {
	parsed, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("%w: método inesperado", ErrTokenInvalido)
		}
		return m.segredo, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpirado
		}
		return nil, ErrTokenInvalido
	}
	claims, ok := parsed.Claims.(*Claims)
	if !ok || !parsed.Valid {
		return nil, ErrTokenInvalido
	}
	if claims.Tipo != ClaimTipoAccess && claims.Tipo != ClaimTipoRefresh {
		return nil, ErrTokenInvalido
	}
	if claims.Tipo == ClaimTipoRefresh && claims.JTI == "" {
		return nil, ErrTokenInvalido
	}
	if m.revogador != nil && claims.Tipo == ClaimTipoRefresh {
		revogado, err := m.revogador.Revogado(claims.JTI)
		if err != nil {
			return nil, fmt.Errorf("jwt: falha ao conferir revogação: %w", err)
		}
		if revogado {
			return nil, ErrTokenInvalido
		}
	}
	return claims, nil
}

func (m *Manager) assinar(claims *Claims) (string, error) {
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.segredo)
}

func workspaceOpcional(w uuid.UUID) string {
	if w == uuid.Nil {
		return ""
	}
	return w.String()
}
