package miniappapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"leo-bot/internal/bot"
)

// Сообщения Лео из мини-аппа и личные ответы.

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
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp init_data invalid: %v", err)
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(body.InitData)
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
	if miniRes.Blocked {
		code := miniRes.BlockCode
		if code == "" {
			code = "moderation_blocked"
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(bot.ModerationHTTPStatus(code))
		_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "message": miniRes.ReplyText})
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	out := map[string]any{"ok": true}
	if miniRes.Pending {
		out["pending"] = true
	}
	if miniRes.ReplyText != "" {
		out["reply_text"] = miniRes.ReplyText
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) handlePostPersonalReplyPoll(w http.ResponseWriter, r *http.Request) {
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
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp personal poll: invalid init: %v", err)
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(body.InitData)
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
		s.logger.Errorf("miniapp personal poll assert chat: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "assert_chat_error")
		return
	}
	txt, has := s.bot.PopMiniappPersonalReply(parsed.User.ID)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	out := map[string]any{"ok": true}
	if has && txt != "" {
		out["reply_text"] = txt
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) handlePostPersonalReplyDrain(w http.ResponseWriter, r *http.Request) {
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
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp personal drain: invalid init: %v", err)
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(body.InitData)
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
		s.logger.Errorf("miniapp personal drain assert chat: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "assert_chat_error")
		return
	}
	cleared := s.bot.ClearMiniappPersonalReplyQueue(parsed.User.ID)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "cleared": cleared})
}

func (s *Server) handlePostPersonalPendingCount(w http.ResponseWriter, r *http.Request) {
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
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp pending count: invalid init: %v", err)
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(body.InitData)
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
		s.logger.Errorf("miniapp pending count assert chat: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "assert_chat_error")
		return
	}
	n := s.bot.MiniappPersonalQueueLen(parsed.User.ID)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":    true,
		"count": n,
	})
}
