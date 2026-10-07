package miniappapi

import "net/http"

// MaxRequestBodyBytes — потолок тела запроса к API мини-аппа. Самое большое,
// что сюда приходит по делу, — вложение трекера 8 МиБ в base64 (≈11 МиБ)
// и фото тренировки 6 МиБ; всё, что больше, обрываем, не читая.
const MaxRequestBodyBytes = 16 << 20

// LimitBody ограничивает размер тела запроса: без этого один клиент может
// слать гигабайты, и multipart-разбор будет складывать их на диск.
func LimitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > MaxRequestBodyBytes {
			http.Error(w, `{"error":"request_too_large"}`, http.StatusRequestEntityTooLarge)
			return
		}
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodyBytes)
		}
		next.ServeHTTP(w, r)
	})
}
