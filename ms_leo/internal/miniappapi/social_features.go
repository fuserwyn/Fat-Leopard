package miniappapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"leo-bot/internal/bot"

	initdata "github.com/telegram-mini-apps/init-data-golang"
)

// authMiniapp — общий boilerplate: валидация initData, парсинг, проверка совпадения стаи.
// При любой ошибке пишет JSON-ответ и возвращает ok=false.
func (s *Server) authMiniapp(w http.ResponseWriter, initDataRaw string) (initdata.InitData, bool) {
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return initdata.InitData{}, false
	}
	if initDataRaw == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return initdata.InitData{}, false
	}
	// Десктопное приложение шлёт сюда токен сессии: подписанного initData в
	// обычном окне взять неоткуда (bot/desktop_auth.go). Отличаем по префиксу.
	if strings.HasPrefix(initDataRaw, bot.DesktopTokenPrefix) {
		parsed, ok := s.authDesktopToken(w, initDataRaw)
		if !ok {
			return initdata.InitData{}, false
		}
		return parsed, true
	}
	if err := s.validateInit(initDataRaw); err != nil {
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return initdata.InitData{}, false
	}
	parsed, err := s.parseInit(initDataRaw)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return initdata.InitData{}, false
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return initdata.InitData{}, false
	}
	if err := s.bot.AssertMiniAppPackChatAligns(parsed); err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return initdata.InitData{}, false
		}
		s.jsonErr(w, http.StatusInternalServerError, "assert_chat_error")
		return initdata.InitData{}, false
	}
	if s.bot.MonetizedChatID() == 0 {
		s.jsonErr(w, http.StatusServiceUnavailable, "pack_not_configured")
		return initdata.InitData{}, false
	}
	return parsed, true
}

// writeSocialErr — маппинг доменных ошибок соц-фич в HTTP-коды.
func (s *Server) writeSocialErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, bot.ErrTrainingFeedSocialForbidden):
		s.jsonErr(w, http.StatusForbidden, "forbidden")
	case errors.Is(err, bot.ErrTrainingFeedParentNotFound):
		s.jsonErr(w, http.StatusBadRequest, "not_found")
	default:
		s.logger.Errorf("social feature error: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "internal_error")
	}
}

func (s *Server) handlePostReminderLoad(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	var body struct {
		InitData string `json:"init_data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	view, err := s.bot.GetWorkoutReminderForViewer(parsed.User.ID, parsed)
	if err != nil {
		s.writeSocialErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":          true,
		"enabled":     view.Enabled,
		"remind_hour": view.RemindHour,
	})
}

func (s *Server) handlePostReminderSave(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	var body struct {
		InitData   string `json:"init_data"`
		Enabled    bool   `json:"enabled"`
		RemindHour int    `json:"remind_hour"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	if body.RemindHour < 0 || body.RemindHour > 23 {
		s.jsonErr(w, http.StatusBadRequest, "invalid_hour")
		return
	}
	if err := s.bot.SaveWorkoutReminderForViewer(parsed.User.ID, parsed, body.Enabled, body.RemindHour); err != nil {
		s.writeSocialErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":          true,
		"enabled":     body.Enabled,
		"remind_hour": body.RemindHour,
	})
}

func (s *Server) handlePostWisdomSubLoad(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	var body struct {
		InitData string `json:"init_data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	view, err := s.bot.GetWisdomSubscriptionForViewer(parsed.User.ID, parsed)
	if err != nil {
		s.writeSocialErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":          true,
		"enabled":     view.Enabled,
		"remind_hour": view.RemindHour,
	})
}

func (s *Server) handlePostWisdomSubSave(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	var body struct {
		InitData   string `json:"init_data"`
		Enabled    bool   `json:"enabled"`
		RemindHour int    `json:"remind_hour"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	if body.RemindHour < 0 || body.RemindHour > 23 {
		s.jsonErr(w, http.StatusBadRequest, "invalid_hour")
		return
	}
	if err := s.bot.SaveWisdomSubscriptionForViewer(parsed.User.ID, parsed, body.Enabled, body.RemindHour); err != nil {
		s.writeSocialErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":          true,
		"enabled":     body.Enabled,
		"remind_hour": body.RemindHour,
	})
}

func (s *Server) handlePostLikeNotificationsLoad(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	var body struct {
		InitData string `json:"init_data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	view, err := s.bot.GetLikeNotificationForViewer(parsed.User.ID, parsed)
	if err != nil {
		s.writeSocialErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"enabled": view.Enabled,
	})
}

func (s *Server) handlePostLikeNotificationsSave(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	var body struct {
		InitData string `json:"init_data"`
		Enabled  bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	if err := s.bot.SaveLikeNotificationForViewer(parsed.User.ID, parsed, body.Enabled); err != nil {
		s.writeSocialErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"enabled": body.Enabled,
	})
}

func (s *Server) handlePostContactJoinNotificationsLoad(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	var body struct {
		InitData string `json:"init_data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	view, err := s.bot.GetContactJoinNotificationForViewer(parsed.User.ID, parsed)
	if err != nil {
		s.writeSocialErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":             true,
		"enabled":        view.Enabled,
		"contacts_count": view.ContactsCount,
	})
}

func (s *Server) handlePostContactJoinNotificationsSave(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	var body struct {
		InitData string `json:"init_data"`
		Enabled  bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	if err := s.bot.SaveContactJoinNotificationForViewer(parsed.User.ID, parsed, body.Enabled); err != nil {
		s.writeSocialErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"enabled": body.Enabled,
	})
}

func (s *Server) handlePostFriendsList(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	var body struct {
		InitData string `json:"init_data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	members, err := s.bot.ListPackMembersForViewer(parsed.User.ID, parsed)
	if err != nil {
		s.writeSocialErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"members": members,
	})
}

func (s *Server) handlePostFriendsFollow(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	var body struct {
		InitData string `json:"init_data"`
		TargetID int64  `json:"target_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	if err := s.bot.FollowFriend(parsed.User.ID, parsed, body.TargetID); err != nil {
		s.writeSocialErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "following": true})
}

func (s *Server) handlePostFriendsNotify(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	var body struct {
		InitData string `json:"init_data"`
		TargetID int64  `json:"target_id"`
		Enabled  bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	if err := s.bot.SetFriendWorkoutNotify(parsed.User.ID, parsed, body.TargetID, body.Enabled); err != nil {
		s.writeSocialErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "notify_workouts": body.Enabled})
}

// handlePostFriendsNotifyAll — глобальный тумблер «Тренировки друзей» из секции
// «Уведомления» профиля: вкл/выкл DM сразу по всем подпискам viewer'а.
func (s *Server) handlePostFriendsNotifyAll(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	var body struct {
		InitData string `json:"init_data"`
		Enabled  bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	if err := s.bot.SetAllFriendsWorkoutNotify(parsed.User.ID, parsed, body.Enabled); err != nil {
		s.writeSocialErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "notify_workouts": body.Enabled})
}

func (s *Server) handlePostFriendsUnfollow(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	var body struct {
		InitData string `json:"init_data"`
		TargetID int64  `json:"target_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	if err := s.bot.UnfollowFriend(parsed.User.ID, parsed, body.TargetID); err != nil {
		s.writeSocialErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "following": false})
}

// authDesktopToken — сессия десктопного приложения. Собираем такой же
// initdata.InitData, как у мини-аппа, только без чата: проверки на совпадение
// чата стаи пропускают пустой Chat.ID, а всё остальное работает как обычно.
func (s *Server) authDesktopToken(w http.ResponseWriter, token string) (initdata.InitData, bool) {
	if s.bot == nil {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return initdata.InitData{}, false
	}
	userID, err := s.bot.DesktopSessionOwner(token)
	if err != nil {
		s.jsonErr(w, http.StatusInternalServerError, "desktop_session_error")
		return initdata.InitData{}, false
	}
	if userID == 0 {
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return initdata.InitData{}, false
	}
	return initdata.InitData{User: initdata.User{ID: userID}}, true
}
