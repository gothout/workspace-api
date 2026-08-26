// Adaptadores do domínio licensing: ligam os contratos ENTRE os três
// subdomínios irmãos (módulo × licença × ativação) resolvendo os singletons
// NA CHAMADA — mesmo desenho dos adaptadores de identidade (regra 4 de
// agents/01). Nenhuma regra de negócio aqui: só tradução de vocabulário
// (ErrNotFound de um lado → sentinela do contrato do outro).
package bootstrap

import (
	"context"
	"errors"

	"github.com/google/uuid"

	dominioLicenca "workspace-api/internal/licensing/domain/licenca"
	dominioModulo "workspace-api/internal/licensing/domain/modulo"
	dominioAtivacao "workspace-api/internal/licensing/domain/ativacao"
	aplicacaoaplicacoes "workspace-api/internal/licensing/application/aplicacoes"
	modelativacao "workspace-api/internal/licensing/model/ativacao"
	modelmodulo "workspace-api/internal/licensing/model/modulo"
	aplicacaoauth "workspace-api/internal/identidade/application/auth"
	dominioWorkspace "workspace-api/internal/identidade/domain/workspace"
	"workspace-api/internal/middleware"
)

// --- modulo → licenca --------------------------------------------------------

// verificadorLicencasDoModulo liga o guarda de remoção do catálogo ao
// agregado licença (ExisteParaModulo é global por natureza).
type verificadorLicencasDoModulo struct{}

func (verificadorLicencasDoModulo) ExisteParaModulo(ctx context.Context, moduloUUID uuid.UUID) (bool, error) {
	return dominioLicenca.MustUse().Repository.ExisteParaModulo(ctx, moduloUUID)
}

var _ dominioModulo.VerificadorLicencas = verificadorLicencasDoModulo{}

// --- licenca → modulo ---------------------------------------------------------

// buscadorModulosDaLicenca resolve o slug informado na atribuição; traduz o
// ErrNotFound do irmão para o vocabulário DO CONTRATO da licença.
type buscadorModulosDaLicenca struct{}

func (buscadorModulosDaLicenca) BuscarPorSlug(ctx context.Context, slug string) (*modelmodulo.Modulo, error) {
	m, err := dominioModulo.MustUse().Service.BuscarPorSlug(ctx, slug)
	if err != nil {
		if errors.Is(err, dominioModulo.ErrNotFound) {
			return nil, dominioLicenca.ErrModuloNaoEncontrado
		}
		return nil, err
	}
	return m, nil
}

var _ dominioLicenca.BuscadorModulos = buscadorModulosDaLicenca{}

// --- ativacao → {modulo, licenca, workspace} ---------------------------------

// buscadorModulosDaAtivacao resolve o slug na ativação; módulo inativo NÃO é
// erro aqui — o service da ativação decide (ErrModuloInvalido).
type buscadorModulosDaAtivacao struct{}

func (buscadorModulosDaAtivacao) BuscarPorSlug(ctx context.Context, slug string) (*modelmodulo.Modulo, error) {
	m, err := dominioModulo.MustUse().Service.BuscarPorSlug(ctx, slug)
	if err != nil {
		if errors.Is(err, dominioModulo.ErrNotFound) {
			return nil, dominioAtivacao.ErrModuloNaoEncontrado
		}
		return nil, err
	}
	return m, nil
}

var _ dominioAtivacao.BuscadorModulos = buscadorModulosDaAtivacao{}

// verificadorLicencasDaAtivacao pergunta à licença se a organization tem a
// concessão viva do módulo.
type verificadorLicencasDaAtivacao struct{}

func (verificadorLicencasDaAtivacao) Existe(ctx context.Context, organizationUUID, moduloUUID uuid.UUID) (bool, error) {
	return dominioLicenca.MustUse().Service.ExisteViva(ctx, organizationUUID, moduloUUID)
}

var _ dominioAtivacao.VerificadorLicencas = verificadorLicencasDaAtivacao{}

// validadorWorkspacesDaAtivacao confere pertencimento+vitalidade pelo finder
// GLOBAL do irmão workspace (mesma regra do adaptador do user): não
// encontrado = false sem erro — "alheio/inexistente" são a mesma recusa.
type validadorWorkspacesDaAtivacao struct{}

func (validadorWorkspacesDaAtivacao) Pertence(ctx context.Context, organizationUUID, workspaceUUID uuid.UUID) (bool, error) {
	resolvido, err := dominioWorkspace.MustUse().Service.ResolverPorUUID(ctx, workspaceUUID)
	if err != nil {
		if errors.Is(err, dominioWorkspace.ErrNotFound) {
			return false, nil // inexistente/alheio/removido: mesma recusa
		}
		return false, err
	}
	return resolvido.OrganizationUUID == organizationUUID && resolvido.Ativo(), nil
}

var _ dominioAtivacao.ValidadorWorkspaces = validadorWorkspacesDaAtivacao{}

// --- middleware → ativação (resolução de módulos liberados) ------------------

// resolvedorAplicacoesLicensing implementa o contrato do middleware com o
// cache app:{org}:{ws} NA FRENTE e a consulta real no service da ativação —
// o caminho quente de TODA requisição de módulo nunca depende do Redis para
// decidir: miss/falha caem ao Postgres (degradar, não bloquear). A base é
// injetável para os testes de composição; nil = singleton resolvido NA
// CHAMADA (regra do bootstrap).
type resolvedorAplicacoesLicensing struct {
	base  func(ctx context.Context, organizationUUID, workspaceUUID uuid.UUID) ([]string, error)
	cache cacheAplicacoesRedis
}

func novoResolvedorAplicacoesComCache(base func(ctx context.Context, organizationUUID, workspaceUUID uuid.UUID) ([]string, error)) resolvedorAplicacoesLicensing {
	if base == nil {
		base = func(ctx context.Context, organizationUUID, workspaceUUID uuid.UUID) ([]string, error) {
			itens, err := dominioAtivacao.MustUse().Service.SlugsLiberados(ctx, organizationUUID, workspaceUUID)
			if err != nil {
				return nil, err
			}
			slugs := make([]string, 0, len(itens))
			for _, item := range itens {
				slugs = append(slugs, item.Slug)
			}
			return slugs, nil
		}
	}
	return resolvedorAplicacoesLicensing{base: base, cache: cacheAplicacoesRedis{}}
}

func (r resolvedorAplicacoesLicensing) Liberadas(ctx context.Context, organizationUUID, workspaceUUID uuid.UUID) ([]string, error) {
	if organizationUUID == uuid.Nil || workspaceUUID == uuid.Nil {
		return nil, nil // sem par resolvido não há módulo liberado — nega no passo
	}
	if slugs, ok := r.cache.Buscar(ctx, organizationUUID.String(), workspaceUUID.String()); ok {
		return slugs, nil
	}
	slugs, err := r.base(ctx, organizationUUID, workspaceUUID)
	if err != nil {
		return nil, err
	}
	r.cache.Guardar(ctx, organizationUUID.String(), workspaceUUID.String(), slugs)
	return slugs, nil
}

var _ middleware.ResolvedorAplicacoes = resolvedorAplicacoesLicensing{}

// --- aplicação aplicacoes → ativação -----------------------------------------

// provedorAcessosAplicacoes liga o contrato da aplicação ao service da
// ativação, resolvido NA CHAMADA — mesmo desenho dos demais adaptadores.
type provedorAcessosAplicacoes struct{}

func (provedorAcessosAplicacoes) Liberadas(ctx context.Context, organizationUUID, workspaceUUID uuid.UUID) ([]modelativacao.AplicacaoDisponivelDto, error) {
	return dominioAtivacao.MustUse().Service.SlugsLiberados(ctx, organizationUUID, workspaceUUID)
}

var _ aplicacaoaplicacoes.ProvedorAcessos = provedorAcessosAplicacoes{}

// --- aplicação auth → ativação (seletor no login, F9) ------------------------

// provedorAcessosAuth liga o contrato DECORATIVO do login ao service da
// ativação, resolvido NA CHAMADA — falha do provedor vira lista vazia no
// service do auth, nunca recusa de sessão.
type provedorAcessosAuth struct{}

func (provedorAcessosAuth) Aplicacoes(ctx context.Context, organizationUUID, workspaceUUID uuid.UUID) ([]modelativacao.AplicacaoDisponivelDto, error) {
	return dominioAtivacao.MustUse().Service.SlugsLiberados(ctx, organizationUUID, workspaceUUID)
}

var _ aplicacaoauth.ProvedorAcessos = provedorAcessosAuth{}
