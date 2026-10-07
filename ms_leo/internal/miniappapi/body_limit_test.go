package miniappapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLimitBody(t *testing.T) {
	var read int64
	h := LimitBody(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, err := io.Copy(io.Discard, r.Body)
		read = n
		if err != nil {
			http.Error(w, "too large", http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/miniapp/x", strings.NewReader(`{"a":1}`)))
	if rec.Code != http.StatusOK || read != 7 {
		t.Fatalf("обычный запрос: код %d, прочитано %d", rec.Code, read)
	}

	// Честный Content-Length сверх лимита — отказ сразу, тело не читаем.
	rec = httptest.NewRecorder()
	big := httptest.NewRequest(http.MethodPost, "/api/miniapp/x", strings.NewReader("x"))
	big.ContentLength = MaxRequestBodyBytes + 1
	read = -1
	h.ServeHTTP(rec, big)
	if rec.Code != http.StatusRequestEntityTooLarge || read != -1 {
		t.Fatalf("большой Content-Length: код %d, прочитано %d", rec.Code, read)
	}

	// Длина не объявлена (chunked) — обрываем на лимите.
	rec = httptest.NewRecorder()
	chunked := httptest.NewRequest(http.MethodPost, "/api/miniapp/x", io.LimitReader(zeros{}, MaxRequestBodyBytes+1024))
	chunked.ContentLength = -1
	h.ServeHTTP(rec, chunked)
	if rec.Code != http.StatusRequestEntityTooLarge || read > MaxRequestBodyBytes {
		t.Fatalf("поток без длины: код %d, прочитано %d", rec.Code, read)
	}
}

type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}
