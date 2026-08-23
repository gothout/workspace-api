package middleware

import (
	"context"
	"errors"
	"net"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"workspace-api/internal/pkg/config"
	"workspace-api/internal/pkg/orgctx"
	"workspace-api/internal/pkg/rest_err"
)

// resolucaoHost é o resultado do casamento do Host: modo plataforma
// ({slug}.{base_domain}), modo custom ({slug}.{dominio-da-organization}) ou
// host desconhecido.
type resolucaoHost struct {
	slug         string    // rótulo à esquerda do sufixo casado ("" = sem workspace)
	orgDono      uuid.UUID // organization dona do domínio custom (Nil na plataforma)
	desconhecido bool
}

// --- Passo 2: ResolveWorkspace ----------------------------------------------

// ResolveWorkspace responde "qual workspace?": casa o Host contra o domínio-base
// da plataforma e os domínios custom white-label (case-insensitive, sem porta),
// resolve e valida o workspace, exige vínculo com a identidade e injeta escopo +
// permissões efetivas. Fail-closed em todos os passos:
//
//	host fora dos domínios conhecidos ......... 404 (nunca fallback aberto)
//	workspace inexistente/inativo/alheio ...... 404 (não vaza existência)
//	sem vínculo user↔workspace (ou escopo) .... 403 (suporte é a exceção auditada)
//	falha de infraestrutura ................... 500 (sobe intacta)
func (c *Cadeia) ResolveWorkspace() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if c.deps.Workspaces == nil || c.deps.Permissoes == nil {
			responderFechada(ctx, "resolucao.cadeia_nao_inicializada")
			return
		}
		id := identidadeDo(ctx.Request.Context())
		if id == nil {
			// SetContextAuthorization não rodou: cadeia declarada incompleta.
			responderFechada(ctx, "resolucao.sem_autenticacao")
			return
		}
		cfg, err := config.Use()
		if err != nil {
			rest_err.WriteError(ctx, rest_err.Interno(err))
			return
		}

		host := hostSemPorta(ctx.Request.Host)
		res, err := c.casarHost(ctx.Request.Context(), host, cfg.App.BaseDomain)
		if err != nil {
			slogErro(ctx, "resolucao.dominios_custom_falhou", err)
			rest_err.WriteError(ctx, rest_err.Interno(err))
			return
		}
		if res.desconhecido {
			slogRecusado(ctx, "resolucao.host_desconhecido",
				"dominio", "identidade", "acao", "resolver_workspace", "success", false,
				"user_uuid", id.usuarioUUID().String())
			rest_err.WriteError(ctx, rest_err.NewNotFoundError("Recurso não encontrado."))
			return
		}
		if res.slug != "" && contemRotuloFixo(c.deps.Workspaces.Fixos(), res.slug) {
			res.slug = "" // endereço fixo da plataforma (api/painel/www...) nunca é workspace
		}

		if res.slug == "" {
			c.seguirSemWorkspace(ctx, id)
			return
		}
		c.resolverPorSlug(ctx, id, res)
	}
}

// casarHost aplica as regras 1–3 do doc 03: sufixo case-insensitive sem porta
// contra base_domain e domínios custom; rótulo à esquerda do sufixo é o slug;
// Host desconhecido NUNCA tem fallback aberto.
func (c *Cadeia) casarHost(ctx context.Context, host, baseDomainBruto string) (resolucaoHost, error) {
	baseDomain := normalizarDominio(baseDomainBruto)

	// Modo plataforma primeiro — {slug}.{base_domain} e o próprio base_domain nu.
	if baseDomain != "" {
		if slug, casou := rotuloDeSufixo(host, baseDomain); casou {
			return resolucaoHost{slug: slug}, nil
		}
	}

	// Modo white-label: {slug}.{dominio-custom} da organization dona do domínio.
	var dominios []DominioCustom
	if c.deps.DominiosCustom != nil {
		listados, err := c.deps.DominiosCustom.Listar(ctx)
		if err != nil {
			return resolucaoHost{}, err
		}
		dominios = listados
	}
	for _, d := range dominios {
		dominio := normalizarDominio(d.Dominio)
		if dominio == "" || dominio == baseDomain {
			continue
		}
		if slug, casou := rotuloDeSufixo(host, dominio); casou {
			return resolucaoHost{slug: slug, orgDono: d.OrganizationUUID}, nil
		}
	}
	return resolucaoHost{desconhecido: true}, nil
}

// resolverPorSlug valida o workspace do rótulo (regras 3–4), o pertencimento
// ao domínio custom quando for o caso, e o vínculo da identidade (regra 7).
func (c *Cadeia) resolverPorSlug(ctx *gin.Context, id *identidade, res resolucaoHost) {
	ws, err := c.deps.Workspaces.BuscarPorSlug(ctx.Request.Context(), res.slug)
	if err != nil {
		if errors.Is(err, ErrNaoEncontrado) {
			c.recusar404(ctx, id)
			return
		}
		slogErro(ctx, "resolucao.busca_falhou", err)
		rest_err.WriteError(ctx, rest_err.Interno(err))
		return
	}
	if ws == nil || !ws.Ativo {
		c.recusar404(ctx, id)
		return
	}
	// Regra 3: workspace alheio no domínio do parceiro = 404 (não vaza existência).
	if res.orgDono != uuid.Nil && ws.OrganizationUUID != res.orgDono {
		c.recusar404(ctx, id)
		return
	}
	if ctx.GetHeader("X-Workspace-Id") != "" {
		// Regra 6: subdomínio VENCE header divergente — loga e ignora.
		slogRecusado(ctx, "resolucao.header_ignorado_pelo_subdominio",
			"workspace_uuid", ws.UUID.String())
	}
	c.concluir(ctx, id, ws)
}

// seguirSemWorkspace trata Host sem rótulo (base_domain nu, api., painel.,
// localhost, IP): console master/acesso direto. X-Workspace-Id vale SÓ aqui
// como fallback de integrações/dev (regra 5); sem ele segue SEM escopo de
// workspace — rotas protegidas negam por falta de permissão (fail-closed).
func (c *Cadeia) seguirSemWorkspace(ctx *gin.Context, id *identidade) {
	header := strings.TrimSpace(ctx.GetHeader("X-Workspace-Id"))
	if header == "" {
		ctx.Next()
		return
	}
	idWorkspace, err := uuid.Parse(header)
	if err != nil {
		slogRecusado(ctx, "resolucao.header_invalido",
			"dominio", "identidade", "acao", "resolver_workspace", "success", false)
		rest_err.WriteError(ctx, rest_err.NewBadRequestError("Cabeçalho X-Workspace-Id inválido."))
		return
	}
	ws, err := c.deps.Workspaces.BuscarPorUUID(ctx.Request.Context(), idWorkspace)
	if err != nil {
		if errors.Is(err, ErrNaoEncontrado) {
			c.recusar404(ctx, id)
			return
		}
		slogErro(ctx, "resolucao.busca_falhou", err)
		rest_err.WriteError(ctx, rest_err.Interno(err))
		return
	}
	if ws == nil || !ws.Ativo {
		c.recusar404(ctx, id)
		return
	}
	c.concluir(ctx, id, ws)
}

// concluir exige o vínculo (regra 7), injeta escopo + permissões efetivas e
// libera a requisição.
func (c *Cadeia) concluir(ctx *gin.Context, id *identidade, ws *WorkspaceResolvido) {
	var perms []string

	switch id.tipo {
	case identidadeHumana:
		usuario := id.usuarioUUID()
		vinculo, err := c.deps.Permissoes.TemVinculo(ctx.Request.Context(), usuario, ws.OrganizationUUID, ws.UUID)
		if err != nil {
			slogErro(ctx, "resolucao.vinculo_falhou", err)
			rest_err.WriteError(ctx, rest_err.Interno(err))
			return
		}
		if !vinculo {
			c.negarVinculo(ctx, usuario, ws)
			return
		}
		efetivas, err := c.deps.Permissoes.PermissoesEfetivas(ctx.Request.Context(), usuario, ws.OrganizationUUID, ws.UUID)
		if err != nil {
			slogErro(ctx, "resolucao.permissoes_falharam", err)
			rest_err.WriteError(ctx, rest_err.Interno(err))
			return
		}
		perms = efetivas

	case identidadeChave:
		chave := id.chave
		if chave.OrganizationUUID != ws.OrganizationUUID {
			c.negarVinculo(ctx, uuid.Nil, ws)
			return
		}
		if !chave.EscopoOrganization && !contemUUID(chave.WorkspacesPermitidos, ws.UUID) {
			// O "vínculo" da chave É o escopo dela — workspace fora da lista = 403.
			c.negarVinculo(ctx, uuid.Nil, ws)
			return
		}
		perms = chave.Permissoes
	}

	novo := orgctx.WithOrganization(ctx.Request.Context(), ws.OrganizationUUID)
	novo = orgctx.WithWorkspace(novo, ws.UUID)
	novo = orgctx.WithPermissoes(novo, perms)
	ctx.Request = ctx.Request.WithContext(novo)
	ctx.Next()
}

func (c *Cadeia) recusar404(ctx *gin.Context, id *identidade) {
	slogRecusado(ctx, "resolucao.workspace_nao_resolvido",
		"dominio", "identidade", "acao", "resolver_workspace", "success", false,
		"user_uuid", id.usuarioUUID().String())
	rest_err.WriteError(ctx, rest_err.NewNotFoundError("Recurso não encontrado."))
}

func (c *Cadeia) negarVinculo(ctx *gin.Context, usuario uuid.UUID, ws *WorkspaceResolvido) {
	slogRecusado(ctx, "resolucao.sem_vinculo",
		"dominio", "identidade", "acao", "resolver_workspace", "success", false,
		"user_uuid", usuario.String(),
		"organization_uuid", ws.OrganizationUUID.String(),
		"workspace_uuid", ws.UUID.String())
	rest_err.WriteError(ctx, rest_err.NewForbiddenError("Sem vínculo com este workspace."))
}

// --- Helpers de Host ---------------------------------------------------------

// hostSemPorta normaliza o Host: lowercase, sem porta (dev usa localhost:8080).
func hostSemPorta(bruto string) string {
	host := strings.ToLower(strings.TrimSpace(bruto))
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}

// normalizarDominio prepara um domínio de referência para casamento.
func normalizarDominio(d string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(d), "."))
}

// rotuloDeSufixo devolve o rótulo à esquerda do sufixo casado: para
// host=filial-sul.exemplo.com e dominio=exemplo.com devolve "filial-sul".
// Host IGUAL ao domínio (sem rótulo) casou mas não tem workspace ("").
func rotuloDeSufixo(host, dominio string) (string, bool) {
	if host == "" || dominio == "" {
		return "", false
	}
	switch {
	case host == dominio:
		return "", true
	case strings.HasSuffix(host, "."+dominio):
		return strings.TrimSuffix(host, "."+dominio), true
	default:
		return "", false
	}
}

func contemRotuloFixo(fixos []string, slug string) bool {
	for _, f := range fixos {
		if strings.EqualFold(strings.TrimSpace(f), slug) {
			return true
		}
	}
	return false
}

func contemUUID(lista []uuid.UUID, alvo uuid.UUID) bool {
	for _, u := range lista {
		if u == alvo {
			return true
		}
	}
	return false
}
