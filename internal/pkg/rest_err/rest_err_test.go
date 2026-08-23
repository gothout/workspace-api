package rest_err

import (
	"errors"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	errSentinelaA = errors.New("recurso não encontrado")
	errSentinelaB = errors.New("slug já em uso")
)

func catalogoTeste() map[error]ErroCatalogado {
	return map[error]ErroCatalogado{
		errSentinelaA: {Codigo: "teste.recursos.nao_encontrado", Mensagem: "Recurso não encontrado.", Status: http.StatusNotFound},
		errSentinelaB: {Codigo: "teste.recursos.slug_em_uso", Mensagem: "Slug já está em uso.", Status: http.StatusConflict},
	}
}

func TestConstrutoresPadronizados(t *testing.T) {
	casos := []struct {
		nome     string
		rerr     *RestErr
		status   int
		code     string
		errorTxt string
	}{
		{"bad request", NewBadRequestError("entrada inválida"), 400, "sistema.bad_request", "bad_request"},
		{"unauthorized", NewUnauthorizedError("token ausente"), 401, "sistema.unauthorized", "unauthorized"},
		{"forbidden", NewForbiddenError("sem permissão"), 403, "sistema.forbidden", "forbidden"},
		{"not found", NewNotFoundError("não encontrado"), 404, "sistema.not_found", "not_found"},
		{"conflict", NewConflictError("conflito"), 409, "sistema.conflict", "conflict"},
		{"unprocessable", NewUnprocessableEntityError("regra violada"), 422, "sistema.unprocessable_entity", "unprocessable_entity"},
		{"internal", NewInternalServerError("erro interno"), 500, "sistema.internal_server_error", "internal_server_error"},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			assert.Equal(t, caso.status, caso.rerr.Status)
			assert.Equal(t, caso.code, caso.rerr.Code)
			assert.Equal(t, caso.errorTxt, caso.rerr.Error)
			assert.NotEmpty(t, caso.rerr.Message)
		})
	}
}

func TestVarianteComCausaPreservaOriginalSemVazarNoCorpo(t *testing.T) {
	causa := errors.New("detalhe interno do driver")
	rerr := NewConflictErrorComCausa("Slug já está em uso.", causa)
	require.Equal(t, http.StatusConflict, rerr.Status)
	require.ErrorIs(t, rerr.Causa(), causa)
	assert.NotContains(t, rerr.Message, "driver")
	assert.Empty(t, rerr.RayTrace)
}

func TestRegistrarCatalogoEDoCatalogoResolvemSentinela(t *testing.T) {
	ResetarRegistroParaTeste()
	RegistrarCatalogo("teste", "recursos", catalogoTeste())

	rerr := DoCatalogo(errSentinelaB)
	require.Equal(t, http.StatusConflict, rerr.Status)
	assert.Equal(t, "teste.recursos.slug_em_uso", rerr.Code)
	assert.Equal(t, "Slug já está em uso.", rerr.Message)
	assert.Equal(t, "conflict", rerr.Error)
}

func TestDoCatalogoResolveSentinelaEnvolvida(t *testing.T) {
	ResetarRegistroParaTeste()
	RegistrarCatalogo("teste", "recursos", catalogoTeste())

	envolvido := fmtWrap{errSentinelaA, "contexto do service"}
	rerr := DoCatalogo(envolvido)
	assert.Equal(t, "teste.recursos.nao_encontrado", rerr.Code)
}

type fmtWrap struct {
	alvo error
	msg  string
}

func (w fmtWrap) Error() string { return w.msg + ": " + w.alvo.Error() }
func (w fmtWrap) Unwrap() error { return w.alvo }

func TestDoCatalogoComErroDesconhecidoCaiNoInterno(t *testing.T) {
	ResetarRegistroParaTeste()
	RegistrarCatalogo("teste", "recursos", catalogoTeste())

	rerr := DoCatalogo(errors.New("algo inesperado"))
	assert.Equal(t, http.StatusInternalServerError, rerr.Status)
	assert.Equal(t, "sistema.internal_server_error", rerr.Code)
}

func TestDoCatalogoNuloDevolveInterno(t *testing.T) {
	rerr := DoCatalogo(nil)
	assert.Equal(t, http.StatusInternalServerError, rerr.Status)
}

func TestCodigoDuplicadoEntraEmPanico(t *testing.T) {
	ResetarRegistroParaTeste()
	RegistrarCatalogo("teste", "recursos", catalogoTeste())
	duplicado := map[error]ErroCatalogado{
		errors.New("outra sentinela"): {Codigo: "teste.recursos.nao_encontrado", Mensagem: "x", Status: 500},
	}
	assert.PanicsWithValue(t,
		"rest_err: código de erro duplicado \"teste.recursos.nao_encontrado\" (teste/outros já registrado antes)",
		func() { RegistrarCatalogo("teste", "outros", duplicado) })
}

func TestMapaErrosAgrupaOrdenado(t *testing.T) {
	ResetarRegistroParaTeste()
	RegistrarCatalogo("identidade", "workspace", map[error]ErroCatalogado{
		errSentinelaB: {Codigo: "identidade.workspace.slug_em_uso", Mensagem: "Slug já está em uso.", Status: 409},
	})
	RegistrarCatalogo("identidade", "user", map[error]ErroCatalogado{
		errSentinelaA: {Codigo: "identidade.user.credenciais_invalidas", Mensagem: "Credenciais inválidas.", Status: 401},
	})

	grupos := MapaErros()
	require.Len(t, grupos, 2)
	assert.Equal(t, "user", grupos[0].Subdominio, "grupos ordenados por subdomínio dentro do domínio")
	assert.Equal(t, "workspace", grupos[1].Subdominio)
	assert.Equal(t, 401, grupos[0].Erros[0].Status)
}

func TestWriteErrorProduzCorpoPadronizadoEStatusNoHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := novoRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = requisicaoFake()

	WriteError(c, NewNotFoundError("Workspace não encontrado."))

	assert.Equal(t, http.StatusNotFound, recorder.Code)
	body := recorder.Body.String()
	assert.Contains(t, body, `"code":"sistema.not_found"`)
	assert.Contains(t, body, `"error":"not_found"`)
	assert.Contains(t, body, `"message":"Workspace não encontrado."`)
	assert.Contains(t, body, `"ray_trace":"ray-123"`)
	assert.NotContains(t, body, `"status"`)
	assert.True(t, c.IsAborted())
}

func TestWriteErrorGeraRayTraceQuandoAusente(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := novoRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = requisicaoFakeSemRay()

	WriteError(c, NewBadRequestError("inválido"))
	body := recorder.Body.String()
	assert.Contains(t, body, `"ray_trace":"`)
	assert.NotContains(t, body, `"ray_trace":""`)
}

func TestWriteErrorComNilDevolveInterno(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := novoRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = requisicaoFake()

	WriteError(c, nil)
	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
}

// R7 (issue #25): DoCatalogo é DETERMINÍSTICO — a mesma sentinela casando em
// DOIS catálogos (ou com duas entradas por wrapping) devolve sempre o menor
// par (domínio.subdomínio, código), nunca o que o mapa do Go sortear.
func TestDoCatalogoDeterministicoComEmpate(t *testing.T) {
	ResetarRegistroParaTeste()
	defer ResetarRegistroParaTeste()

	sentinela := errors.New("erro compartilhado entre subdomínios")
	registrar("identidade", "workspace", sentinela, "identidade.workspace.ultimo", 404)
	registrar("identidade", "auth", sentinela, "identidade.auth.primeiro", 401)
	registrar("aaa", "zzz", sentinela, "aaa.zzz.empata_no_grupo", 400)

	for i := 0; i < 50; i++ {
		err := DoCatalogo(sentinela)
		assert.Equal(t, "aaa.zzz.empata_no_grupo", err.Code,
			"menor grupo vence — ordem estável em %d execuções", i)
		assert.Equal(t, 400, err.Status)
	}
}

// registrar insere um catálogo de entrada única com o par sentinela → código.
func registrar(grupo, sub string, sentinela error, codigo string, status int) {
	RegistrarCatalogo(grupo, sub, map[error]ErroCatalogado{
		sentinela: {Codigo: codigo, Mensagem: "mensagem", Status: status},
	})
}
