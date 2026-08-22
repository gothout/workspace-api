// Contratos com o mundo externo (ACL — regras 3 e 4 de agents/01):
//
//   - CacheResolucao é o lado do CACHE de resolução por slug (evolução Redis,
//     issue #8): entra por esta interface declarada no consumidor e ligada no
//     cmd/bootstrap. Ausência de cache (nil) é operação NORMAL — só mais
//     cara; nenhuma regra de negócio depende dele.
package workspace

import "context"

// CacheResolucao guarda o resultado da resolução {slug} → workspace — a
// consulta é global e roda em toda requisição com subdomínio no Host, então
// é a candidata natural a cache.
//
// Obrigações da implementação (agents/02/03): TTL CURTO obrigatório e
// invalidação ATIVA em inativação de organization/workspace e troca de
// domínio custom — a invariante "filho nunca mais vivo que o pai" não pode
// depender de TTL expirando sozinho.
type CacheResolucao interface {
	// Buscar devolve o workspace cacheado para o slug; ausente = false.
	Buscar(ctx context.Context, slug string) (*EntradaResolucao, bool)
	// Guardar armazena a resolução do slug (a implementação aplica o TTL).
	Guardar(ctx context.Context, slug string, entrada EntradaResolucao)
	// Invalidar descarta o slug específico (troca/remoção do workspace).
	Invalidar(ctx context.Context, slug string)
	// InvalidarOrganization descarta TODAS as entradas derivadas da
	// organization (cascata de inativação — invalidação grosseira e segura).
	InvalidarOrganization(ctx context.Context, organizationUUID string)
}

// EntradaResolucao é o mínimo que o cache precisa reter: identificadores e
// vitalidade. Nunca a entidade inteira — cache carrega cópia, não verdade.
type EntradaResolucao struct {
	WorkspaceUUID    string
	OrganizationUUID string
	Status           string
}
