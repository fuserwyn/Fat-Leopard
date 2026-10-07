package miniappapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"leo-bot/internal/bot"
)

// Ответы с ошибкой и заголовки CORS.

func (s *Server) jsonModerationErr(w http.ResponseWriter, err error) bool {
	var mod *bot.ModerationBlockedError
	if !errors.As(err, &mod) || mod == nil {
		return false
	}
	code := mod.APICode
	if code == "" {
		code = "moderation_blocked"
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(bot.ModerationHTTPStatus(code))
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "message": mod.Message})
	return true
}

func (s *Server) jsonErr(w http.ResponseWriter, code int, err string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err})
}

func withCORS(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			corsWriteHeaders(w, r)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		corsWriteHeaders(w, r)
		h.ServeHTTP(w, r)
	})
}

func corsWriteHeaders(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	acc := "Content-Type"
	if rh := r.Header.Get("Access-Control-Request-Headers"); strings.TrimSpace(rh) != "" {
		acc = rh
	}
	w.Header().Set("Access-Control-Allow-Headers", acc)
}
