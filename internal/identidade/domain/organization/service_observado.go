package organization

import (
	"context"

	"github.com/google/uuid"

	orgmodel "workspace-api/internal/identidade/model/organization"
	"workspace-api/internal/pkg/errobserve"
	"workspace-api/internal/pkg/pagination"
)

// serviceObservado decora o Service do subdomínio observando TODO erro que
// sobe ao chamador (evolução errobserve): o erro sai INTACTO — a telemetria
// nunca muda a resposta ao cliente. Montado DENTRO do NewService, então
// singleton, seed e testes observam igualmente; métodos sem erro (nenhum
// aqui) delegariam pelo embedding.
type serviceObservado struct {
	Service
	obs *errobserve.Observador
}

func (s serviceObservado) observar(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	return s.obs.Observe(ctx, err)
}

func (s serviceObservado) Create(ctx context.Context, in orgmodel.CreateInput) (*orgmodel.Organization, error) {
	o, err := s.Service.Create(ctx, in)
	return o, s.observar(ctx, err)
}

func (s serviceObservado) Read(ctx context.Context, id uuid.UUID) (*orgmodel.Organization, error) {
	o, err := s.Service.Read(ctx, id)
	return o, s.observar(ctx, err)
}

func (s serviceObservado) List(ctx context.Context, f orgmodel.ListFilter) ([]orgmodel.Organization, int64, error) {
	itens, total, err := s.Service.List(ctx, f)
	return itens, total, s.observar(ctx, err)
}

func (s serviceObservado) Update(ctx context.Context, id uuid.UUID, in orgmodel.UpdateInput) (*orgmodel.Organization, error) {
	o, err := s.Service.Update(ctx, id, in)
	return o, s.observar(ctx, err)
}

func (s serviceObservado) Reativar(ctx context.Context, id uuid.UUID) (*orgmodel.Organization, error) {
	o, err := s.Service.Reativar(ctx, id)
	return o, s.observar(ctx, err)
}

func (s serviceObservado) Delete(ctx context.Context, id uuid.UUID) error {
	return s.observar(ctx, s.Service.Delete(ctx, id))
}

func (s serviceObservado) DefinirDominio(ctx context.Context, id uuid.UUID, valor string) (*orgmodel.Organization, error) {
	o, err := s.Service.DefinirDominio(ctx, id, valor)
	return o, s.observar(ctx, err)
}

func (s serviceObservado) RemoverDominio(ctx context.Context, id uuid.UUID) (*orgmodel.Organization, error) {
	o, err := s.Service.RemoverDominio(ctx, id)
	return o, s.observar(ctx, err)
}

func (s serviceObservado) ListarDominiosAtivos(ctx context.Context) ([]DominioRegistrado, error) {
	lista, err := s.Service.ListarDominiosAtivos(ctx)
	return lista, s.observar(ctx, err)
}

func (s serviceObservado) CriarApiKey(ctx context.Context, organizationUUID uuid.UUID, in ApiKeyEntrada) (*orgmodel.ApiKey, string, error) {
	k, chave, err := s.Service.CriarApiKey(ctx, organizationUUID, in)
	return k, chave, s.observar(ctx, err)
}

func (s serviceObservado) ListarApiKeys(ctx context.Context, organizationUUID uuid.UUID, p pagination.Pagination) ([]orgmodel.ApiKey, int64, error) {
	chaves, total, err := s.Service.ListarApiKeys(ctx, organizationUUID, p)
	return chaves, total, s.observar(ctx, err)
}

func (s serviceObservado) RevogarApiKey(ctx context.Context, organizationUUID, chaveUUID uuid.UUID) error {
	return s.observar(ctx, s.Service.RevogarApiKey(ctx, organizationUUID, chaveUUID))
}
