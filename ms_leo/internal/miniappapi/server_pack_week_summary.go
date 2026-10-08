package miniappapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"leo-bot/internal/bot"
)

// Итоги недели стаи: модалка для участников общего зачёта прошлой недели.

// packWeekSummaryAuth — общая проверка запроса: подпись Telegram, пользователь, стая.
// ok=false — ответ с ошибкой уже записан.
func (s *Server) packWeekSummaryAuth(w http.ResponseWriter, initData string) (userID, packID int64, ok bool) {
	if initData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return 0, 0, false
	}
	if err := s.validateInit(initData); err != nil {
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return 0, 0, false
	}
	parsed, err := s.parseInit(initData)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return 0, 0, false
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return 0, 0, false
	}
	if err := s.bot.AssertMiniAppPackChatAligns(parsed); err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return 0, 0, false
		}
		s.jsonErr(w, http.StatusInternalServerError, "assert_chat_error")
		return 0, 0, false
	}
	packID = s.bot.MonetizedChatID()
	if packID == 0 {
		s.jsonErr(w, http.StatusServiceUnavailable, "pack_not_configured")
		return 0, 0, false
	}
	return parsed.User.ID, packID, true
}

// handlePostPackWeekSummary — итоги прошлой недели, если участник их ещё не видел; иначе summary=null.
func (s *Server) handlePostPackWeekSummary(w http.ResponseWriter, r *http.Request) {
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
	userID, packID, ok := s.packWeekSummaryAuth(w, body.InitData)
	if !ok {
		return
	}
	summary, err := s.bot.GetPendingPackWeekSummaryForAPI(userID, packID, time.Now())
	if err != nil {
		s.logger.Warnf("pack week summary user=%d: %v", userID, err)
		s.jsonErr(w, http.StatusInternalServerError, "summary_failed")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "summary": summary})
}

// handlePostPackWeekSummarySeen — участник закрыл модалку: больше её не показываем.
func (s *Server) handlePostPackWeekSummarySeen(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData  string `json:"init_data"`
		WeekStart string `json:"week_start"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	userID, packID, ok := s.packWeekSummaryAuth(w, body.InitData)
	if !ok {
		return
	}
	if err := s.bot.MarkPackWeekSummarySeenForAPI(userID, packID, body.WeekStart); err != nil {
		if errors.Is(err, bot.ErrPackWeekSummaryNotFound) {
			s.jsonErr(w, http.StatusNotFound, "summary_not_found")
			return
		}
		s.logger.Warnf("pack week summary seen user=%d: %v", userID, err)
		s.jsonErr(w, http.StatusInternalServerError, "seen_failed")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}
