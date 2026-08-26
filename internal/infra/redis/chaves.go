// Prefixos de chave FIXOS do template (issue #8 / agents/02). Toda chave
// Redis nasce por um destes construtores — grep pelo prefixo acha o dono, o
// TTL e a invalidação. Prefixo novo só entra com motivo documentado aqui e
// no AGENTS.md do pacote.
package redis

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const (
	// PrefixoPermissoes — perm:{org}:{user}:{wks}: permissões efetivas do
	// par usuário×workspace (unidade de papéis). Invalidado na escrita de
	// atribuição (contrato ObservadorAtribuicoes do user) e limitado por TTL.
	PrefixoPermissoes = "perm:"

	// PrefixoWorkspaceSlug — workspace:slug:{slug}: resultado da resolução
	// {slug} → workspace consumida pelo Host em toda requisição. Contrato
	// CacheResolucao do subdomínio workspace; invalidação ativa nas escritas.
	PrefixoWorkspaceSlug = "workspace:slug:"

	// PrefixoLock — lock:c:{hash} e lock:b:{hash}: contador da janela de
	// falhas e marcador de bloqueio do lockout de login por (e-mail, IP).
	// O par é hasheado antes de virar chave (PII não repousa crua).
	PrefixoLock = "lock:"

	// PrefixoIdempotencia — idempot:{chave}: RESERVADO para idempotência de
	// escrita futura; nenhum código grava aqui hoje (documentado para o
	// desenho de chaves não colidir quando chegar).
	PrefixoIdempotencia = "idempot:"

	// PrefixoDenylistJWT — jwt:deny:{jti}: cache da revogação persistida do
	// refresh token (a verdade fica em identidade_user_refresh_token). Só
	// resultado POSITIVO é cacheado — revogação é permanente.
	PrefixoDenylistJWT = "jwt:deny:"

	// PrefixoAplicacoes — app:{org}:{ws}: módulos liberados do par
	// (organization, workspace) — licença ∩ ativação ∩ módulo ativo.
	// Consumido pelo RequireAplicacao do middleware em toda requisição de
	// módulo; invalidação ativa nas escritas de licença/ativação/módulo.
	PrefixoAplicacoes = "app:"
)

// ChavePermissoes monta perm:{org}:{user}:{wks}.
func ChavePermissoes(organizationUUID, usuarioUUID, workspaceUUID string) string {
	return PrefixoPermissoes + organizationUUID + ":" + usuarioUUID + ":" + workspaceUUID
}

// ChavePermissoesUsuario é o padrão de TODAS as entradas do usuário numa
// organization — usado pela invalidação grosseira (SCAN + DEL).
func ChavePermissoesUsuario(organizationUUID, usuarioUUID string) string {
	return PrefixoPermissoes + organizationUUID + ":" + usuarioUUID + ":*"
}

// ChaveResolucaoSlug monta workspace:slug:{slug}.
func ChaveResolucaoSlug(slug string) string {
	return PrefixoWorkspaceSlug + slug
}

// ChaveLockContador monta lock:c:{hash(e-mail|IP)} — contador da janela. O
// par é NORMALIZADO AQUI (caixa/espaços): mesma origem vira a MESMA chave,
// venha de quem vier.
func ChaveLockContador(email, ip string) string {
	return PrefixoLock + "c:" + hashPar(normalizarPar(email, ip))
}

// ChaveLockBloqueio monta lock:b:{hash(e-mail|IP)} — marcador do bloqueio.
func ChaveLockBloqueio(email, ip string) string {
	return PrefixoLock + "b:" + hashPar(normalizarPar(email, ip))
}

// ChaveDenylistJWT monta jwt:deny:{jti}.
func ChaveDenylistJWT(jti string) string {
	return PrefixoDenylistJWT + jti
}

// ChaveAplicacoes monta app:{org}:{ws} — o par resolve os módulos liberados.
func ChaveAplicacoes(organizationUUID, workspaceUUID string) string {
	return PrefixoAplicacoes + organizationUUID + ":" + workspaceUUID
}

// PadraoAplicacoesOrganization é o padrão de TODAS as entradas de uma
// organization — usado pela invalidação grosseira (SCAN + DEL) nas escritas
// de licença.
func PadraoAplicacoesOrganization(organizationUUID string) string {
	return PrefixoAplicacoes + organizationUUID + ":*"
}

// hashPar resume o par JÁ normalizado para chave: PII não repousa cru no
// Redis e o tamanho da chave fica limitado.
func hashPar(emailNormalizado, ipNormalizado string) string {
	resumo := sha256.Sum256([]byte(emailNormalizado + "|" + ipNormalizado))
	return hex.EncodeToString(resumo[:])
}

// normalizarPar padroniza e-mail/IP antes de hashear: caixa e espaços não
// separam contadores do mesmo par.
func normalizarPar(email, ip string) (string, string) {
	return strings.ToLower(strings.TrimSpace(email)), strings.TrimSpace(ip)
}
