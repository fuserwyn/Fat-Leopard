package miniappapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"leo-bot/internal/bot"
)

// Лента стаи: посты, реакции, комментарии, опросы, жалобы.

func (s *Server) handlePostFeedTrainingThreadUnreadCount(w http.ResponseWriter, r *http.Request) {
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
		s.logger.Warnf("miniapp feed thread unread count: invalid init: %v", err)
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
	n, cardIDs, err := s.bot.MiniappTrainingThreadUnreadSummary(parsed, parsed.User.ID)
	if err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		s.logger.Errorf("feed thread unread count: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "unread_error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "count": n, "user_message_ids": cardIDs})
}

func (s *Server) handlePostFeedTrainingThreadUnreadClear(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData      string `json:"init_data"`
		UserMessageID int64  `json:"user_message_id"`
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
		s.logger.Warnf("miniapp feed thread unread clear: invalid init: %v", err)
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
	if err := s.bot.MiniappTrainingThreadUnreadClear(parsed, parsed.User.ID, body.UserMessageID); err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		s.logger.Errorf("feed thread unread clear: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "clear_error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

// parseFeedCursorTS — курсор единой ленты из RFC3339-строки. Пусто/битое → nil (без курсора).
func parseFeedCursorTS(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	tu := t.UTC()
	return &tu
}

func (s *Server) handlePostFeed(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData string `json:"init_data"`
		// Курсоры единой ленты — по времени (RFC3339). since_ts: только новее (polling),
		// before_ts: более старые (подгрузка вниз). since_ts имеет приоритет.
		SinceTS  string `json:"since_ts"`
		BeforeTS string `json:"before_ts"`
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
		s.logger.Warnf("miniapp feed init_data invalid: %v", err)
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
	sinceTS := parseFeedCursorTS(body.SinceTS)
	beforeTS := parseFeedCursorTS(body.BeforeTS)
	items, err := s.bot.PackFeedForViewer(parsed.User.ID, parsed, body.InitData, sinceTS, beforeTS)
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
	// Закреплённые объявления — авторитетный текущий набор (в т.ч. при incremental polling),
	// чтобы фронт синхронизировал закреп/откреп даже для старых постов вне окна выдачи.
	// Для подгрузки вниз (before_ts) закрепы уже есть на фронте — не дёргаем повторно.
	var pinned []bot.PackFeedItem
	if beforeTS == nil {
		p, pErr := s.bot.PackFeedPinnedForViewer(parsed.User.ID, parsed, body.InitData)
		if pErr != nil {
			s.logger.Warnf("pack feed pinned: %v", pErr)
			p = nil
		}
		pinned = p
	}
	workoutTypeCounts := map[string]int{}
	if counts, cErr := s.bot.PackWorkoutTypeCountsForViewer(parsed.User.ID, parsed); cErr != nil {
		s.logger.Warnf("pack workout type counts: %v", cErr)
	} else if counts != nil {
		workoutTypeCounts = counts
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":                  true,
		"items":               items,
		"pinned":              pinned,
		"workout_type_counts": workoutTypeCounts,
	})
}

func (s *Server) handlePostFeedPin(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData      string `json:"init_data"`
		UserMessageID int64  `json:"user_message_id"`
		Pin           bool   `json:"pin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if body.UserMessageID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "missing_user_message_id")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp feed pin: invalid init: %v", err)
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
	if err := s.bot.PackFeedAdminSetPin(parsed.User.ID, parsed, body.UserMessageID, body.Pin); err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		if errors.Is(err, bot.ErrPackFeedForbidden) {
			s.jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedParentNotFound) {
			s.jsonErr(w, http.StatusNotFound, "not_found")
			return
		}
		s.logger.Errorf("feed pin: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "feed_pin_error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (s *Server) handlePostFeedPollVote(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData      string `json:"init_data"`
		UserMessageID int64  `json:"user_message_id"`
		OptionIndex   int    `json:"option_index"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if body.UserMessageID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "missing_user_message_id")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp feed poll vote invalid init: %v", err)
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
	if err := s.bot.PackFeedPollVote(parsed.User.ID, parsed, body.UserMessageID, body.OptionIndex); err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		if errors.Is(err, bot.ErrPackFeedForbidden) {
			s.jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
		if errors.Is(err, bot.ErrPackFeedPollNotFound) {
			s.jsonErr(w, http.StatusNotFound, "not_found")
			return
		}
		if errors.Is(err, bot.ErrPackFeedPollInvalidOption) {
			s.jsonErr(w, http.StatusBadRequest, "invalid_option")
			return
		}
		s.logger.Errorf("pack feed poll vote: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "poll_vote_error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (s *Server) handleGetUserAvatar(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		http.Error(w, "server_unavailable", http.StatusServiceUnavailable)
		return
	}
	initData := strings.TrimSpace(r.URL.Query().Get("init_data"))
	if initData == "" {
		http.Error(w, "missing_init_data", http.StatusBadRequest)
		return
	}
	uidStr := strings.TrimSpace(r.URL.Query().Get("user_id"))
	subjectUID, err := strconv.ParseInt(uidStr, 10, 64)
	if err != nil || subjectUID <= 0 {
		http.Error(w, "invalid_user_id", http.StatusBadRequest)
		return
	}
	if err := s.validateInit(initData); err != nil {
		s.logger.Warnf("miniapp user-avatar init_data invalid: %v", err)
		http.Error(w, "invalid_init_data", http.StatusUnauthorized)
		return
	}
	parsed, err := s.parseInit(initData)
	if err != nil || parsed.User.ID == 0 {
		http.Error(w, "parse_init_data", http.StatusBadRequest)
		return
	}
	aerr := s.bot.WritePackMemberAvatarHTTP(w, r, parsed.User.ID, subjectUID, parsed)
	if aerr == nil {
		return
	}
	if errors.Is(aerr, bot.ErrPackFeedForbidden) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if errors.Is(aerr, bot.ErrMiniAppChatMismatch) {
		http.Error(w, "chat_mismatch", http.StatusConflict)
		return
	}
	if errors.Is(aerr, bot.ErrPackMemberAvatarNotFound) {
		http.NotFound(w, r)
		return
	}
	s.logger.Warnf("miniapp user-avatar user=%d subject=%d: %v", parsed.User.ID, subjectUID, aerr)
	http.Error(w, "avatar_error", http.StatusInternalServerError)
}

func (s *Server) handlePostFeedTrainingReact(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData      string `json:"init_data"`
		UserMessageID int64  `json:"user_message_id"`
		Emoji         string `json:"emoji"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if body.UserMessageID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "missing_user_message_id")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp feed training react: invalid init: %v", err)
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
	if err := s.bot.PackTrainingFeedReact(parsed.User.ID, parsed, body.UserMessageID, body.Emoji); err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedSocialForbidden) {
			s.jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedInvalidEmoji) {
			s.jsonErr(w, http.StatusBadRequest, "invalid_emoji")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedParentNotFound) {
			s.jsonErr(w, http.StatusNotFound, "not_found")
			return
		}
		s.logger.Errorf("feed training react: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "react_error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (s *Server) handlePostFeedTrainingThread(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}

	// Комментарий может прийти JSON'ом (только текст) или multipart'ом (текст + фото).
	var (
		initDataRaw   string
		userMessageID int64
		text          string
		replyToID     int64
		photoURL      string
		postAs        string
	)
	isMultipart := strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data")
	if isMultipart {
		if err := r.ParseMultipartForm(maxWorkoutPhotoBytes + 65536); err != nil {
			s.jsonErr(w, http.StatusBadRequest, "invalid_multipart")
			return
		}
		initDataRaw = strings.TrimSpace(r.FormValue("init_data"))
		text = r.FormValue("text")
		if v, perr := strconv.ParseInt(strings.TrimSpace(r.FormValue("user_message_id")), 10, 64); perr == nil {
			userMessageID = v
		}
		if rid := strings.TrimSpace(r.FormValue("reply_to_id")); rid != "" {
			if v, perr := strconv.ParseInt(rid, 10, 64); perr == nil {
				replyToID = v
			}
		}
		postAs = strings.TrimSpace(r.FormValue("post_as"))
		if postAs == "" {
			// legacy: as_admin=1 раньше означало голос Лео.
			switch strings.TrimSpace(r.FormValue("as_admin")) {
			case "1", "true", "yes":
				postAs = "leo"
			}
		}
	} else {
		var body struct {
			InitData      string `json:"init_data"`
			UserMessageID int64  `json:"user_message_id"`
			Text          string `json:"text"`
			ReplyToID     int64  `json:"reply_to_id"`
			PostAs        string `json:"post_as"`
			AsAdmin       bool   `json:"as_admin"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			s.jsonErr(w, http.StatusBadRequest, "invalid_json")
			return
		}
		initDataRaw = body.InitData
		userMessageID = body.UserMessageID
		text = body.Text
		replyToID = body.ReplyToID
		postAs = strings.TrimSpace(body.PostAs)
		if postAs == "" && body.AsAdmin {
			postAs = "leo"
		}
	}

	if initDataRaw == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if userMessageID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "missing_user_message_id")
		return
	}
	if err := s.validateInit(initDataRaw); err != nil {
		s.logger.Warnf("miniapp feed training thread: invalid init: %v", err)
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(initDataRaw)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return
	}

	// Фото загружаем в хранилище ПОСЛЕ валидации initData (не тратим запись на анонимов).
	if isMultipart && r.MultipartForm != nil {
		if fs := r.MultipartForm.File["photo"]; len(fs) > 0 {
			file, ferr := fs[0].Open()
			if ferr != nil {
				s.jsonErr(w, http.StatusBadRequest, "photo_open_error")
				return
			}
			defer file.Close()
			url, ok := s.storeUploadedPhoto(w, r, file)
			if !ok {
				return // storeUploadedPhoto уже записал ошибку
			}
			photoURL = url
		}
	}

	if err := s.bot.PackTrainingFeedThreadPost(parsed.User.ID, parsed, userMessageID, text, replyToID, photoURL, postAs); err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedSocialForbidden) {
			s.jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedThreadEmpty) {
			s.jsonErr(w, http.StatusBadRequest, "empty_text")
			return
		}
		if s.jsonModerationErr(w, err) {
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedParentNotFound) {
			s.jsonErr(w, http.StatusNotFound, "not_found")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedThreadInvalidReply) {
			s.jsonErr(w, http.StatusBadRequest, "invalid_reply")
			return
		}
		s.logger.Errorf("feed training thread: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "thread_error")
		return
	}
	replies, rerr := s.bot.PackFeedThreadRepliesForViewer(parsed.User.ID, userMessageID, initDataRaw)
	if rerr != nil {
		s.logger.Warnf("feed training thread: list after insert: %v", rerr)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	out := map[string]any{"ok": true}
	if rerr == nil {
		out["thread"] = replies
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) handlePostFeedTrainingThreadDelete(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData      string `json:"init_data"`
		ThreadReplyID int64  `json:"thread_reply_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if body.ThreadReplyID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "missing_thread_reply_id")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp feed training thread delete: invalid init: %v", err)
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
	parentID, err := s.bot.PackTrainingFeedThreadDelete(parsed.User.ID, parsed, body.ThreadReplyID)
	if err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedSocialForbidden) {
			s.jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedThreadDeleteNotFound) {
			s.jsonErr(w, http.StatusNotFound, "not_found")
			return
		}
		s.logger.Errorf("feed training thread delete: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "thread_delete_error")
		return
	}
	replies, rerr := s.bot.PackFeedThreadRepliesForViewer(parsed.User.ID, parentID, body.InitData)
	if rerr != nil {
		s.logger.Warnf("feed training thread delete: list after delete: %v", rerr)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	out := map[string]any{"ok": true}
	if rerr == nil {
		out["thread"] = replies
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) handlePostFeedTrainingThreadEdit(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData      string `json:"init_data"`
		ThreadReplyID int64  `json:"thread_reply_id"`
		Text          string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if body.ThreadReplyID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "missing_thread_reply_id")
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" {
		s.jsonErr(w, http.StatusBadRequest, "empty_text")
		return
	}
	if utf8.RuneCountInString(text) > 500 {
		s.jsonErr(w, http.StatusBadRequest, "text_too_long")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp feed training thread edit: invalid init: %v", err)
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
	parentID, err := s.bot.PackTrainingFeedThreadEdit(parsed.User.ID, parsed, body.ThreadReplyID, text)
	if err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedSocialForbidden) {
			s.jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedThreadEmpty) {
			s.jsonErr(w, http.StatusBadRequest, "empty_text")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedThreadDeleteNotFound) {
			s.jsonErr(w, http.StatusNotFound, "not_found")
			return
		}
		if s.jsonModerationErr(w, err) {
			return
		}
		s.logger.Errorf("feed training thread edit: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "thread_edit_error")
		return
	}
	replies, rerr := s.bot.PackFeedThreadRepliesForViewer(parsed.User.ID, parentID, body.InitData)
	if rerr != nil {
		s.logger.Warnf("feed training thread edit: list after edit: %v", rerr)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	out := map[string]any{"ok": true}
	if rerr == nil {
		out["thread"] = replies
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) handlePostFeedEdit(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData      string `json:"init_data"`
		UserMessageID int64  `json:"user_message_id"`
		Text          string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if body.UserMessageID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "missing_user_message_id")
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
		s.logger.Warnf("miniapp feed edit: invalid init: %v", err)
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
	if err := s.bot.PackFeedPostEdit(parsed.User.ID, parsed, body.UserMessageID, text); err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedSocialForbidden) {
			s.jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedThreadEmpty) {
			s.jsonErr(w, http.StatusBadRequest, "empty_text")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedParentNotFound) {
			s.jsonErr(w, http.StatusNotFound, "not_found")
			return
		}
		if s.jsonModerationErr(w, err) {
			return
		}
		s.logger.Errorf("feed edit: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "feed_edit_error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (s *Server) handlePostFeedDelete(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData      string `json:"init_data"`
		UserMessageID int64  `json:"user_message_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if body.UserMessageID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "missing_user_message_id")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp feed delete: invalid init: %v", err)
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
	deletedPhotoURL, err := s.bot.PackFeedAdminDeletePost(parsed.User.ID, parsed, body.UserMessageID)
	if err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		if errors.Is(err, bot.ErrPackFeedForbidden) {
			s.jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedParentNotFound) {
			s.jsonErr(w, http.StatusNotFound, "not_found")
			return
		}
		s.logger.Errorf("feed hide: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "feed_delete_error")
		return
	}
	_ = deletedPhotoURL
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (s *Server) handlePostFeedTrainingThreadLike(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData      string `json:"init_data"`
		ThreadReplyID int64  `json:"thread_reply_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if body.ThreadReplyID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "missing_thread_reply_id")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp feed training thread like: invalid init: %v", err)
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
	parentID, err := s.bot.PackTrainingFeedThreadLikeToggle(parsed.User.ID, parsed, body.ThreadReplyID)
	if err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedSocialForbidden) {
			s.jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedThreadDeleteNotFound) {
			s.jsonErr(w, http.StatusNotFound, "not_found")
			return
		}
		s.logger.Errorf("feed training thread like: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "thread_like_error")
		return
	}
	replies, rerr := s.bot.PackFeedThreadRepliesForViewer(parsed.User.ID, parentID, body.InitData)
	if rerr != nil {
		s.logger.Warnf("feed training thread like: list after toggle: %v", rerr)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	out := map[string]any{"ok": true}
	if rerr == nil {
		out["thread"] = replies
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) handlePostFeedReport(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData      string `json:"init_data"`
		UserMessageID int64  `json:"user_message_id"`
		ThreadReplyID int64  `json:"thread_reply_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if body.UserMessageID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "missing_user_message_id")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp feed report: invalid init: %v", err)
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
	if err := s.bot.PackFeedReport(parsed.User.ID, parsed, body.UserMessageID, body.ThreadReplyID); err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedSocialForbidden) {
			s.jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedParentNotFound) || errors.Is(err, bot.ErrTrainingFeedThreadDeleteNotFound) {
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
		s.logger.Errorf("feed report: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "report_error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}
