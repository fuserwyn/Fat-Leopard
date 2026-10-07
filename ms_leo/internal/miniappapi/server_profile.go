package miniappapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"leo-bot/internal/bot"
)

// Здоровье, онбординг, профиль, история кубков, спасение стрика.

func (s *Server) handlePostHealthStatus(w http.ResponseWriter, r *http.Request) {
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
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"on_sick": s.bot.MiniappHealthStatus(parsed.User.ID),
	})
}

// handlePostOnboardingEnsure — идемпотентный хендшейк мини-аппа: на первом открытии
// после оплаты стартуем таймер неактивности и пишем pack_join/pack_rejoin в ленту стаи.
func (s *Server) handlePostOnboardingEnsure(w http.ResponseWriter, r *http.Request) {
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
	res, err := s.bot.EnsureMiniAppOnboarding(parsed)
	if err != nil {
		s.logger.Errorf("miniapp onboarding ensure user=%d: %v", parsed.User.ID, err)
		s.jsonErr(w, http.StatusInternalServerError, "onboarding_failed")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":             true,
		"in_pack":        res.InPack,
		"deleted":        res.Deleted,
		"access_state":   res.AccessState,
		"just_onboarded": res.JustOnboarded,
		"is_rejoin":      res.IsRejoin,
	})
}

func (s *Server) handlePostProfileLoad(w http.ResponseWriter, r *http.Request) {
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
	packID := s.bot.MonetizedChatID()
	if packID == 0 {
		s.jsonErr(w, http.StatusServiceUnavailable, "pack_not_configured")
		return
	}
	g, d, a := s.bot.GetMiniappUserProfileJSONForAPI(parsed.User.ID, packID)
	tz := s.bot.GetTimezoneOffsetForAPI(parsed.User.ID, packID)
	stats := s.bot.GetMiniappProfileStatsForAPI(parsed.User.ID, packID)
	workoutsByDay := s.bot.GetMiniappWorkoutsByDayForAPI(parsed.User.ID, packID, 90)
	suggestedWorkoutTypes := s.bot.GetSuggestedWorkoutTypesForAPI(parsed.User.ID, packID, tz, stats.DaysSinceLastTraining)
	kickAt := s.bot.GetMiniappInactivityRemovalDeadlineRFC3339(parsed.User.ID, packID)
	packWeekly := s.bot.GetMiniappPackWeeklyProgressForAPI(packID)
	theme := s.bot.GetMiniappThemeForAPI(parsed.User.ID, packID)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	out := map[string]any{
		"ok":                         true,
		"gender":                     g,
		"display_name":               d,
		"theme":                      theme,
		"timezone_offset":            tz,
		"xp":                         stats.XP,
		"level":                      stats.Level,
		"level_name":                 stats.LevelName,
		"streak_days":                stats.StreakDays,
		"max_streak_days":            stats.MaxStreakDays,
		"achievement_count":          stats.AchievementCount,
		"achievements_max":           stats.AchievementsMax,
		"workouts_total":             stats.WorkoutsTotal,
		"max_cups_per_training":      stats.MaxCupsPerTraining,
		"workouts_week":              stats.WorkoutsWeek,
		"days_since_last_training":   stats.DaysSinceLastTraining,
		"last_training_date":         stats.LastTrainingDate,
		"streak_save_attempts_used":  stats.StreakSaveAttemptsUsed,
		"streak_save_attempts_max":   stats.StreakSaveAttemptsMax,
		"streak_save_attempts_avail": stats.StreakSaveAttemptsAvail,
		"days_in_pack":               stats.DaysInPack,
		"is_admin":                   s.bot.IsMiniappViewerAdmin(parsed.User.ID),
		"access_price_rub":           s.bot.AccessPriceRub(),
		"workouts_by_day":            workoutsByDay,
		"suggested_workout_types":    suggestedWorkoutTypes,
		"pack_workouts_week":         packWeekly.WorkoutsWeek,
		"pack_workouts_goal":         packWeekly.Goal,
		"pack_week_start":            packWeekly.WeekStart,
		"pack_week_end":              packWeekly.WeekEnd,
		"pack_goal_reached":          packWeekly.GoalReached,
		"pack_bonus_theme_active":    packWeekly.BonusActive,
	}
	if kickAt != "" {
		out["inactivity_removal_at"] = kickAt
	}
	if packWeekly.BonusActiveUntil != "" {
		out["pack_bonus_theme_active_until"] = packWeekly.BonusActiveUntil
	}
	if a != nil {
		out["age"] = *a
	} else {
		out["age"] = nil
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) handlePostProfileCupsHistory(w http.ResponseWriter, r *http.Request) {
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
	packID := s.bot.MonetizedChatID()
	if packID == 0 {
		s.jsonErr(w, http.StatusServiceUnavailable, "pack_not_configured")
		return
	}
	history := s.bot.GetMiniappCupsHistoryForAPI(parsed.User.ID, packID)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":          true,
		"limit":       history.Limit,
		"workouts":    history.Workouts,
		"pack_weekly": history.PackWeekly,
	})
}

func (s *Server) handlePostProfileSave(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData       string  `json:"init_data"`
		Gender         *string `json:"gender"`
		DisplayName    *string `json:"display_name"`
		Age            *int    `json:"age"`
		TimezoneOffset *int    `json:"timezone_offset"`
		Theme          *string `json:"theme"`
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
	if err := s.bot.AssertMiniAppPackChatAligns(parsed); err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		s.jsonErr(w, http.StatusInternalServerError, "assert_chat_error")
		return
	}
	packID := s.bot.MonetizedChatID()
	if packID == 0 {
		s.jsonErr(w, http.StatusServiceUnavailable, "pack_not_configured")
		return
	}
	if body.Gender != nil || body.DisplayName != nil || body.Age != nil {
		gv := ""
		if body.Gender != nil {
			gv = *body.Gender
		}
		dn := ""
		if body.DisplayName != nil {
			dn = *body.DisplayName
		}
		if err := s.bot.SaveMiniappUserProfileFromMiniapp(parsed.User.ID, packID, gv, dn, body.Age); err != nil {
			s.logger.Errorf("miniapp profile save: %v", err)
			s.jsonErr(w, http.StatusInternalServerError, "save_failed")
			return
		}
	}
	if body.Theme != nil {
		if err := s.bot.SaveMiniappThemeFromMiniapp(parsed.User.ID, packID, *body.Theme); err != nil {
			s.logger.Errorf("miniapp theme save: %v", err)
			s.jsonErr(w, http.StatusBadRequest, "invalid_theme")
			return
		}
	}
	if body.TimezoneOffset != nil {
		if err := s.bot.SaveTimezoneOffsetFromMiniapp(parsed.User.ID, packID, *body.TimezoneOffset); err != nil {
			s.logger.Errorf("miniapp tz save: %v", err)
			s.jsonErr(w, http.StatusBadRequest, "invalid_timezone_offset")
			return
		}
	}
	g, d, a := s.bot.GetMiniappUserProfileJSONForAPI(parsed.User.ID, packID)
	tz := s.bot.GetTimezoneOffsetForAPI(parsed.User.ID, packID)
	stats := s.bot.GetMiniappProfileStatsForAPI(parsed.User.ID, packID)
	kickAt := s.bot.GetMiniappInactivityRemovalDeadlineRFC3339(parsed.User.ID, packID)
	packWeekly := s.bot.GetMiniappPackWeeklyProgressForAPI(packID)
	theme := s.bot.GetMiniappThemeForAPI(parsed.User.ID, packID)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	out := map[string]any{
		"ok":                         true,
		"gender":                     g,
		"display_name":               d,
		"theme":                      theme,
		"timezone_offset":            tz,
		"xp":                         stats.XP,
		"level":                      stats.Level,
		"level_name":                 stats.LevelName,
		"streak_days":                stats.StreakDays,
		"max_streak_days":            stats.MaxStreakDays,
		"achievement_count":          stats.AchievementCount,
		"achievements_max":           stats.AchievementsMax,
		"workouts_total":             stats.WorkoutsTotal,
		"max_cups_per_training":      stats.MaxCupsPerTraining,
		"workouts_week":              stats.WorkoutsWeek,
		"days_since_last_training":   stats.DaysSinceLastTraining,
		"last_training_date":         stats.LastTrainingDate,
		"streak_save_attempts_used":  stats.StreakSaveAttemptsUsed,
		"streak_save_attempts_max":   stats.StreakSaveAttemptsMax,
		"streak_save_attempts_avail": stats.StreakSaveAttemptsAvail,
		"days_in_pack":               stats.DaysInPack,
		"pack_workouts_week":         packWeekly.WorkoutsWeek,
		"pack_workouts_goal":         packWeekly.Goal,
		"pack_week_start":            packWeekly.WeekStart,
		"pack_week_end":              packWeekly.WeekEnd,
		"pack_goal_reached":          packWeekly.GoalReached,
		"pack_bonus_theme_active":    packWeekly.BonusActive,
	}
	if kickAt != "" {
		out["inactivity_removal_at"] = kickAt
	}
	if packWeekly.BonusActiveUntil != "" {
		out["pack_bonus_theme_active_until"] = packWeekly.BonusActiveUntil
	}
	if a != nil {
		out["age"] = *a
	} else {
		out["age"] = nil
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) handlePostStreakSaveUse(w http.ResponseWriter, r *http.Request) {
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
	packID := s.bot.MonetizedChatID()
	if packID == 0 {
		s.jsonErr(w, http.StatusServiceUnavailable, "pack_not_configured")
		return
	}
	used, max, avail, restoredStreak, err := s.bot.UseStreakSaveAttemptForAPI(parsed.User.ID, packID)
	if err != nil {
		switch err.Error() {
		case "no_attempts", "not_needed", "too_late", "nothing_to_save", "no_training_history":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":                      err.Error(),
				"streak_days":                restoredStreak,
				"streak_save_attempts_used":  used,
				"streak_save_attempts_max":   max,
				"streak_save_attempts_avail": avail,
			})
			return
		}
		s.logger.Errorf("streak save use user=%d pack=%d: %v", parsed.User.ID, packID, err)
		s.jsonErr(w, http.StatusInternalServerError, "save_failed")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":                         true,
		"streak_days":                restoredStreak,
		"streak_save_attempts_used":  used,
		"streak_save_attempts_max":   max,
		"streak_save_attempts_avail": avail,
	})
}
