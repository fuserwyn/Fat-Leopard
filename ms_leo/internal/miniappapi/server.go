package miniappapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"leo-bot/internal/bot"
	"leo-bot/internal/logger"

	initdata "github.com/telegram-mini-apps/init-data-golang"
)

const maxTextRunes = 4000

type Server struct {
	bot    *bot.Bot
	token  string
	logger logger.Logger
}

// New — HTTP-оболочка для мини-апpa: валидация initData и тот же обработчик, что getUpdates.
func New(b *bot.Bot, token string, log logger.Logger) http.Handler {
	s := &Server{bot: b, token: token, logger: log}
	return withCORS(http.HandlerFunc(s.serve))
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case path == "/healthz" && r.Method == http.MethodGet:
		s.handleHealthz(w, r)
	case path == "/api/miniapp/messages" && r.Method == http.MethodPost:
		s.handlePostMessage(w, r)
	case path == "/api/miniapp/feed" && r.Method == http.MethodPost:
		s.handlePostFeed(w, r)
	case path == "/" && r.Method == http.MethodGet:
		s.handleRoot(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePostMessage(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}

	var body struct {
		InitData string `json:"init_data"`
		Text     string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" {
		s.jsonErr(w, http.StatusBadRequest, "empty_text")
		return
	}
	if utf8.RuneCountInString(text) > maxTextRunes {
		s.jsonErr(w, http.StatusBadRequest, "text_too_long")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if err := initdata.Validate(body.InitData, s.token, 24*time.Hour); err != nil {
		s.logger.Warnf("miniapp init_data invalid: %v", err)
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := initdata.Parse(body.InitData)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return
	}
	if err := s.bot.AssertMiniAppPackChatAligns(parsed); err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		s.logger.Errorf("miniapp assert pack chat: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "assert_chat_error")
		return
	}
	miniRes := s.bot.ProcessMiniAppPrivateText(parsed, text)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	out := map[string]any{"ok": true}
	if miniRes.ReplyText != "" {
		out["reply_text"] = miniRes.ReplyText
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) handlePostFeed(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData string `json:"init_data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if err := initdata.Validate(body.InitData, s.token, 24*time.Hour); err != nil {
		s.logger.Warnf("miniapp feed init_data invalid: %v", err)
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := initdata.Parse(body.InitData)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return
	}
	items, err := s.bot.PackFeedForViewer(parsed.User.ID, parsed)
	if err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		if errors.Is(err, bot.ErrPackFeedForbidden) {
			s.jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
		s.logger.Errorf("pack feed: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "feed_error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "items": items})
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
