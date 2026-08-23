package rest_err

import (
	"bytes"
	"net/http"
	"net/http/httptest"
)

// novoRecorder monta um ResponseRecorder com corpo bufferado para os testes.
func novoRecorder() *httptest.ResponseRecorder {
	return httptest.NewRecorder()
}

func requisicaoFake() *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/teste", bytes.NewReader(nil))
	req.Header.Set("X-Request-Id", "ray-123")
	return req
}

func requisicaoFakeSemRay() *http.Request {
	return httptest.NewRequest(http.MethodGet, "/api/teste", nil)
}
