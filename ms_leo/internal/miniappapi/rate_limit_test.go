package miniappapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiter(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	l := NewRateLimiter()
	l.limit = 3
	l.now = func() time.Time { return now }
	h := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))

	do := func(method, ip string) int {
		r := httptest.NewRequest(method, "/api/miniapp/x", nil)
		r.Header.Set("X-Forwarded-For", ip+", 10.0.0.1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec.Code
	}
	for i := 0; i < 3; i++ {
		if code := do(http.MethodPost, "1.1.1.1"); code != http.StatusOK {
			t.Fatalf("запрос %d: %d", i+1, code)
		}
	}
	if code := do(http.MethodPost, "1.1.1.1"); code != http.StatusTooManyRequests {
		t.Fatalf("сверх лимита: %d", code)
	}
	if code := do(http.MethodPost, "2.2.2.2"); code != http.StatusOK {
		t.Fatalf("другой адрес не должен страдать: %d", code)
	}
	if code := do(http.MethodGet, "1.1.1.1"); code != http.StatusOK {
		t.Fatalf("GET не считаем: %d", code)
	}
	now = now.Add(time.Minute)
	if code := do(http.MethodPost, "1.1.1.1"); code != http.StatusOK {
		t.Fatalf("через минуту окно новое: %d", code)
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.RemoteAddr = "9.9.9.9:1234"
	if got := clientIP(r); got != "9.9.9.9" {
		t.Errorf("без прокси: %q", got)
	}
	r.Header.Set("X-Forwarded-For", " 5.5.5.5 , 10.0.0.1")
	if got := clientIP(r); got != "5.5.5.5" {
		t.Errorf("за прокси: %q", got)
	}
}
