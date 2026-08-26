package user

import (
	"context"
	"time"

	"github.com/google/uuid"

	modeluser "workspace-api/internal/identidade/model/user"
	"workspace-api/internal/pkg/errobserve"
)

// serviceObservado decora o Service do subdomínio observando TODO erro que
// sobe ao chamador (evolução errobserve): o erro sai INTACTO — a telemetria
// nunca muda a resposta ao cliente. Montado DENTRO do NewService, então
// singleton, seed e testes observam igualmente.
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

func (s serviceObservado) Create(ctx context.Context, in EntradaCriacao) (*modeluser.User, error) {
	u, err := s.Service.Create(ctx, in)
	return u, s.observar(ctx, err)
}

func (s serviceObservado) Read(ctx context.Context, id uuid.UUID) (*modeluser.User, error) {
	u, err := s.Service.Read(ctx, id)
	return u, s.observar(ctx, err)
}

func (s serviceObservado) List(ctx context.Context, f modeluser.ListFilter) ([]modeluser.User, int64, error) {
	itens, total, err := s.Service.List(ctx, f)
	return itens, total, s.observar(ctx, err)
}

func (s serviceObservado) Update(ctx context.Context, id uuid.UUID, in modeluser.UpdateInput) (*modeluser.User, error) {
	u, err := s.Service.Update(ctx, id, in)
	return u, s.observar(ctx, err)
}

func (s serviceObservado) Delete(ctx context.Context, id uuid.UUID) error {
	return s.observar(ctx, s.Service.Delete(ctx, id))
}

func (s serviceObservado) Autenticar(ctx context.Context, email string, senha string) (*modeluser.User, error) {
	u, err := s.Service.Autenticar(ctx, email, senha)
	return u, s.observar(ctx, err)
}

func (s serviceObservado) RegistrarSessao(ctx context.Context, usuarioUUID uuid.UUID, jti string, expiraEm time.Time) error {
	return s.observar(ctx, s.Service.RegistrarSessao(ctx, usuarioUUID, jti, expiraEm))
}

func (s serviceObservado) SessaoAtiva(ctx context.Context, usuarioUUID uuid.UUID, jti string) (bool, error) {
	ativa, err := s.Service.SessaoAtiva(ctx, usuarioUUID, jti)
	return ativa, s.observar(ctx, err)
}

func (s serviceObservado) EncerrarSessao(ctx context.Context, usuarioUUID uuid.UUID, jti string) error {
	return s.observar(ctx, s.Service.EncerrarSessao(ctx, usuarioUUID, jti))
}

func (s serviceObservado) RevogarSessoesDaOrganization(ctx context.Context) (int64, error) {
	encerradas, err := s.Service.RevogarSessoesDaOrganization(ctx)
	return encerradas, s.observar(ctx, err)
}

func (s serviceObservado) TemVinculo(ctx context.Context, usuarioUUID, workspaceUUID uuid.UUID) (bool, error) {
	vinculo, err := s.Service.TemVinculo(ctx, usuarioUUID, workspaceUUID)
	return vinculo, s.observar(ctx, err)
}

func (s serviceObservado) PermissoesEfetivas(ctx context.Context, usuarioUUID, workspaceUUID uuid.UUID) ([]string, error) {
	permissoes, err := s.Service.PermissoesEfetivas(ctx, usuarioUUID, workspaceUUID)
	return permissoes, s.observar(ctx, err)
}

func (s serviceObservado) AtribuirPapel(ctx context.Context, usuarioUUID, workspaceUUID, papelUUID uuid.UUID) (*modeluser.AtribuicaoComPapel, error) {
	atribuicao, err := s.Service.AtribuirPapel(ctx, usuarioUUID, workspaceUUID, papelUUID)
	return atribuicao, s.observar(ctx, err)
}

func (s serviceObservado) Atribuicoes(ctx context.Context, usuarioUUID uuid.UUID) ([]modeluser.AtribuicaoComPapel, error) {
	atribuicoes, err := s.Service.Atribuicoes(ctx, usuarioUUID)
	return atribuicoes, s.observar(ctx, err)
}

func (s serviceObservado) RemoverAtribuicao(ctx context.Context, usuarioUUID, atribuicaoUUID uuid.UUID) error {
	return s.observar(ctx, s.Service.RemoverAtribuicao(ctx, usuarioUUID, atribuicaoUUID))
}

func (s serviceObservado) Papeis(ctx context.Context) ([]modeluser.Papel, error) {
	papeis, err := s.Service.Papeis(ctx)
	return papeis, s.observar(ctx, err)
}
