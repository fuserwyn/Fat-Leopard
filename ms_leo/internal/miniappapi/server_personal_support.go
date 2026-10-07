package miniappapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"leo-bot/internal/bot"
	"leo-bot/internal/database"
)

// Личный чат с Лео и чат поддержки.

// handlePostPersonalChatFeed — серверная история приватного чата юзера с Лео.
// Источник правды для синхронизации между устройствами: localStorage заменён БД.
// Если sinceID > 0 — отдаём только записи новее (incremental polling); иначе —
// последние N сообщений (для первого открытия экрана).
func (s *Server) handlePostPersonalChatFeed(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData string `json:"init_data"`
		SinceID  int64  `json:"since_id"`
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
		s.logger.Warnf("miniapp personal chat feed invalid init: %v", err)
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
		s.logger.Errorf("miniapp personal chat feed assert: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "assert_chat_error")
		return
	}
	items, err := s.bot.MiniappPersonalChatHistory(parsed.User.ID, body.SinceID)
	if err != nil {
		s.logger.Errorf("miniapp personal chat feed load: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "feed_error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "messages": items})
}

func (s *Server) handlePostPersonalChatLike(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData  string `json:"init_data"`
		MessageID int64  `json:"message_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if body.MessageID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "missing_message_id")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp personal chat like invalid init: %v", err)
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
		s.logger.Errorf("miniapp personal chat like assert: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "assert_chat_error")
		return
	}
	if err := s.bot.MiniappPersonalChatLikeToggle(parsed.User.ID, body.MessageID); err != nil {
		s.logger.Errorf("miniapp personal chat like: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "like_error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

// handlePostSupportChatFeed — серверная история отдельного чата поддержки.
func (s *Server) handlePostSupportChatFeed(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData string `json:"init_data"`
		SinceID  int64  `json:"since_id"`
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
		s.logger.Warnf("miniapp support feed invalid init: %v", err)
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
		s.logger.Errorf("miniapp support feed assert: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "assert_chat_error")
		return
	}
	// Первая загрузка ленты (since_id=0) = открытие экрана «Сообщить о проблеме».
	if body.SinceID == 0 {
		s.bot.TrackEvent(database.AnalyticsEvent{
			Name:       database.EventSupportButtonClicked,
			TelegramID: parsed.User.ID,
			Payload:    map[string]any{"from": "miniapp"},
		})
	}
	items, err := s.bot.MiniappSupportChatHistory(parsed.User.ID, body.SinceID)
	if err != nil {
		s.logger.Errorf("miniapp support feed load: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "feed_error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "messages": items})
}

// handlePostSupportChatSend — пользователь пишет в поддержку, не в Лео.
func (s *Server) handlePostSupportChatSend(w http.ResponseWriter, r *http.Request) {
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
		s.logger.Warnf("miniapp support send invalid init: %v", err)
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
		s.logger.Errorf("miniapp support send assert: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "assert_chat_error")
		return
	}
	if err := s.bot.MiniappSupportSendFromUser(parsed.User.ID, text, ""); err != nil {
		s.logger.Errorf("miniapp support send: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "send_error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}
