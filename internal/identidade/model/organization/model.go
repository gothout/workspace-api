// Package organization implementa o MODELO do subdomínio organization do
// domínio identidade: entidade raiz da hierarquia (dona do contrato), a chave
// de API dela e os VOs/invariantes dos dois.
//
// FOLHA do domínio: não importa domain, application, infra nem middleware —
// qualquer camada pode importá-lo (regra 9 do doc 01).
package organization

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/net/publicsuffix"
	"gorm.io/gorm"

	"workspace-api/internal/pkg/pagination"
)

const (
	// Dominio e Subdominio identificam este modelo nos catálogos (erros,
	// permissões) e logs.
	Dominio    = "identidade"
	Subdominio = "organization"
)

// Sentinelas de invariante do MODELO — o pacote é folha e não importa o
// errors.go do subdomínio; o catálogo de lá (code estável + status) registra
// estas sentinelas.
var (
	ErrNomeInvalido         = errors.New("nome fora do formato esperado")
	ErrDominioInvalido      = errors.New("domínio fora do formato DNS ou proibido")
	ErrDominioNaoDefinido   = errors.New("organization não tem domínio custom definido")
	ErrJaInativo            = errors.New("registro já está inativo")
	ErrJaAtivo              = errors.New("registro já está ativo")
	ErrApiKeyInvalida       = errors.New("dados da chave de API inválidos")
	ErrPermissaoInvalida    = errors.New("permissão fora do formato dominio:subdominio:acao")
	ErrEscopoApiKeyInvalido = errors.New("escopo da chave de API inválido: informe a organization inteira ou ao menos um workspace")
)

// Status — ciclo de vida compartilhado por organization e apikey.
type Status string

const (
	StatusAtivo   Status = "ativo"
	StatusInativo Status = "inativo"
)

// Valido confere se o valor está no conjunto fechado — todo tipo nomeado tem um.
func (s Status) Valido() bool {
	switch s {
	case StatusAtivo, StatusInativo:
		return true
	}
	return false
}

// --- VO DominioCustom (white-label) --------------------------------------------

// dominioLabelRegex é o formato de cada rótulo DNS (LDH, minúsculas).
var dominioLabelRegex = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// permissaoRegex aceita `*:*` (dois segmentos) e `dominio:subdominio:acao`,
// com curinga por segmento — mesma gramática das permissões do seed.
var permissaoRegex = regexp.MustCompile(`^(\*|[a-z0-9_-]+):(\*|[a-z0-9_-]+):(\*|[a-z0-9_-]+)$|^\*:\*$`)

// DominioCustom é um VALUE OBJECT: imutável, definido pelo valor, válido
// desde o nascimento. É o domínio white-label registrado pela organization
// (`*.{dominio}` resolve os workspaces dela — doc 03). O tipo NÃO se chama
// `Dominio` porque a constante de catálogo `Dominio` (= "identidade", padrão
// de todos os subdomínios) já ocupa o identificador neste pacote; o campo da
// entidade e o parser mantêm o nome canônico do negócio.
type DominioCustom string

// ParseDominio valida e devolve o VO. Recusa: formato DNS inválido, valor
// IGUAL ao base_domain da plataforma, ASCENDENTE ou DESCENDENTE dele, rótulo
// único e public suffix — exige eTLD+1 no mínimo (a checagem usa a PSL real
// de golang.org/x/net/publicsuffix). Entrada é normalizada (lowercase, sem
// ponto final).
func ParseDominio(valor, baseDomainBruto string) (DominioCustom, error) {
	d := normalizar(valor)
	if d == "" || len(d) > 253 {
		return "", ErrDominioInvalido
	}
	base := normalizar(baseDomainBruto)
	if base != "" && (d == base || strings.HasSuffix(d, "."+base) || strings.HasSuffix(base, "."+d)) {
		return "", ErrDominioInvalido
	}
	rotulos := strings.Split(d, ".")
	if len(rotulos) < 2 {
		return "", ErrDominioInvalido // rótulo único é TLD/public suffix
	}
	for _, rotulo := range rotulos {
		if !dominioLabelRegex.MatchString(rotulo) {
			return "", ErrDominioInvalido
		}
	}
	// eTLD+1 no mínimo: se o valor INTEIRO é public suffix (co.uk, com.br…),
	// ninguém pode registrá-lo como domínio custom.
	if _, err := publicsuffix.EffectiveTLDPlusOne(d); err != nil {
		return "", ErrDominioInvalido
	}
	return DominioCustom(d), nil
}

func normalizar(d string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(d), "."))
}

func (d DominioCustom) String() string { return string(d) }

// Value/Scan mapeiam "" ↔ NULL: dominio é OPCIONAL na tabela.
func (d DominioCustom) Value() (driver.Value, error) {
	if d == "" {
		return nil, nil
	}
	return string(d), nil
}

func (d *DominioCustom) Scan(valor any) error {
	switch t := valor.(type) {
	case nil:
		*d = ""
	case string:
		*d = DominioCustom(t)
	case []byte:
		*d = DominioCustom(t)
	default:
		return fmt.Errorf("organization: tipo inesperado para dominio: %T", valor)
	}
	return nil
}

// --- Entidade Organization ----------------------------------------------------

// Organization é a ENTIDADE raiz do agregado do subdomínio: tem identidade
// (uuid) e ciclo de vida, e é ela quem define o escopo de todo o resto da
// hierarquia. Campos exportados são concessão ao GORM — MUTAÇÃO DIRETA fora
// dos métodos de comportamento é proibida.
type Organization struct {
	UUID      uuid.UUID      `gorm:"column:uuid;type:uuid;primaryKey" json:"uuid"`
	Nome      string         `gorm:"column:nome;not null" json:"nome"`
	Documento string         `gorm:"column:documento" json:"documento"`
	Dominio   DominioCustom  `gorm:"column:dominio;type:text" json:"dominio,omitempty"`
	Status    Status         `gorm:"column:status;not null;default:'ativo'" json:"status"`
	CreatedAt time.Time      `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at;index" json:"-"`
}

func (Organization) TableName() string { return "identidade_organization_organization" }

// NewOrganization é o CONSTRUTOR do agregado: valida as invariantes antes de
// devolver a entidade. Controller e service nunca montam entidade campo a campo.
func NewOrganization(in CreateInput) (*Organization, error) {
	nome := strings.TrimSpace(in.Nome)
	if len(nome) < 2 || len(nome) > 120 {
		return nil, ErrNomeInvalido
	}
	documento := strings.TrimSpace(in.Documento)
	if len(documento) > 64 {
		return nil, ErrNomeInvalido
	}
	return &Organization{
		UUID:      uuid.New(),
		Nome:      nome,
		Documento: documento,
		Status:    StatusAtivo,
	}, nil
}

// Renomear — comportamento com invariante de tamanho.
func (o *Organization) Renomear(nome string) error {
	nome = strings.TrimSpace(nome)
	if len(nome) < 2 || len(nome) > 120 {
		return ErrNomeInvalido
	}
	o.Nome = nome
	return nil
}

// Inativar — transição de estado com invariante: organization inativa não
// inativa de novo. O SUSPENSÃO dos workspaces filhos é orquestrada pelo
// service do subdomínio (contratos.go), nunca aqui — filho nunca fica mais
// vivo que o pai.
func (o *Organization) Inativar() error {
	if o.Status == StatusInativo {
		return ErrJaInativo
	}
	o.Status = StatusInativo
	return nil
}

// Reativar devolve a organization ao ar — ação de negócio própria
// (POST .../acoes/reativar), nunca PATCH de campo.
func (o *Organization) Reativar() error {
	if o.Status == StatusAtivo {
		return ErrJaAtivo
	}
	o.Status = StatusAtivo
	return nil
}

// DefinirDominio aponta o domínio custom white-label; o VO já nasceu validado.
func (o *Organization) DefinirDominio(d DominioCustom) { o.Dominio = d }

// TemDominio informa se há domínio custom apontado.
func (o *Organization) TemDominio() bool { return o.Dominio != "" }

// RemoverDominio desliga o white-label; recusa quando nada está apontado.
func (o *Organization) RemoverDominio() error {
	if !o.TemDominio() {
		return ErrDominioNaoDefinido
	}
	o.Dominio = ""
	return nil
}

// CreateInput e UpdateInput carregam só o que a regra permite escrever. A
// organization é a RAIZ: não recebe escopo de ninguém.
type CreateInput struct {
	Nome      string
	Documento string
}

type UpdateInput struct {
	Nome   *string
	Status *Status
}

// ListFilter — filtros de listagem global (rota restrita a super_admin).
type ListFilter struct {
	Nome   string
	Status *Status
	pagination.Pagination
}

// --- Entidade ApiKey ------------------------------------------------------------

// ListaTextos é uma lista de textos persistida em coluna jsonb — evita
// dependência extra de driver de array no modelo folha.
type ListaTextos []string

func (l ListaTextos) Value() (driver.Value, error) {
	if l == nil {
		return []byte("[]"), nil
	}
	return json.Marshal(l)
}

func (l *ListaTextos) Scan(valor any) error { return scanJSON(valor, l) }

// GormDataType diz ao gorm que a coluna é jsonb.
func (ListaTextos) GormDataType() string { return "jsonb" }

// ListaUUIDs é uma lista de UUIDs persistida em coluna jsonb.
type ListaUUIDs []uuid.UUID

func (l ListaUUIDs) Value() (driver.Value, error) {
	textos := make(ListaTextos, 0, len(l))
	for _, u := range l {
		textos = append(textos, u.String())
	}
	return textos.Value()
}

func (l *ListaUUIDs) Scan(valor any) error {
	var textos ListaTextos
	if err := scanJSON(valor, &textos); err != nil {
		return err
	}
	lista := make(ListaUUIDs, 0, len(textos))
	for _, texto := range textos {
		id, err := uuid.Parse(texto)
		if err != nil {
			return fmt.Errorf("organization: uuid inválido em lista: %w", err)
		}
		lista = append(lista, id)
	}
	*l = lista
	return nil
}

func (ListaUUIDs) GormDataType() string { return "jsonb" }

func scanJSON(valor any, alvo any) error {
	switch t := valor.(type) {
	case nil:
		return nil
	case []byte:
		if len(t) == 0 {
			return nil
		}
		return json.Unmarshal(t, alvo)
	case string:
		if t == "" {
			return nil
		}
		return json.Unmarshal([]byte(t), alvo)
	default:
		return fmt.Errorf("organization: tipo inesperado para coluna jsonb: %T", valor)
	}
}

// ApiKey é o registro-filho da raiz Organization (mesmo agregado, operado
// pela raiz): credencial de integração cuja parte secreta NUNCA sai daqui —
// o banco guarda só o SHA-256 (KeyHash, `json:"-"`).
type ApiKey struct {
	UUID                 uuid.UUID      `gorm:"column:uuid;type:uuid;primaryKey" json:"uuid"`
	OrganizationUUID     uuid.UUID      `gorm:"column:organization_uuid;type:uuid;not null;index" json:"organization_uuid"`
	Nome                 string         `gorm:"column:nome;not null" json:"nome"`
	KeyHash              string         `gorm:"column:key_hash;not null" json:"-"`
	EscopoOrganization   bool           `gorm:"column:escopo_organization;not null" json:"escopo_organization"`
	WorkspacesPermitidos ListaUUIDs     `gorm:"column:workspaces_permitidos;type:jsonb;not null" json:"workspaces_permitidos"`
	Permissoes           ListaTextos    `gorm:"column:permissoes;type:jsonb;not null" json:"permissoes"`
	ExpiresAt            *time.Time     `gorm:"column:expires_at" json:"expires_at,omitempty"`
	Status               Status         `gorm:"column:status;not null;default:'ativo'" json:"status"`
	CreatedAt            time.Time      `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt            time.Time      `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
	DeletedAt            gorm.DeletedAt `gorm:"column:deleted_at;index" json:"-"`
}

func (ApiKey) TableName() string { return "identidade_organization_apikey" }

// CreateApiKeyInput carrega o que a regra permite escrever. KeyHash entra
// pronto (gerado por NovaChaveSegredo/HashDeChave) — o segredo em claro
// nunca atravessa o construtor.
type CreateApiKeyInput struct {
	OrganizationUUID     uuid.UUID
	Nome                 string
	KeyHash              string
	EscopoOrganization   bool
	WorkspacesPermitidos []uuid.UUID
	Permissoes           []string
	ExpiresAt            *time.Time
}

// NewApiKey é o CONSTRUTOR do registro-filho: valida nome, hash, gramática
// das permissões e o escopo (organization inteira OU lista de workspaces —
// chave sem vínculo nenhum é recusada).
func NewApiKey(in CreateApiKeyInput) (*ApiKey, error) {
	nome := strings.TrimSpace(in.Nome)
	if len(nome) < 3 || len(nome) > 120 {
		return nil, ErrApiKeyInvalida
	}
	if in.OrganizationUUID == uuid.Nil || strings.TrimSpace(in.KeyHash) == "" {
		return nil, ErrApiKeyInvalida
	}
	if len(in.Permissoes) == 0 {
		return nil, ErrApiKeyInvalida
	}
	permissoes := make(ListaTextos, 0, len(in.Permissoes))
	vistas := map[string]bool{}
	for _, p := range in.Permissoes {
		p = strings.TrimSpace(p)
		if !permissaoRegex.MatchString(p) {
			return nil, ErrPermissaoInvalida
		}
		if vistas[p] {
			continue
		}
		vistas[p] = true
		permissoes = append(permissoes, p)
	}
	workspaces := make(ListaUUIDs, 0, len(in.WorkspacesPermitidos))
	vistos := map[uuid.UUID]bool{}
	for _, w := range in.WorkspacesPermitidos {
		if w == uuid.Nil || vistos[w] {
			continue
		}
		vistos[w] = true
		workspaces = append(workspaces, w)
	}
	if !in.EscopoOrganization && len(workspaces) == 0 {
		return nil, ErrEscopoApiKeyInvalido
	}
	return &ApiKey{
		UUID:                 uuid.New(),
		OrganizationUUID:     in.OrganizationUUID,
		Nome:                 nome,
		KeyHash:              strings.TrimSpace(in.KeyHash),
		EscopoOrganization:   in.EscopoOrganization,
		WorkspacesPermitidos: workspaces,
		Permissoes:           permissoes,
		ExpiresAt:            in.ExpiresAt,
		Status:               StatusAtivo,
	}, nil
}

// prefixoChave marca a chave gerada — reconhecível sem revelar nada.
const prefixoChave = "wka_"

// NovaChaveSegredo gera o par (chave em claro, SHA-256 dela). A chave em
// claro é exibida UMA ÚNICA vez na criação e nunca persistida.
func NovaChaveSegredo() (chave, hash string, err error) {
	bruto := make([]byte, 32)
	if _, err = rand.Read(bruto); err != nil {
		return "", "", fmt.Errorf("organization: falha ao gerar chave de API: %w", err)
	}
	chave = prefixoChave + hex.EncodeToString(bruto)
	hash = HashDeChave(chave)
	return chave, hash, nil
}

// HashDeChave calcula o SHA-256 hex da chave em claro — é o único valor que
// o resolvedor do middleware consulta.
func HashDeChave(chave string) string {
	soma := sha256.Sum256([]byte(chave))
	return hex.EncodeToString(soma[:])
}
