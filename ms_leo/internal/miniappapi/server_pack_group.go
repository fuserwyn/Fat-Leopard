package miniappapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"leo-bot/internal/bot"
)

// Общий чат стаи: сообщения, поиск, реакции, непрочитанное.

func (s *Server) handlePostPackGroupReport(w http.ResponseWriter, r *http.Request) {
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
		s.logger.Warnf("miniapp pack group report: invalid init: %v", err)
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
	if err := s.bot.PackGroupChatReport(parsed.User.ID, parsed, body.MessageID); err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedSocialForbidden) {
			s.jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
		if errors.Is(err, bot.ErrPackGroupMessageNotFound) {
			s.jsonErr(w, http.StatusNotFound, "not_found")
			return
		}
		if errors.Is(err, bot.ErrFeedReportSelf) {
			s.jsonErr(w, http.StatusBadRequest, "cannot_report_self")
			return
		}
		if errors.Is(err, bot.ErrFeedReportLeo) {
			s.jsonErr(w, http.StatusBadRequest, "cannot_report_leo")
			return
		}
		if errors.Is(err, bot.ErrFeedReportAlreadyExists) {
			s.jsonErr(w, http.StatusConflict, "already_reported")
			return
		}
		s.logger.Errorf("pack group report: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "report_error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (s *Server) handlePostPackGroupFeed(w http.ResponseWriter, r *http.Request) {
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
		s.logger.Warnf("miniapp pack group feed: invalid init: %v", err)
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
	items, err := s.bot.PackGroupChatForViewer(parsed.User.ID, parsed, body.InitData)
	if err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		if errors.Is(err, bot.ErrPackFeedForbidden) {
			s.jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
		s.logger.Errorf("pack group feed: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "pack_group_error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "messages": items})
}

func (s *Server) handlePostPackGroupSearch(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData string `json:"init_data"`
		Query    string `json:"query"`
		Limit    int    `json:"limit"`
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
	items, err := s.bot.PackGroupChatSearch(parsed.User.ID, parsed, body.Query, body.Limit)
	if err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		if errors.Is(err, bot.ErrPackFeedForbidden) {
			s.jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
		s.logger.Errorf("pack group search: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "pack_group_error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "messages": items})
}

func (s *Server) handlePostPackGroupMessage(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData  string `json:"init_data"`
		Text      string `json:"text"`
		ReplyToID int64  `json:"reply_to_id"`
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
		s.logger.Warnf("miniapp pack group msg: invalid init: %v", err)
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
		s.jsonErr(w, http.StatusInternalServerError, "assert_chat_error")
		return
	}
	miniRes, perr := s.bot.ProcessMiniAppPackGroupMessage(parsed, text, body.ReplyToID, "")
	if perr != nil {
		if errors.Is(perr, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		if errors.Is(perr, bot.ErrPackFeedForbidden) {
			s.jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
		if errors.Is(perr, bot.ErrPackGroupInvalidReply) {
			s.jsonErr(w, http.StatusBadRequest, "invalid_reply")
			return
		}
		if s.jsonModerationErr(w, perr) {
			return
		}
		s.logger.Errorf("pack group message: %v", perr)
		s.jsonErr(w, http.StatusInternalServerError, "pack_group_error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	out := map[string]any{"ok": true}
	if miniRes.ReplyText != "" {
		out["reply_text"] = miniRes.ReplyText
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) handlePostPackGroupMessageDelete(w http.ResponseWriter, r *http.Request) {
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
		s.logger.Warnf("miniapp pack group delete: invalid init: %v", err)
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
	deleted, derr := s.bot.DeleteMiniAppPackGroupMessage(parsed.User.ID, parsed, body.MessageID)
	if derr != nil {
		if errors.Is(derr, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		if errors.Is(derr, bot.ErrPackFeedForbidden) {
			s.jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
		s.logger.Errorf("pack group delete: %v", derr)
		s.jsonErr(w, http.StatusInternalServerError, "pack_group_delete_error")
		return
	}
	if !deleted {
		s.jsonErr(w, http.StatusNotFound, "not_found")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (s *Server) handlePostPackGroupMessageEdit(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData  string `json:"init_data"`
		MessageID int64  `json:"message_id"`
		Text      string `json:"text"`
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
	text := strings.TrimSpace(body.Text)
	if text == "" {
		s.jsonErr(w, http.StatusBadRequest, "empty_text")
		return
	}
	if utf8.RuneCountInString(text) > maxTextRunes {
		s.jsonErr(w, http.StatusBadRequest, "text_too_long")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp pack group edit: invalid init: %v", err)
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
	updated, uerr := s.bot.EditMiniAppPackGroupMessage(parsed.User.ID, parsed, body.MessageID, text)
	if uerr != nil {
		if errors.Is(uerr, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		if errors.Is(uerr, bot.ErrPackFeedForbidden) {
			s.jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
		if s.jsonModerationErr(w, uerr) {
			return
		}
		s.logger.Errorf("pack group edit: %v", uerr)
		s.jsonErr(w, http.StatusInternalServerError, "pack_group_edit_error")
		return
	}
	if !updated {
		s.jsonErr(w, http.StatusNotFound, "not_found")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (s *Server) handlePostPackGroupUnreadCount(w http.ResponseWriter, r *http.Request) {
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
		s.logger.Warnf("miniapp pack group unread count: invalid init: %v", err)
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
	summary, err := s.bot.MiniappPackGroupUnreadSummary(parsed, parsed.User.ID)
	if err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		s.logger.Errorf("pack group unread summary: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "unread_error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "count": summary.Count, "pack_message_ids": summary.MessageIDs})
}

func (s *Server) handlePostPackGroupUnreadClear(w http.ResponseWriter, r *http.Request) {
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
		s.logger.Warnf("miniapp pack group unread clear: invalid init: %v", err)
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
	if err := s.bot.MiniappPackGroupUnreadClear(parsed, parsed.User.ID); err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		s.logger.Errorf("pack group unread clear: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "unread_error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (s *Server) handlePostPackGroupReact(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData  string `json:"init_data"`
		MessageID int64  `json:"message_id"`
		Emoji     string `json:"emoji"`
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
		s.logger.Warnf("miniapp pack group react: invalid init: %v", err)
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
	if err := s.bot.PackGroupChatReact(parsed.User.ID, parsed, body.MessageID, body.Emoji); err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		if errors.Is(err, bot.ErrPackFeedForbidden) {
			s.jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedInvalidEmoji) {
			s.jsonErr(w, http.StatusBadRequest, "invalid_emoji")
			return
		}
		if errors.Is(err, bot.ErrPackGroupMessageNotFound) {
			s.jsonErr(w, http.StatusNotFound, "not_found")
			return
		}
		s.logger.Errorf("pack group react: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "react_error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}
