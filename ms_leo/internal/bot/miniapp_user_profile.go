package bot

import (
	"database/sql"
	"errors"
	"math"
	"strings"
	"time"

	"leo-bot/internal/database"
	"leo-bot/internal/game/leopardmoney"
	"leo-bot/internal/utils"
)

// Лимиты полей мини-апп — профиль.
const (
	miniappProfileNameMax     = 64
	miniappProfileAgeMax      = 120
	miniappProfileTzOffsetMin = -12
	miniappProfileTzOffsetMax = 12
)

// LeoUserGenderForTrainingFeed — m/f/пусто: сначала явный мини-апп, затем training_state, без догадок по имени.
func (b *Bot) LeoUserGenderForTrainingFeed(userID int64, logGender string) string {
	g := strings.TrimSpace(strings.ToLower(logGender))
	if b != nil && b.config != nil && b.config.MonetizedChatID != 0 && b.db != nil {
		if p, err := b.db.GetMiniappUserProfile(userID, b.config.MonetizedChatID); err == nil && p != nil {
			pg := strings.TrimSpace(strings.ToLower(p.Gender))
			if pg == "m" || pg == "f" {
				return pg
			}
		}
	}
	if g == "m" || g == "f" {
		return g
	}
	return ""
}

// LeoUserProfileForFeedPrompt — подсказки к ИИ (имя, возраст).
func (b *Bot) LeoUserProfileForFeedPrompt(userID int64) (displayName string, age *int) {
	if b == nil || b.db == nil || b.config == nil || b.config.MonetizedChatID == 0 {
		return "", nil
	}
	p, err := b.db.GetMiniappUserProfile(userID, b.config.MonetizedChatID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", nil
	}
	if p == nil {
		return "", nil
	}
	dn := strings.TrimSpace(p.DisplayName)
	if len([]rune(dn)) > miniappProfileNameMax {
		dn = string([]rune(dn)[:miniappProfileNameMax])
	}
	if p.AgeYears.Valid {
		a := int(p.AgeYears.Int64)
		if a > 0 && a <= miniappProfileAgeMax {
			age = &a
		}
	}
	return dn, age
}

// MonetizedChatID — id чата стаи (MONETIZED_CHAT_ID); для мини-апп API.
func (b *Bot) MonetizedChatID() int64 {
	if b == nil || b.config == nil {
		return 0
	}
	return b.config.MonetizedChatID
}

// SaveMiniappUserProfileFromMiniapp пишет miniapp_user_profile и дублирует gender в training_state.
func (b *Bot) SaveMiniappUserProfileFromMiniapp(userID, packChatID int64, gender, displayName string, age *int) error {
	if b == nil || b.db == nil {
		return nil
	}
	g := strings.TrimSpace(strings.ToLower(gender))
	if g != "m" && g != "f" && g != "" {
		g = ""
	}
	dn := strings.TrimSpace(displayName)
	if len([]rune(dn)) > miniappProfileNameMax {
		dn = string([]rune(dn)[:miniappProfileNameMax])
	}
	var n sql.NullInt64
	existing, errG := b.db.GetMiniappUserProfile(userID, packChatID)
	if age == nil {
		if errG == nil && existing != nil && existing.AgeYears.Valid {
			n = existing.AgeYears
		}
	} else {
		if *age == 0 {
			// явный сброс
			n = sql.NullInt64{Valid: false}
		} else if *age > 0 && *age <= miniappProfileAgeMax {
			n = sql.NullInt64{Int64: int64(*age), Valid: true}
		}
	}
	theme := ""
	if errG == nil && existing != nil {
		theme = strings.TrimSpace(existing.Theme)
	}
	prof := &database.MiniappUserProfile{
		UserID:      userID,
		PackChatID:  packChatID,
		Gender:      g,
		DisplayName: dn,
		AgeYears:    n,
		Theme:       theme,
	}
	if err := b.db.UpsertMiniappUserProfile(prof); err != nil {
		return err
	}
	_ = b.db.PatchTrainingStateGenderIfExists(userID, packChatID, g)
	return nil
}

// GetTimezoneOffsetForAPI — текущий TZ-offset (часы относительно МСК) пользователя из training_state.
// 0 если строки нет — соответствует «по умолчанию МСК».
func (b *Bot) GetTimezoneOffsetForAPI(userID, packChatID int64) int {
	if b == nil || b.db == nil || packChatID == 0 {
		return 0
	}
	off, err := b.db.GetTimezoneOffsetFromTrainingState(userID, packChatID)
	if err != nil {
		b.logger.Errorf("miniapp tz offset load user=%d pack=%d: %v", userID, packChatID, err)
		return 0
	}
	if off < miniappProfileTzOffsetMin || off > miniappProfileTzOffsetMax {
		return 0
	}
	return off
}

// SaveTimezoneOffsetFromMiniapp — сохраняет смещение часового пояса (-12..+12) в training_state.
// Если онбординга ещё не было — UPDATE просто ничего не сделает; пользователь увидит выбор «МСК+0» по умолчанию.
func (b *Bot) SaveTimezoneOffsetFromMiniapp(userID, packChatID int64, offset int) error {
	if b == nil || b.db == nil {
		return nil
	}
	if offset < miniappProfileTzOffsetMin || offset > miniappProfileTzOffsetMax {
		return errors.New("invalid timezone offset")
	}
	return b.db.PatchTrainingStateTimezoneOffsetIfExists(userID, packChatID, offset)
}

// GetMiniappUserProfileJSONForAPI — DTO GET (мини-апп, иначе gender из training_state).
func (b *Bot) GetMiniappUserProfileJSONForAPI(userID, packChatID int64) (gender, displayName string, age *int) {
	if b == nil || b.db == nil || packChatID == 0 {
		return "", "", nil
	}
	if p, err := b.db.GetMiniappUserProfile(userID, packChatID); err == nil && p != nil {
		gender = strings.TrimSpace(strings.ToLower(p.Gender))
		displayName = strings.TrimSpace(p.DisplayName)
		if p.AgeYears.Valid {
			a := int(p.AgeYears.Int64)
			if a > 0 {
				age = &a
			}
		}
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", "", nil
	}
	if gender != "m" && gender != "f" {
		if ml, e := b.db.GetMessageLog(userID, packChatID); e == nil && ml != nil {
			g2 := strings.TrimSpace(strings.ToLower(ml.Gender))
			if g2 == "m" || g2 == "f" {
				gender = g2
			}
		}
	}
	return gender, displayName, age
}

const miniappLeopardThemeMinLevel = 5
const miniappWildThemeMinStreak = 365

// MiniappThemeAccess — уровень, стрик и админ-флаг для разблокировки тем.
type MiniappThemeAccess struct {
	Level         int
	StreakDays    int
	MaxStreakDays int
	IsAdmin       bool
}

func isMiniappThemeName(t string) bool {
	return t == "light" || t == "dark" || t == "leopard" || t == "wild"
}

func canUseWildMiniappTheme(access MiniappThemeAccess) bool {
	if access.IsAdmin {
		return true
	}
	streak := access.StreakDays
	if access.MaxStreakDays > streak {
		streak = access.MaxStreakDays
	}
	return streak >= miniappWildThemeMinStreak
}

// NormalizeMiniappTheme — light/dark/leopard; розовая только с уровня Лев.
func NormalizeMiniappTheme(raw string, level int) (string, bool) {
	return NormalizeMiniappThemeAccess(raw, MiniappThemeAccess{Level: level})
}

// NormalizeMiniappThemeAccess — light/dark/leopard/wild с проверкой стрика и админа.
func NormalizeMiniappThemeAccess(raw string, access MiniappThemeAccess) (string, bool) {
	t := strings.TrimSpace(strings.ToLower(raw))
	if !isMiniappThemeName(t) {
		return "", false
	}
	if t == "leopard" && access.Level < miniappLeopardThemeMinLevel {
		return "dark", true
	}
	if t == "wild" && !canUseWildMiniappTheme(access) {
		return "dark", true
	}
	return t, true
}

// GetMiniappThemeForAPI — сохранённая тема мини-аппа; пусто — клиент берёт кэш.
func (b *Bot) GetMiniappThemeForAPI(userID, packChatID int64) string {
	if b == nil || b.db == nil || packChatID == 0 || userID == 0 {
		return ""
	}
	p, err := b.db.GetMiniappUserProfile(userID, packChatID)
	if err != nil || p == nil {
		return ""
	}
	t := strings.TrimSpace(strings.ToLower(p.Theme))
	if !isMiniappThemeName(t) {
		return ""
	}
	return t
}

// SaveMiniappThemeFromMiniapp пишет только тему; пол и имя не трогает.
func (b *Bot) SaveMiniappThemeFromMiniapp(userID, packChatID int64, theme string) error {
	if b == nil || b.db == nil {
		return nil
	}
	stats := b.GetMiniappProfileStatsForAPI(userID, packChatID)
	normalized, ok := NormalizeMiniappThemeAccess(theme, MiniappThemeAccess{
		Level:         stats.Level,
		StreakDays:    stats.StreakDays,
		MaxStreakDays: stats.MaxStreakDays,
		IsAdmin:       b.IsMiniappViewerAdmin(userID),
	})
	if !ok {
		return errors.New("invalid theme")
	}
	return b.db.UpsertMiniappTheme(userID, packChatID, normalized)
}

type MiniappProfileStats struct {
	XP                      int
	Level                   int
	LevelName               string
	StreakDays              int
	MaxStreakDays           int
	AchievementCount        int
	AchievementsMax         int
	WorkoutsTotal           int
	MaxCupsPerTraining      int // максимум кубков за одну тренировку/активность
	WorkoutsWeek            int
	DaysSinceLastTraining   int    // -1, если тренировок ещё не было
	LastTrainingDate        string // YYYY-MM-DD в локальном TZ; пусто, если не было отчётов
	StreakSaveAttemptsUsed  int    // сколько попыток спасения стрика использовано (lifetime)
	StreakSaveAttemptsMax   int    // кап = min(level, 6); +1 за каждый новый уровень
	StreakSaveAttemptsAvail int    // = max - used (не меньше 0)
	DaysInPack              int    // дней в стае с первого входа (включая день входа), локальный TZ
}

// StreakSaveAttemptsMaxForLevel возвращает кап попыток спасения для данного уровня (1-based).
// Правило: на старте 1 попытка, +1 за каждый новый уровень, максимум 6 (уровень Слон).
func StreakSaveAttemptsMaxForLevel(level int) int {
	if level < 1 {
		level = 1
	}
	if level > leopardmoney.MaxLevel {
		return leopardmoney.MaxLevel
	}
	return level
}

// GetMiniappProfileStatsForAPI — срез метрик профиля для мини-аппа (XP/стрик/рекорд/ачивки/тренировки).
func (b *Bot) GetMiniappProfileStatsForAPI(userID, packChatID int64) MiniappProfileStats {
	out := MiniappProfileStats{
		AchievementsMax:       leopardmoney.MaxAchievements,
		DaysSinceLastTraining: -1,
	}
	if b == nil || b.db == nil || userID == 0 || packChatID == 0 {
		return out
	}
	applyLog := func(chatID int64) {
		ml, err := b.db.GetMessageLog(userID, chatID)
		if err != nil || ml == nil {
			return
		}
		out.StreakDays = int(math.Max(float64(out.StreakDays), float64(ml.StreakDays)))
		maxStreak := ml.MaxStreakDays
		if maxStreak < ml.StreakDays {
			maxStreak = ml.StreakDays
		}
		out.MaxStreakDays = int(math.Max(float64(out.MaxStreakDays), float64(maxStreak)))
		out.AchievementCount = int(math.Max(float64(out.AchievementCount), float64(ml.AchievementCount)))

		// Дней без тренировок: берём last_training_date (YYYY-MM-DD в локальном TZ) и считаем
		// дельту до «сегодня» того же TZ. Если тренировок ещё не было — оставляем -1.
		if ml.LastTrainingDate != nil {
			ltd := strings.TrimSpace(*ml.LastTrainingDate)
			if ltd != "" {
				if last, parseErr := time.Parse("2006-01-02", ltd); parseErr == nil {
					if out.LastTrainingDate == "" {
						out.LastTrainingDate = ltd
					} else if prev, prevErr := time.Parse("2006-01-02", out.LastTrainingDate); prevErr == nil && last.After(prev) {
						out.LastTrainingDate = ltd
					}
				}
				if last, parseErr := time.Parse("2006-01-02", ltd); parseErr == nil {
					today, todayErr := time.Parse("2006-01-02", b.getUserLocalDate(ml.TimezoneOffsetFromMoscow))
					if todayErr == nil {
						days := int(math.Floor(today.Sub(last).Hours()/24 + 0.5))
						if days < 0 {
							days = 0
						}
						if out.DaysSinceLastTraining < 0 || days < out.DaysSinceLastTraining {
							out.DaysSinceLastTraining = days
						}
					}
				}
			}
		}
	}
	// Primary source: pack-row. Fallback/migration source: private-row (chat_id=userID).
	applyLog(packChatID)
	applyLog(userID)

	// В мини-аппе поле `xp` в JSON — накопленные кубки (шкала 1).
	// Берём только pack-row: private-row — bookkeeping и может содержать устаревшие значения.
	if err := b.db.SyncPrivateRowCupsFromPack(userID, packChatID); err != nil {
		b.logger.Warnf("sync private cups user=%d pack=%d: %v", userID, packChatID, err)
	}
	if ml, err := b.db.GetMessageLog(userID, packChatID); err == nil && ml != nil {
		out.XP = ml.CupsEarned
	} else if ml, err := b.db.GetMessageLog(userID, userID); err == nil && ml != nil {
		out.XP = ml.CupsEarned
	}
	if out.XP < 0 {
		out.XP = 0
	}

	today := time.Now().UTC()
	weekAgo := today.AddDate(0, 0, -6)
	countAndMerge := func(chatID int64) {
		total, err := b.db.CountTrainingSessionsInDateRange(userID, chatID, "2000-01-01", today.Format("2006-01-02"))
		if err == nil {
			out.WorkoutsTotal = int(math.Max(float64(out.WorkoutsTotal), float64(total)))
		}
		week, err := b.db.CountTrainingSessionsInDateRange(userID, chatID, weekAgo.Format("2006-01-02"), today.Format("2006-01-02"))
		if err == nil {
			out.WorkoutsWeek = int(math.Max(float64(out.WorkoutsWeek), float64(week)))
		}
		maxCups, err := b.db.MaxTrainingSessionCups(userID, chatID)
		if err == nil {
			out.MaxCupsPerTraining = int(math.Max(float64(out.MaxCupsPerTraining), float64(maxCups)))
		}
	}
	countAndMerge(packChatID)
	countAndMerge(userID)

	// Уровень и попытки спасения стрика — только от накопленных кубков (pack-row).
	out.Level = leopardmoney.LevelFromTotalCups(out.XP)
	out.LevelName = leopardmoney.LevelName(out.Level)
	out.StreakSaveAttemptsMax = StreakSaveAttemptsMaxForLevel(out.Level)
	if used, err := b.db.GetStreakSaveAttemptsUsed(userID, packChatID); err == nil {
		out.StreakSaveAttemptsUsed = used
	}
	avail := out.StreakSaveAttemptsMax - out.StreakSaveAttemptsUsed
	if avail < 0 {
		avail = 0
	}
	out.StreakSaveAttemptsAvail = avail
	storedStreak := out.StreakDays
	// Больничный замораживает стрик (kick-таймер на паузе — стрик по аналогии): дни болезни
	// не считаем «пропущенными», иначе стрик сгорает прямо во время больничного.
	if packLog, err := b.db.GetMessageLog(userID, packChatID); err == nil {
		out.DaysSinceLastTraining = b.sickAdjustedDaysSince(packLog, out.LastTrainingDate, out.DaysSinceLastTraining)
	}
	out.StreakDays = EffectiveStreakDays(storedStreak, out.DaysSinceLastTraining)
	if storedStreak > 0 && out.StreakDays == 0 && out.DaysSinceLastTraining >= 3 {
		b.reconcileExpiredStreakInDB(userID, packChatID)
	}
	b.reconcileAchievementsWithStreak(&out, userID, packChatID)
	tzOffset := b.GetTimezoneOffsetForAPI(userID, packChatID)
	out.DaysInPack = b.packMembershipDaysForAPI(userID, packChatID, tzOffset)
	return out
}

// packMembershipDaysForAPI — число календарных дней в стае с первого входа (день входа = 1).
// Якорь — PackMiniappHistorySinceUTC (текущее членство: оплата или GREATEST(created_at, returned_at)).
func (b *Bot) packMembershipDaysForAPI(userID, packChatID int64, tzOffset int) int {
	if b == nil || b.db == nil || userID == 0 || packChatID == 0 {
		return 0
	}
	anchor, err := b.db.PackMiniappHistorySinceUTC(userID, packChatID, b.paywallActive())
	if err != nil {
		b.logger.Warnf("pack membership days user=%d pack=%d: %v", userID, packChatID, err)
		return 0
	}
	moscowNow := utils.GetMoscowTime()
	joinLocal := anchor.In(moscowNow.Location()).Add(time.Duration(tzOffset) * time.Hour)
	joinDateStr := joinLocal.Format("2006-01-02")
	todayStr := b.getUserLocalDate(tzOffset)
	joinDate, err1 := time.Parse("2006-01-02", joinDateStr)
	today, err2 := time.Parse("2006-01-02", todayStr)
	if err1 != nil || err2 != nil {
		return 0
	}
	days := int(math.Floor(today.Sub(joinDate).Hours()/24+0.5)) + 1
	if days < 1 {
		return 1
	}
	return days
}

const miniappProfileChartDaysDefault = 90

// MiniappWorkoutDayPoint — одна точка графика тренировок (дата → число сессий).
type MiniappWorkoutDayPoint struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

// GetMiniappWorkoutsByDayForAPI — тренировки по дням за последние days календарных дней
// (локальный TZ пользователя, включая сегодня). session_date из training_sessions.
func (b *Bot) GetMiniappWorkoutsByDayForAPI(userID, packChatID int64, days int) []MiniappWorkoutDayPoint {
	if b == nil || b.db == nil || userID == 0 || packChatID == 0 {
		return nil
	}
	if days <= 0 || days > 365 {
		days = miniappProfileChartDaysDefault
	}
	tzOffset := b.GetTimezoneOffsetForAPI(userID, packChatID)
	endStr := b.getUserLocalDate(tzOffset)
	end, err := time.Parse("2006-01-02", endStr)
	if err != nil {
		end = time.Now().UTC()
		endStr = end.Format("2006-01-02")
	}
	start := end.AddDate(0, 0, -(days - 1))
	startStr := start.Format("2006-01-02")

	merged := map[string]int{}
	merge := func(chatID int64) {
		rows, err := b.db.GetTrainingCountsByDay(userID, chatID, startStr, endStr)
		if err != nil {
			b.logger.Warnf("workouts by day user=%d chat=%d: %v", userID, chatID, err)
			return
		}
		for _, r := range rows {
			if r.Count > merged[r.Date] {
				merged[r.Date] = r.Count
			}
		}
	}
	merge(packChatID)
	merge(userID)

	out := make([]MiniappWorkoutDayPoint, 0, days)
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		ds := d.Format("2006-01-02")
		out = append(out, MiniappWorkoutDayPoint{Date: ds, Count: merged[ds]})
	}
	return out
}

// reconcileExpiredStreakInDB обнуляет streak_days в БД, если стрик уже сгорел по календарю.
func (b *Bot) reconcileExpiredStreakInDB(userID, packChatID int64) {
	if b == nil || b.db == nil {
		return
	}
	resetIfExpired := func(chatID int64) {
		ml, err := b.db.GetMessageLog(userID, chatID)
		if err != nil || ml == nil || ml.StreakDays <= 0 {
			return
		}
		if ml.LastTrainingDate == nil {
			return
		}
		ltd := strings.TrimSpace(*ml.LastTrainingDate)
		if ltd == "" {
			return
		}
		last, parseErr := time.Parse("2006-01-02", ltd)
		if parseErr != nil {
			return
		}
		today, todayErr := time.Parse("2006-01-02", b.getUserLocalDate(ml.TimezoneOffsetFromMoscow))
		if todayErr != nil {
			return
		}
		days := int(math.Floor(today.Sub(last).Hours()/24 + 0.5))
		// Больничный замораживает стрик — не жжём его дни болезни (см. sickAdjustedDaysSince).
		days = b.sickAdjustedDaysSince(ml, ltd, days)
		if days < 3 {
			return
		}
		if err := b.db.ResetStreakDays(userID, chatID); err != nil {
			b.logger.Warnf("reconcile expired streak user=%d chat=%d: %v", userID, chatID, err)
		}
	}
	resetIfExpired(packChatID)
	resetIfExpired(userID)
}

// reconcileAchievementsWithStreak — в профиле ачивки должны соответствовать рекорду стрика (пороги 7, 14, 30…).
func (b *Bot) reconcileAchievementsWithStreak(out *MiniappProfileStats, userID, packChatID int64) {
	if b == nil || b.db == nil || out == nil || userID == 0 || packChatID == 0 {
		return
	}
	want := leopardmoney.AchievementsCountForStreak(out.MaxStreakDays)
	if want <= out.AchievementCount {
		return
	}
	out.AchievementCount = want
	last := leopardmoney.LastAchievementMilestoneForStreak(out.MaxStreakDays)
	if err := b.db.SetAchievementsForUserScope(userID, packChatID, want, last); err != nil {
		b.logger.Warnf("reconcile achievements user=%d pack=%d max_streak=%d: %v", userID, packChatID, out.MaxStreakDays, err)
	}
}

// StreakSaveWindowError — почему нельзя спасти стрик при текущих daysSince / avail.
// Пусто — окно открыто (ровно 1 пропущенный день: daysSince == 2).
func StreakSaveWindowError(daysSinceLastTraining, attemptsAvail int) string {
	if attemptsAvail <= 0 {
		return "no_attempts"
	}
	if daysSinceLastTraining < 0 {
		return "no_training_history"
	}
	if daysSinceLastTraining < 2 {
		return "not_needed"
	}
	if daysSinceLastTraining > 2 {
		return "too_late"
	}
	return ""
}

// restoreStreakForSave — какое значение streak_days удерживать/восстанавливать при спасении.
// Если в БД стрик ещё не обнулён — оставляем его; иначе берём рекорд (max).
func restoreStreakForSave(storedStreak, maxStreak int) int {
	if storedStreak > 0 {
		return storedStreak
	}
	if maxStreak > 0 {
		return maxStreak
	}
	return 0
}

// UseStreakSaveAttemptForAPI использует одну попытку спасения стрика:
// один пропущенный день (daysSinceLastTraining == 2) «закрывается» сдвигом last_training_date.
// Возвращает обновлённый used и восстановленный streak_days или ошибку.
func (b *Bot) UseStreakSaveAttemptForAPI(userID, packChatID int64) (used, max, avail, restoredStreak int, err error) {
	if b == nil || b.db == nil || userID == 0 || packChatID == 0 {
		return 0, 0, 0, 0, errors.New("not_configured")
	}
	stats := b.GetMiniappProfileStatsForAPI(userID, packChatID)
	if reason := StreakSaveWindowError(stats.DaysSinceLastTraining, stats.StreakSaveAttemptsAvail); reason != "" {
		avail = stats.StreakSaveAttemptsAvail
		if reason == "no_attempts" {
			avail = 0
		}
		return stats.StreakSaveAttemptsUsed, stats.StreakSaveAttemptsMax, avail, stats.StreakDays, errors.New(reason)
	}

	ml, mlErr := b.db.GetMessageLog(userID, packChatID)
	if mlErr != nil || ml == nil {
		return stats.StreakSaveAttemptsUsed, stats.StreakSaveAttemptsMax, stats.StreakSaveAttemptsAvail, stats.StreakDays, errors.New("user_not_found")
	}
	maxStreak := ml.MaxStreakDays
	if maxStreak < ml.StreakDays {
		maxStreak = ml.StreakDays
	}
	restoreStreak := restoreStreakForSave(ml.StreakDays, maxStreak)
	if restoreStreak <= 0 {
		return stats.StreakSaveAttemptsUsed, stats.StreakSaveAttemptsMax, stats.StreakSaveAttemptsAvail, stats.StreakDays, errors.New("nothing_to_save")
	}

	yesterday := b.getUserLocalNow(ml.TimezoneOffsetFromMoscow).AddDate(0, 0, -1).Format("2006-01-02")
	if err := b.db.ApplyStreakSaveForUserScope(userID, packChatID, yesterday, restoreStreak); err != nil {
		return stats.StreakSaveAttemptsUsed, stats.StreakSaveAttemptsMax, stats.StreakSaveAttemptsAvail, stats.StreakDays, err
	}

	newUsed, err := b.db.IncrementStreakSaveAttemptsUsed(userID, packChatID)
	if err != nil {
		return stats.StreakSaveAttemptsUsed, stats.StreakSaveAttemptsMax, stats.StreakSaveAttemptsAvail, stats.StreakDays, err
	}
	avail = stats.StreakSaveAttemptsMax - newUsed
	if avail < 0 {
		avail = 0
	}
	b.trackStreakAttemptUsed(userID, avail) // §5: попытка спасла стрик
	restoredStreak = b.GetMiniappProfileStatsForAPI(userID, packChatID).StreakDays
	return newUsed, stats.StreakSaveAttemptsMax, avail, restoredStreak, nil
}

// GetMiniappInactivityRemovalDeadlineRFC3339 — когда возможен кик за неактивность (если не будет отчёта), Europe/Moscow в RFC3339. Пусто — нет таймера, освобождён от кика или нет данных.
func (b *Bot) GetMiniappInactivityRemovalDeadlineRFC3339(userID, packChatID int64) string {
	if b == nil || b.db == nil || userID == 0 || packChatID == 0 {
		return ""
	}
	ml, err := b.db.GetMessageLog(userID, packChatID)
	if err != nil || ml == nil {
		return ""
	}
	if ml.IsExemptFromDeletion || ml.IsDeleted {
		return ""
	}
	now := utils.GetMoscowTime()
	dl, ok := inactivityKickDeadline(ml, now)
	if !ok {
		return ""
	}
	loc := utils.GetMoscowTime().Location()
	return dl.In(loc).Format(time.RFC3339)
}

// SyncMiniappTelegramPhotoFromInit — сохраняет URL аватарки из WebApp initData (user.photo_url) для ленты/чата.
// Пустая строка не затирает уже сохранённое (см. UpsertMiniappTelegramPhotoURL).
func (b *Bot) SyncMiniappTelegramPhotoFromInit(userID, packChatID int64, photoURL string) {
	if b == nil || b.db == nil {
		return
	}
	photoURL = strings.TrimSpace(photoURL)
	if photoURL == "" {
		return
	}
	if len(photoURL) > 768 {
		return
	}
	if err := b.db.UpsertMiniappTelegramPhotoURL(userID, packChatID, photoURL); err != nil {
		b.logger.Warnf("miniapp sync telegram photo user=%d: %v", userID, err)
	}
}
