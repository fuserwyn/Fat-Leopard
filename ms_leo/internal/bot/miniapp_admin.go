package bot

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"leo-bot/internal/database"
	"leo-bot/internal/domain"

	initdata "github.com/telegram-mini-apps/init-data-golang"
)

var (
	ErrAdminNotFound      = errors.New("admin target not found")
	ErrAdminActionInvalid = errors.New("admin action invalid")
)

// MiniappAdminOverview — счётчики для главной админки в мини-аппе.
type MiniappAdminOverview struct {
	Users          int `json:"users"`
	UsersActive    int `json:"users_active"`
	UsersKicked    int `json:"users_kicked"`
	ReportsOpen    int `json:"reports_open"`
	SupportWaiting int `json:"support_waiting"`
	Hidden         int `json:"hidden"`
	Payments       int `json:"payments"`
	AccessPriceRub int `json:"access_price_rub"`
}

// MiniappAdminUsersPage — страница списка/поиска участников стаи.
type MiniappAdminUsersPage struct {
	Users       []MiniappAdminUserRow `json:"users"`
	Total       int                   `json:"total"`
	Offset      int                   `json:"offset"`
	Limit       int                   `json:"limit"`
	UsersActive int                   `json:"users_active"`
	UsersKicked int                   `json:"users_kicked"`
}

// MiniappAdminPaywallPrice — текущая цена доступа, которую админ может поменять.
type MiniappAdminPaywallPrice struct {
	AmountRub        int    `json:"amount_rub"`
	AmountMinor      int    `json:"amount_minor"`
	Currency         string `json:"currency"`
	IsCustom         bool   `json:"is_custom"`
	DefaultAmountRub int    `json:"default_amount_rub"`
}

// MiniappAdminUserRow — строка списка/поиска пользователей.
type MiniappAdminUserRow struct {
	UserID           int64  `json:"user_id"`
	Username         string `json:"username,omitempty"`
	DisplayName      string `json:"display_name,omitempty"`
	Cups             int    `json:"cups"`
	StreakDays       int    `json:"streak_days"`
	IsDeleted        bool   `json:"is_deleted"`
	HasActivePaywall bool   `json:"has_active_paywall"`
}

// MiniappAdminUserCard — карточка пользователя для админки мини-аппа.
type MiniappAdminUserCard struct {
	UserID                  int64  `json:"user_id"`
	Username                string `json:"username,omitempty"`
	DisplayName             string `json:"display_name,omitempty"`
	Cups                    int    `json:"cups"`
	Level                   int    `json:"level"`
	LevelName               string `json:"level_name"`
	StreakDays              int    `json:"streak_days"`
	MaxStreakDays           int    `json:"max_streak_days"`
	DaysSinceLastTraining   int    `json:"days_since_last_training"`
	LastTrainingDate        string `json:"last_training_date,omitempty"`
	InactivityRemovalAt     string `json:"inactivity_removal_at,omitempty"`
	StreakSaveAttemptsUsed  int    `json:"streak_save_attempts_used"`
	StreakSaveAttemptsMax   int    `json:"streak_save_attempts_max"`
	StreakSaveAttemptsAvail int    `json:"streak_save_attempts_avail"`
	SickLeave               string `json:"sick_leave"`
	IsDeleted               bool   `json:"is_deleted"`
	HasActivePaywall        bool   `json:"has_active_paywall"`
	UGCViolations           int    `json:"ugc_violations"`
	UGCMutedUntil           string `json:"ugc_muted_until,omitempty"`
	WorkoutsTotal           int    `json:"workouts_total"`
}

// MiniappAdminHiddenItem — скрытый контент, который можно вернуть.
type MiniappAdminHiddenItem struct {
	Kind         string `json:"kind"`
	ID           int64  `json:"id"`
	ParentID     int64  `json:"parent_id,omitempty"`
	AuthorUserID int64  `json:"author_user_id"`
	AuthorName   string `json:"author_name,omitempty"`
	Text         string `json:"text"`
	CreatedAt    string `json:"created_at"`
	Reason       string `json:"reason,omitempty"`
}

func (b *Bot) requireMiniappAdmin(viewerUserID int64, initD initdata.InitData) (int64, error) {
	if err := b.AssertMiniAppPackChatAligns(initD); err != nil {
		return 0, err
	}
	if !b.IsMiniappViewerAdmin(viewerUserID) {
		return 0, ErrPackFeedForbidden
	}
	packID := b.adminPackChatID()
	if packID == 0 {
		return 0, fmt.Errorf("pack not configured")
	}
	return packID, nil
}

func (b *Bot) MiniappAdminOverview(viewerUserID int64, initD initdata.InitData) (MiniappAdminOverview, error) {
	var out MiniappAdminOverview
	packID, err := b.requireMiniappAdmin(viewerUserID, initD)
	if err != nil {
		return out, err
	}
	if counts, e := b.db.CountPackUsersBreakdownForAdmin(packID); e == nil {
		out.Users = counts.Total
		out.UsersActive = counts.Active
		out.UsersKicked = counts.Kicked
	} else {
		return out, e
	}
	if n, e := b.db.CountOpenMiniappFeedReports(packID); e == nil {
		out.ReportsOpen = n
	} else {
		return out, e
	}
	if n, e := b.db.CountMiniappSupportNeedingReply(packID); e == nil {
		out.SupportWaiting = n
	} else {
		return out, e
	}
	if n, e := b.db.CountHiddenModerationItems(packID); e == nil {
		out.Hidden = n
	} else {
		return out, e
	}
	if n, e := b.db.CountMoneyPaymentsForAdmin(packID); e == nil {
		out.Payments = n
	} else {
		return out, e
	}
	out.AccessPriceRub = b.AccessPriceRub()
	return out, nil
}

func (b *Bot) MiniappAdminPaywallPrice(viewerUserID int64, initD initdata.InitData) (MiniappAdminPaywallPrice, error) {
	var out MiniappAdminPaywallPrice
	if _, err := b.requireMiniappAdmin(viewerUserID, initD); err != nil {
		return out, err
	}
	def := b.defaultPaywallAmountMinor()
	override := b.packPaywallOverrideMinor()
	minor := def
	if override > 0 {
		minor = override
	}
	out = MiniappAdminPaywallPrice{
		AmountRub:        minor / 100,
		AmountMinor:      minor,
		Currency:         "RUB",
		IsCustom:         override > 0,
		DefaultAmountRub: def / 100,
	}
	return out, nil
}

func (b *Bot) MiniappAdminSetPaywallPrice(viewerUserID int64, initD initdata.InitData, amountRub int, reset bool) error {
	packID, err := b.requireMiniappAdmin(viewerUserID, initD)
	if err != nil {
		return err
	}
	if reset {
		return b.db.ClearPackPaywallAmountMinor(packID)
	}
	minor, err := parsePaywallPriceRub(amountRub)
	if err != nil {
		return err
	}
	return b.db.SetPackPaywallAmountMinor(packID, minor, viewerUserID)
}

func (b *Bot) MiniappAdminSupportInbox(viewerUserID int64, initD initdata.InitData) ([]*domain.MiniappSupportConversation, error) {
	packID, err := b.requireMiniappAdmin(viewerUserID, initD)
	if err != nil {
		return nil, err
	}
	return b.db.ListMiniappSupportConversations(packID, 40)
}

func (b *Bot) MiniappAdminSupportThread(viewerUserID int64, initD initdata.InitData, targetUserID int64) ([]*domain.MiniappSupportChatMessage, error) {
	if _, err := b.requireMiniappAdmin(viewerUserID, initD); err != nil {
		return nil, err
	}
	if targetUserID <= 0 {
		return nil, ErrAdminActionInvalid
	}
	return b.AdminSupportChatHistory(targetUserID)
}

func (b *Bot) MiniappAdminSupportReply(viewerUserID int64, initD initdata.InitData, targetUserID int64, text, photoURL string) error {
	if _, err := b.requireMiniappAdmin(viewerUserID, initD); err != nil {
		return err
	}
	if targetUserID <= 0 {
		return ErrAdminActionInvalid
	}
	t := strings.TrimSpace(text)
	photoURL = strings.TrimSpace(photoURL)
	if t == "" && photoURL == "" {
		return ErrAdminActionInvalid
	}
	return b.AdminSupportReply(targetUserID, t, photoURL)
}

func (b *Bot) MiniappAdminReports(viewerUserID int64, initD initdata.InitData) ([]*domain.MiniappFeedReport, error) {
	packID, err := b.requireMiniappAdmin(viewerUserID, initD)
	if err != nil {
		return nil, err
	}
	return b.db.ListOpenMiniappFeedReports(packID, 40)
}

func (b *Bot) MiniappAdminReportAction(viewerUserID int64, initD initdata.InitData, reportID int64, action string) error {
	packID, err := b.requireMiniappAdmin(viewerUserID, initD)
	if err != nil {
		return err
	}
	if reportID <= 0 {
		return ErrAdminActionInvalid
	}
	switch strings.TrimSpace(action) {
	case "dismiss":
		ok, derr := b.db.DismissMiniappFeedReport(packID, reportID)
		if derr != nil {
			return derr
		}
		if !ok {
			return ErrAdminNotFound
		}
		b.trackReportResolved(reportID, viewerUserID, 0, "dismiss")
		return nil
	case "hide":
		return b.hideReportedContentForAdmin(viewerUserID, packID, reportID)
	default:
		return ErrAdminActionInvalid
	}
}

func (b *Bot) hideReportedContentForAdmin(adminUserID, packChatID, reportID int64) error {
	item, err := b.db.GetMiniappFeedReport(packChatID, reportID)
	if err != nil || item == nil {
		return ErrAdminNotFound
	}
	var ok bool
	switch item.TargetType {
	case "pack_group_message":
		ok, err = b.db.AdminHidePackGroupMessage(packChatID, item.UserMessageID)
	case "thread_reply":
		ok, err = b.db.AdminHideTrainingFeedThreadReply(packChatID, item.ThreadReplyID)
	default:
		ok, err = b.db.AdminHideFeedUserMessage(packChatID, item.UserMessageID, "report")
	}
	if err != nil {
		return err
	}
	if !ok {
		return ErrAdminNotFound
	}
	if item.TargetUserID > 0 {
		b.recordUGCViolation(item.TargetUserID, packChatID, false)
	}
	_, _ = b.db.DismissMiniappFeedReport(packChatID, reportID)
	b.trackReportResolved(reportID, adminUserID, item.TargetUserID, "hide")
	return nil
}

func (b *Bot) MiniappAdminHidden(viewerUserID int64, initD initdata.InitData) ([]MiniappAdminHiddenItem, error) {
	packID, err := b.requireMiniappAdmin(viewerUserID, initD)
	if err != nil {
		return nil, err
	}
	raw, err := b.db.ListHiddenModerationItems(packID, 40)
	if err != nil {
		return nil, err
	}
	out := make([]MiniappAdminHiddenItem, 0, len(raw))
	for _, item := range raw {
		out = append(out, MiniappAdminHiddenItem{
			Kind:         item.Kind,
			ID:           item.ID,
			ParentID:     item.ParentID,
			AuthorUserID: item.AuthorUserID,
			AuthorName:   item.AuthorName,
			Text:         item.Text,
			CreatedAt:    item.CreatedAt.UTC().Format(time.RFC3339),
			Reason:       item.Reason,
		})
	}
	return out, nil
}

func (b *Bot) MiniappAdminUnhide(viewerUserID int64, initD initdata.InitData, kind string, id int64) error {
	packID, err := b.requireMiniappAdmin(viewerUserID, initD)
	if err != nil {
		return err
	}
	if id <= 0 {
		return ErrAdminActionInvalid
	}
	var ok bool
	switch strings.TrimSpace(kind) {
	case "feed_post":
		ok, err = b.db.AdminUnhideFeedUserMessage(packID, id)
	case "thread_reply":
		ok, err = b.db.AdminUnhideTrainingFeedThreadReply(packID, id)
	case "pack_group_message":
		ok, err = b.db.AdminUnhidePackGroupMessage(packID, id)
	default:
		return ErrAdminActionInvalid
	}
	if err != nil {
		return err
	}
	if !ok {
		return ErrAdminNotFound
	}
	return nil
}

func (b *Bot) MiniappAdminUsers(viewerUserID int64, initD initdata.InitData, query string, offset, limit int, filter string) (MiniappAdminUsersPage, error) {
	var out MiniappAdminUsersPage
	packID, err := b.requireMiniappAdmin(viewerUserID, initD)
	if err != nil {
		return out, err
	}
	statusFilter := database.NormalizePackUserAdminFilter(filter)
	if limit <= 0 || limit > 40 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	out.Offset = offset
	out.Limit = limit

	counts, err := b.db.CountPackUsersBreakdownForAdmin(packID)
	if err != nil {
		return out, err
	}
	out.UsersActive = counts.Active
	out.UsersKicked = counts.Kicked

	q := strings.TrimSpace(query)
	if q != "" {
		hits, err := b.db.SearchPackUsersForAdmin(packID, q, limit, statusFilter)
		if err != nil {
			return out, err
		}
		out.Total = len(hits)
		out.Users = make([]MiniappAdminUserRow, 0, len(hits))
		for _, h := range hits {
			out.Users = append(out.Users, MiniappAdminUserRow{
				UserID:      h.UserID,
				Username:    h.Username,
				DisplayName: h.DisplayName,
				IsDeleted:   h.IsDeleted,
			})
		}
		return out, nil
	}

	total, err := b.db.CountPackUsersForAdminFiltered(packID, statusFilter)
	if err != nil {
		return out, err
	}
	out.Total = total
	rows, err := b.db.ListPackUsersForAdmin(packID, offset, limit, statusFilter)
	if err != nil {
		return out, err
	}
	out.Users = make([]MiniappAdminUserRow, 0, len(rows))
	for _, r := range rows {
		out.Users = append(out.Users, MiniappAdminUserRow{
			UserID:           r.UserID,
			Username:         r.Username,
			DisplayName:      r.DisplayName,
			Cups:             r.Cups,
			StreakDays:       r.StreakDays,
			IsDeleted:        r.IsDeleted,
			HasActivePaywall: r.HasActivePaywall,
		})
	}
	return out, nil
}

func (b *Bot) MiniappAdminUserCard(viewerUserID int64, initD initdata.InitData, targetUserID int64) (MiniappAdminUserCard, error) {
	var out MiniappAdminUserCard
	packID, err := b.requireMiniappAdmin(viewerUserID, initD)
	if err != nil {
		return out, err
	}
	if targetUserID <= 0 {
		return out, ErrAdminActionInvalid
	}
	ml, err := b.db.GetMessageLogAnyState(targetUserID, packID)
	if err != nil || ml == nil {
		return out, ErrAdminNotFound
	}
	maxStreak := ml.MaxStreakDays
	if maxStreak < ml.StreakDays {
		maxStreak = ml.StreakDays
	}
	if _, syncErr := b.syncAchievementsFromStreak(targetUserID, packID, maxStreak); syncErr != nil {
		b.logger.Warnf("miniapp admin sync achievements user=%d: %v", targetUserID, syncErr)
	}
	stats := b.GetMiniappProfileStatsForAPI(targetUserID, packID)
	_, displayName, _ := b.GetMiniappUserProfileJSONForAPI(targetUserID, packID)
	sick := "нет"
	if ml.HasSickLeave && !ml.HasHealthy {
		sick = "активен"
	} else if ml.SickApprovalPending {
		sick = "ожидает"
	}
	paywall := false
	if active, perr := b.db.UserHasActivePaywallAccess(targetUserID, packID); perr == nil {
		paywall = active
	}
	out = MiniappAdminUserCard{
		UserID:                  targetUserID,
		Username:                strings.TrimSpace(ml.Username),
		DisplayName:             strings.TrimSpace(displayName),
		Cups:                    stats.XP,
		Level:                   stats.Level,
		LevelName:               stats.LevelName,
		StreakDays:              stats.StreakDays,
		MaxStreakDays:           stats.MaxStreakDays,
		DaysSinceLastTraining:   stats.DaysSinceLastTraining,
		LastTrainingDate:        stats.LastTrainingDate,
		InactivityRemovalAt:     b.GetMiniappInactivityRemovalDeadlineRFC3339(targetUserID, packID),
		StreakSaveAttemptsUsed:  stats.StreakSaveAttemptsUsed,
		StreakSaveAttemptsMax:   stats.StreakSaveAttemptsMax,
		StreakSaveAttemptsAvail: stats.StreakSaveAttemptsAvail,
		SickLeave:               sick,
		IsDeleted:               ml.IsDeleted,
		HasActivePaywall:        paywall,
		WorkoutsTotal:           stats.WorkoutsTotal,
	}
	if ugc, uerr := b.db.GetUGCModerationState(targetUserID, packID); uerr == nil {
		out.UGCViolations = ugc.ViolationCount
		if ugc.MutedUntil != nil && ugc.MutedUntil.After(time.Now()) {
			out.UGCMutedUntil = ugc.MutedUntil.UTC().Format(time.RFC3339)
		}
	}
	return out, nil
}

func (b *Bot) MiniappAdminUserAction(viewerUserID int64, initD initdata.InitData, targetUserID int64, action string) error {
	packID, err := b.requireMiniappAdmin(viewerUserID, initD)
	if err != nil {
		return err
	}
	if targetUserID <= 0 {
		return ErrAdminActionInvalid
	}
	if !isAllowedMiniappAdminUserAction(action) {
		return ErrAdminActionInvalid
	}
	if action == "kick" && targetUserID == viewerUserID {
		return fmt.Errorf("нельзя удалить себя из стаи из мини-аппа")
	}
	switch action {
	case "sick_set":
		if err := b.setSickLeaveForUser(targetUserID, packID); err != nil {
			return err
		}
		b.notifyUserTextByID(targetUserID, packID, "🏥 Администратор оформил тебе больничный. Таймер неактивности приостановлен — выздоравливай! Когда поправишься — «Выйти с больничного» в профиле.", "", 0)
		return nil
	case "sick_cancel":
		if err := b.cancelSickLeaveForUser(targetUserID, packID); err != nil {
			return err
		}
		b.notifyUserTextByID(targetUserID, packID, "🏥 Больничный завершён администратором. Таймер неактивности снова активен — не забудь про тренировку!", "", 0)
		return nil
	case "restore_full":
		return b.restoreUserInPack(targetUserID, true)
	case "restore_scratch":
		return b.restoreUserInPack(targetUserID, false)
	case "mute":
		return b.muteUserUGCForMiniapp(targetUserID, packID, 24)
	case "unmute":
		return b.unmuteUserUGCForMiniapp(targetUserID, packID)
	case "grant_save":
		if _, err := b.db.DecrementStreakSaveAttemptsUsed(targetUserID, packID, 1); err != nil {
			return err
		}
		return nil
	case "kick":
		return b.kickUserFromPack(targetUserID)
	default:
		return ErrAdminActionInvalid
	}
}

func isAllowedMiniappAdminUserAction(action string) bool {
	switch strings.TrimSpace(action) {
	case "sick_set", "sick_cancel", "restore_full", "restore_scratch", "mute", "unmute", "grant_save", "kick":
		return true
	default:
		return false
	}
}

func (b *Bot) muteUserUGCForMiniapp(targetUserID, packChatID int64, hours int) error {
	if hours <= 0 {
		hours = 24
	}
	if err := b.db.MuteUserUGCUntil(targetUserID, packChatID, time.Now().UTC().Add(time.Duration(hours)*time.Hour)); err != nil {
		return err
	}
	_, _ = b.db.IncrementUGCViolationCount(targetUserID, packChatID)
	b.notifyUserTextByID(targetUserID, packChatID,
		fmt.Sprintf("🔇 Публикации в Стае ограничены на %d ч. по решению модератора.", hours),
		"", 0)
	return nil
}

func (b *Bot) unmuteUserUGCForMiniapp(targetUserID, packChatID int64) error {
	muted, err := b.db.IsUserUGCMuted(targetUserID, packChatID)
	if err != nil {
		return err
	}
	if !muted {
		return fmt.Errorf("пользователь не в UGC-мьюте")
	}
	if err := b.db.UnmuteUserUGC(targetUserID, packChatID); err != nil {
		return err
	}
	b.notifyUserTextByID(targetUserID, packChatID,
		"🔊 Ограничения на публикации в Стае сняты. Можно снова писать в ленте, чате и заметках.",
		"", 0)
	return nil
}

func (b *Bot) kickUserFromPack(targetUserID int64) error {
	packChatID := b.adminPackChatID()
	ml, err := b.db.GetMessageLogAnyState(targetUserID, packChatID)
	if err != nil || ml == nil {
		return ErrAdminNotFound
	}
	if ml.IsDeleted {
		return fmt.Errorf("пользователь уже удалён из стаи")
	}
	username := ml.Username
	if username == "" {
		username = fmt.Sprintf("User%d", targetUserID)
	}
	b.removeUser(targetUserID, packChatID, username)
	return nil
}

func (b *Bot) MiniappAdminPublishPost(viewerUserID int64, initD initdata.InitData, author, text string) error {
	if _, err := b.requireMiniappAdmin(viewerUserID, initD); err != nil {
		return err
	}
	t := strings.TrimSpace(text)
	if t == "" {
		return ErrAdminActionInvalid
	}
	return b.saveAdminCustomPackFeed(viewerUserID, normalizeAdminPostAuthor(author), t)
}

func normalizeAdminPostAuthor(author string) string {
	if strings.TrimSpace(author) == adminPostAuthorLeo {
		return adminPostAuthorLeo
	}
	return adminPostAuthorAdmin
}
