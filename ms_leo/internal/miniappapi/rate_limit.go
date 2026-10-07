package miniappapi

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Ограничение частоты запросов по IP — защита от скрипта, который долбит API
// в цикле. Порог с большим запасом: мини-апп сам опрашивает ленту и чат
// (около 30 запросов в минуту на человека), а за одним адресом оператора
// или Wi-Fi зала может сидеть несколько десятков участников.
const (
	rateLimitWindow  = time.Minute
	rateLimitPerIP   = 1500
	rateLimitMaxKeys = 20000
)

type rateWindow struct {
	start time.Time
	count int
}

// RateLimiter — счётчик запросов в скользящем по минутам окне на каждый IP.
// Состояние в памяти процесса: при двух экземплярах лимит фактически вдвое
// мягче, для защиты от перегрузки этого достаточно.
type RateLimiter struct {
	mu      sync.Mutex
	windows map[string]*rateWindow
	limit   int
	window  time.Duration
	now     func() time.Time
}

func NewRateLimiter() *RateLimiter {
	return &RateLimiter{windows: make(map[string]*rateWindow), limit: rateLimitPerIP, window: rateLimitWindow, now: time.Now}
}

// allow — можно ли пропустить ещё один запрос с этого ключа.
func (l *RateLimiter) allow(key string) bool {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	w := l.windows[key]
	if w == nil || now.Sub(w.start) >= l.window {
		if w == nil && len(l.windows) >= rateLimitMaxKeys {
			l.sweep(now)
		}
		l.windows[key] = &rateWindow{start: now, count: 1}
		return true
	}
	w.count++
	return w.count <= l.limit
}

// sweep выбрасывает истёкшие окна, чтобы карта не росла без конца.
func (l *RateLimiter) sweep(now time.Time) {
	for k, w := range l.windows {
		if now.Sub(w.start) >= l.window {
			delete(l.windows, k)
		}
	}
}

// Middleware отвечает 429 сверх лимита. Отдачу файлов и проверки живости
// не считаем: лента с фото делает десятки GET за раз.
func (l *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && !l.allow(clientIP(r)) {
			w.Header().Set("Retry-After", "60")
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"too_many_requests","message":"Слишком много запросов. Подождите минуту."}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP — адрес клиента: за прокси Railway настоящий адрес лежит первым
// в X-Forwarded-For.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		first, _, _ := strings.Cut(xff, ",")
		if ip := strings.TrimSpace(first); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
