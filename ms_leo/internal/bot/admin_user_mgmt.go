package bot

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"leo-bot/internal/database"
	"leo-bot/internal/game/leopardmoney"
	"leo-bot/internal/utils"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// syncAchievementsFromStreak выставляет achievement_count по порогам рекорда стрика на pack-row и private-row.
func (b *Bot) syncAchievementsFromStreak(userID, packChatID int64, maxStreakDays int) (int, error) {
	if b == nil || b.db == nil || userID == 0 || packChatID == 0 {
		return 0, nil
	}
	want := leopardmoney.AchievementsCountForStreak(maxStreakDays)
	last := leopardmoney.LastAchievementMilestoneForStreak(maxStreakDays)
	if err := b.db.SetAchievementsForUserScope(userID, packChatID, want, last); err != nil {
		return 0, err
	}
	return want, nil
}

func (b *Bot) adminPackChatID() int64 {
	if b != nil && b.config != nil && b.config.MonetizedChatID != 0 {
		return b.config.MonetizedChatID
	}
	return 0
}

func (b *Bot) startAdminUserMgmt(adminID int64) {
	b.adminSessionsMutex.Lock()
	b.adminSessions[adminID] = &adminSession{Mode: "user_mgmt", Step: "await_user_id"}
	b.adminSessionsMutex.Unlock()
}

func (b *Bot) startAdminUserAction(adminID, targetUserID int64, mode, step string) {
	b.adminSessionsMutex.Lock()
	b.adminSessions[adminID] = &adminSession{
		Mode:         mode,
		Step:         step,
		TargetUserID: targetUserID,
	}
	b.adminSessionsMutex.Unlock()
}

func (b *Bot) showAdminUserCard(chatID, targetUserID int64) {
	packChatID := b.adminPackChatID()
	if packChatID == 0 {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ MonetizedChatID not configured."))
		return
	}

	ml, err := b.db.GetMessageLogAnyState(targetUserID, packChatID)
	if err != nil || ml == nil {
		b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ User %d not found in the pack.", targetUserID)))
		return
	}
	maxStreak := ml.MaxStreakDays
	if maxStreak < ml.StreakDays {
		maxStreak = ml.StreakDays
	}
	if _, syncErr := b.syncAchievementsFromStreak(targetUserID, packChatID, maxStreak); syncErr != nil {
		b.logger.Warnf("admin sync achievements user=%d: %v", targetUserID, syncErr)
	} else {
		ml, _ = b.db.GetMessageLogAnyState(targetUserID, packChatID)
	}

	stats := b.GetMiniappProfileStatsForAPI(targetUserID, packChatID)
	sick := "none"
	if ml.HasSickLeave && !ml.HasHealthy {
		sick = "active"
	} else if ml.SickApprovalPending {
		sick = "pending"
	}
	deleted := "no"
	if ml.IsDeleted {
		deleted = "yes"
	}
	paywall := "no active access"
	if active, err := b.db.UserHasActivePaywallAccess(targetUserID, packChatID); err == nil && active {
		paywall = "paid ✅"
	}

	var body strings.Builder
	body.WriteString("<b>👤 User</b>\n\n")
	body.WriteString(fmt.Sprintf("ID: <code>%d</code>\n", targetUserID))
	body.WriteString(fmt.Sprintf("Name: %s\n", adminEscapeHTML(adminSupportTitle(ml.Username, targetUserID))))
	body.WriteString(fmt.Sprintf("Cups: %d\n", stats.XP))
	body.WriteString(fmt.Sprintf("Level: %d · %s\n", stats.Level, adminEscapeHTML(stats.LevelName)))
	body.WriteString(fmt.Sprintf("Streak: %d (record %d)\n", stats.StreakDays, stats.MaxStreakDays))
	if stats.DaysSinceLastTraining >= 0 {
		body.WriteString(fmt.Sprintf("Days without workout: %d\n", stats.DaysSinceLastTraining))
	} else {
		body.WriteString("Days without workout: —\n")
	}
	if removalAt := b.GetMiniappInactivityRemovalDeadlineRFC3339(targetUserID, packChatID); removalAt != "" {
		body.WriteString(fmt.Sprintf("Inactivity kick at: %s\n", adminEscapeHTML(removalAt)))
	}
	body.WriteString(fmt.Sprintf("Streak saves: %d/%d (available %d)\n", stats.StreakSaveAttemptsUsed, stats.StreakSaveAttemptsMax, stats.StreakSaveAttemptsAvail))
	body.WriteString(fmt.Sprintf("Sick leave: %s\n", adminEscapeHTML(sick)))
	body.WriteString(fmt.Sprintf("Removed from pack: %s\n", adminEscapeHTML(deleted)))
	body.WriteString(fmt.Sprintf("Access: %s\n", adminEscapeHTML(paywall)))
	ugcMuted := false
	if ugc, uerr := b.db.GetUGCModerationState(targetUserID, packChatID); uerr == nil {
		body.WriteString(fmt.Sprintf("UGC violations: %d\n", ugc.ViolationCount))
		if ugc.MutedUntil != nil && ugc.MutedUntil.After(time.Now()) {
			ugcMuted = true
			body.WriteString(fmt.Sprintf("UGC mute until: %s\n", adminEscapeHTML(ugc.MutedUntil.UTC().Format("2006-01-02 15:04 UTC"))))
		}
	}
	body.WriteString(fmt.Sprintf("Achievements: %d/%d", stats.AchievementCount, stats.AchievementsMax))
	body.WriteString(b.formatAdminUserPaymentsBlock(targetUserID, packChatID))

	rows := [][]tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("➕ Cups", "admin_user_cups_"+strconv.FormatInt(targetUserID, 10)),
			tgbotapi.NewInlineKeyboardButtonData("➖ Cups", "admin_user_cups_sub_"+strconv.FormatInt(targetUserID, 10)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🎯 Set cups", "admin_user_cups_set_"+strconv.FormatInt(targetUserID, 10)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("➕ Streak", "admin_user_streak_"+strconv.FormatInt(targetUserID, 10)),
			tgbotapi.NewInlineKeyboardButtonData("➖ Streak", "admin_user_streak_sub_"+strconv.FormatInt(targetUserID, 10)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🛟 +1 streak save", "admin_user_save_"+strconv.FormatInt(targetUserID, 10)),
			sickLeaveAdminButton(targetUserID, (ml.HasSickLeave && !ml.HasHealthy) || ml.SickApprovalPending),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⏳ Days idle", "admin_user_inactive_"+strconv.FormatInt(targetUserID, 10)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🗑 Feed", "admin_user_del_feed_"+strconv.FormatInt(targetUserID, 10)),
			tgbotapi.NewInlineKeyboardButtonData("🗑 Pack chat", "admin_user_del_chat_"+strconv.FormatInt(targetUserID, 10)),
		),
	}
	if ugcMuted {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔊 UGC unmute", "admin_user_unmute_ugc_"+strconv.FormatInt(targetUserID, 10)),
		))
	} else {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔇 UGC mute 24h", "admin_user_mute_ugc_"+strconv.FormatInt(targetUserID, 10)),
		))
	}
	if !ml.IsDeleted {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⛔ Remove from pack", "admin_user_del_"+strconv.FormatInt(targetUserID, 10)),
		))
	} else {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("♻️ Restore with progress", "admin_user_restore_full_"+strconv.FormatInt(targetUserID, 10)),
		))
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🆕 Restore from scratch", "admin_user_restore_scratch_"+strconv.FormatInt(targetUserID, 10)),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("☠️ Purge from all tables", "admin_user_purge_"+strconv.FormatInt(targetUserID, 10)),
	))
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("📋 Back to list", "admin_users_list_0"),
		tgbotapi.NewInlineKeyboardButtonData("⬅️ Admin", "admin_open"),
	))

	msg := tgbotapi.NewMessage(chatID, body.String())
	msg.ParseMode = "HTML"
	msg.DisableWebPagePreview = true
	msg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
	b.api.Send(msg)
}

func (b *Bot) handleAdminUserMgmtCallback(callback *tgbotapi.CallbackQuery) bool {
	if callback == nil || callback.Message == nil {
		return false
	}
	data := callback.Data
	chatID := callback.Message.Chat.ID
	adminID := callback.From.ID

	parseTarget := func(prefix string) (int64, bool) {
		id, err := strconv.ParseInt(strings.TrimPrefix(data, prefix), 10, 64)
		return id, err == nil && id > 0
	}

	switch {
	case data == "admin_users":
		b.startAdminUserMgmt(adminID)
		b.api.Send(tgbotapi.NewMessage(chatID, "👤 Send Telegram user_id, @username or profile name."))
		return true

	case strings.HasPrefix(data, "admin_user_pick_"):
		targetID, ok := parseTarget("admin_user_pick_")
		if ok {
			b.clearAdminFlow(adminID)
			b.showAdminUserCard(chatID, targetID)
		}
		return true

	case strings.HasPrefix(data, "admin_user_open_"):
		targetID, ok := parseTarget("admin_user_open_")
		if ok {
			b.showAdminUserCard(chatID, targetID)
		}
		return true

	case strings.HasPrefix(data, "admin_user_cups_set_"):
		targetID, ok := parseTarget("admin_user_cups_set_")
		if ok {
			b.startAdminUserAction(adminID, targetID, "user_set_cups", "await_cups_set")
			b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf(
				"🎯 Set cups for user %d?\n\nSend the exact number (e.g. 425). Replaces the current value in the database.",
				targetID)))
		}
		return true

	case strings.HasPrefix(data, "admin_user_cups_sub_"):
		targetID, ok := parseTarget("admin_user_cups_sub_")
		if ok {
			b.startAdminUserAction(adminID, targetID, "user_sub_cups", "await_amount")
			b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("➖ Remove how many cups from user %d? Send a number (0 means don't go below zero).", targetID)))
		}
		return true

	case strings.HasPrefix(data, "admin_user_cups_"):
		targetID, ok := parseTarget("admin_user_cups_")
		if ok {
			b.startAdminUserAction(adminID, targetID, "user_add_cups", "await_amount")
			b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("➕ Add how many cups to user %d? Send a number.", targetID)))
		}
		return true

	case strings.HasPrefix(data, "admin_user_streak_sub_"):
		targetID, ok := parseTarget("admin_user_streak_sub_")
		if ok {
			b.startAdminUserAction(adminID, targetID, "user_sub_streak", "await_amount")
			b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("➖ Reduce user %d streak by how many days? Send a number (0 — don't go below zero).", targetID)))
		}
		return true

	case strings.HasPrefix(data, "admin_user_streak_"):
		targetID, ok := parseTarget("admin_user_streak_")
		if ok {
			b.startAdminUserAction(adminID, targetID, "user_add_streak", "await_amount")
			b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("➕ Increase user %d streak by how many days? Send a number.", targetID)))
		}
		return true

	case strings.HasPrefix(data, "admin_user_save_"):
		targetID, ok := parseTarget("admin_user_save_")
		if ok {
			b.adminGrantStreakSaveAttempt(chatID, targetID)
		}
		return true

	case strings.HasPrefix(data, "admin_user_sick_set_"):
		targetID, ok := parseTarget("admin_user_sick_set_")
		if ok {
			b.adminActivateSickLeave(chatID, targetID)
		}
		return true

	case strings.HasPrefix(data, "admin_user_sick_"):
		targetID, ok := parseTarget("admin_user_sick_")
		if ok {
			b.adminCancelSickLeave(chatID, targetID)
		}
		return true

	case strings.HasPrefix(data, "admin_user_inactive_"):
		targetID, ok := parseTarget("admin_user_inactive_")
		if ok {
			b.startAdminUserAction(adminID, targetID, "user_set_inactive", "await_days")
			b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf(
				"⏳ How many days has user %d been inactive?\n\n"+
					"0 — workout today\n"+
					"5 — yellow warning in profile\n"+
					"6 — red, 7+ — burgundy\n\n"+
					"For a safe kick test, use /set_exempt on a test account.",
				targetID)))
		}
		return true

	case strings.HasPrefix(data, "admin_user_del_feed_yes_"):
		targetID, ok := parseTarget("admin_user_del_feed_yes_")
		if ok {
			n, err := b.db.AdminDeleteAllFeedMessagesByUser(b.adminPackChatID(), targetID)
			if err != nil {
				b.api.Send(tgbotapi.NewMessage(chatID, "❌ Error: "+err.Error()))
			} else {
				b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Removed %d feed posts for user %d.", n, targetID)))
			}
			b.showAdminUserCard(chatID, targetID)
		}
		return true

	case strings.HasPrefix(data, "admin_user_del_feed_"):
		targetID, ok := parseTarget("admin_user_del_feed_")
		if ok {
			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf(
				"🗑 Delete all feed posts for user %d?\n\nIncluding comments, reactions, and poll votes.",
				targetID,
			))
			msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("✅ Yes, delete", "admin_user_del_feed_yes_"+strconv.FormatInt(targetID, 10)),
					tgbotapi.NewInlineKeyboardButtonData("❌ Cancel", "admin_user_open_"+strconv.FormatInt(targetID, 10)),
				),
			)
			b.api.Send(msg)
		}
		return true

	case strings.HasPrefix(data, "admin_user_del_chat_yes_"):
		targetID, ok := parseTarget("admin_user_del_chat_yes_")
		if ok {
			n, err := b.db.AdminDeleteAllPackGroupMessagesByUser(b.adminPackChatID(), targetID)
			if err != nil {
				b.api.Send(tgbotapi.NewMessage(chatID, "❌ Error: "+err.Error()))
			} else {
				b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Removed %d pack chat messages for user %d.", n, targetID)))
			}
			b.showAdminUserCard(chatID, targetID)
		}
		return true

	case strings.HasPrefix(data, "admin_user_del_chat_"):
		targetID, ok := parseTarget("admin_user_del_chat_")
		if ok {
			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf(
				"🗑 Delete all pack chat messages for user %d?\n\nIncluding reactions and reports on them.",
				targetID,
			))
			msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("✅ Yes, delete", "admin_user_del_chat_yes_"+strconv.FormatInt(targetID, 10)),
					tgbotapi.NewInlineKeyboardButtonData("❌ Cancel", "admin_user_open_"+strconv.FormatInt(targetID, 10)),
				),
			)
			b.api.Send(msg)
		}
		return true

	case strings.HasPrefix(data, "admin_user_del_yes_"):
		targetID, ok := parseTarget("admin_user_del_yes_")
		if ok {
			b.adminDeleteUser(chatID, targetID)
		}
		return true

	case strings.HasPrefix(data, "admin_user_restore_full_"):
		targetID, ok := parseTarget("admin_user_restore_full_")
		if ok {
			b.adminRestoreUser(chatID, targetID, true)
		}
		return true

	case strings.HasPrefix(data, "admin_user_restore_scratch_"):
		targetID, ok := parseTarget("admin_user_restore_scratch_")
		if ok {
			b.adminRestoreUser(chatID, targetID, false)
		}
		return true

	// Полное каскадное удаление: превью → подтверждение → стирание.
	// Порядок веток важен — yes/go специфичнее общего префикса.
	case strings.HasPrefix(data, "admin_user_purge_yes_"):
		targetID, ok := parseTarget("admin_user_purge_yes_")
		if ok {
			b.executeAdminUserPurge(chatID, adminID, targetID)
		}
		return true

	case strings.HasPrefix(data, "admin_user_purge_go_"):
		targetID, ok := parseTarget("admin_user_purge_go_")
		if ok {
			b.showAdminUserPurgeConfirm(chatID, adminID, targetID)
		}
		return true

	case strings.HasPrefix(data, "admin_user_purge_"):
		targetID, ok := parseTarget("admin_user_purge_")
		if ok {
			b.showAdminUserPurgePrompt(chatID, targetID)
		}
		return true

	case strings.HasPrefix(data, "admin_user_del_"):
		targetID, ok := parseTarget("admin_user_del_")
		if ok {
			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("⛔ Remove user %d from the pack?\nCups and streak reset; mini app access is revoked.", targetID))
			msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("✅ Yes, delete", "admin_user_del_yes_"+strconv.FormatInt(targetID, 10)),
					tgbotapi.NewInlineKeyboardButtonData("❌ Cancel", "admin_user_open_"+strconv.FormatInt(targetID, 10)),
				),
			)
			b.api.Send(msg)
		}
		return true

	case strings.HasPrefix(data, "admin_feed_report_del_"):
		reportID, err := strconv.ParseInt(strings.TrimPrefix(data, "admin_feed_report_del_"), 10, 64)
		if err == nil && reportID > 0 {
			b.adminHideReportedContent(chatID, reportID)
		}
		return true

	case strings.HasPrefix(data, "admin_hidden_restore_post_"):
		id, err := strconv.ParseInt(strings.TrimPrefix(data, "admin_hidden_restore_post_"), 10, 64)
		if err == nil && id > 0 {
			b.adminRestoreHiddenContent(chatID, "feed_post", id)
		}
		return true
	case strings.HasPrefix(data, "admin_hidden_restore_thread_"):
		id, err := strconv.ParseInt(strings.TrimPrefix(data, "admin_hidden_restore_thread_"), 10, 64)
		if err == nil && id > 0 {
			b.adminRestoreHiddenContent(chatID, "thread_reply", id)
		}
		return true
	case strings.HasPrefix(data, "admin_hidden_restore_chat_"):
		id, err := strconv.ParseInt(strings.TrimPrefix(data, "admin_hidden_restore_chat_"), 10, 64)
		if err == nil && id > 0 {
			b.adminRestoreHiddenContent(chatID, "pack_group_message", id)
		}
		return true
	case data == "admin_hidden_inbox":
		b.showAdminHiddenContentInbox(chatID)
		return true

	case strings.HasPrefix(data, "admin_feed_report_hide_"):
		reportID, err := strconv.ParseInt(strings.TrimPrefix(data, "admin_feed_report_hide_"), 10, 64)
		if err == nil && reportID > 0 {
			b.adminHideReportedContent(chatID, reportID)
		}
		return true

	case strings.HasPrefix(data, "admin_feed_report_unmute_"):
		reportID, err := strconv.ParseInt(strings.TrimPrefix(data, "admin_feed_report_unmute_"), 10, 64)
		if err == nil && reportID > 0 {
			packChatID := b.adminPackChatID()
			item, err := b.db.GetMiniappFeedReport(packChatID, reportID)
			if err != nil || item == nil {
				b.api.Send(tgbotapi.NewMessage(chatID, "❌ Report not found."))
				return true
			}
			if item.TargetUserID <= 0 {
				b.api.Send(tgbotapi.NewMessage(chatID, "❌ No user to unmute."))
				return true
			}
			b.adminUnmuteUserUGC(chatID, item.TargetUserID, packChatID)
			b.showAdminFeedReport(chatID, reportID)
		}
		return true

	case strings.HasPrefix(data, "admin_feed_report_mute_yes_"):
		reportID, err := strconv.ParseInt(strings.TrimPrefix(data, "admin_feed_report_mute_yes_"), 10, 64)
		if err == nil && reportID > 0 {
			b.adminMuteReportedUser(chatID, reportID)
		}
		return true

	case strings.HasPrefix(data, "admin_feed_report_mute_"):
		reportID, err := strconv.ParseInt(strings.TrimPrefix(data, "admin_feed_report_mute_"), 10, 64)
		if err == nil && reportID > 0 {
			packChatID := b.adminPackChatID()
			item, err := b.db.GetMiniappFeedReport(packChatID, reportID)
			if err != nil || item == nil {
				b.api.Send(tgbotapi.NewMessage(chatID, "❌ Report not found."))
				return true
			}
			if item.TargetUserID <= 0 {
				b.api.Send(tgbotapi.NewMessage(chatID, "❌ No user to mute."))
				return true
			}
			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf(
				"🔇 Give user %d a 24h UGC mute for report #%d?\n\n"+
					"They won't be able to post in the feed, pack chat, or workout notes.",
				item.TargetUserID, reportID,
			))
			msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("✅ Yes, mute", "admin_feed_report_mute_yes_"+strconv.FormatInt(reportID, 10)),
					tgbotapi.NewInlineKeyboardButtonData("❌ Cancel", "admin_feed_report_"+strconv.FormatInt(reportID, 10)),
				),
			)
			b.api.Send(msg)
		}
		return true

	case strings.HasPrefix(data, "admin_user_unmute_ugc_"):
		targetID, ok := parseTarget("admin_user_unmute_ugc_")
		if ok {
			b.adminUnmuteUserUGC(chatID, targetID, b.adminPackChatID())
			b.showAdminUserCard(chatID, targetID)
		}
		return true

	case strings.HasPrefix(data, "admin_user_mute_ugc_yes_"):
		targetID, ok := parseTarget("admin_user_mute_ugc_yes_")
		if ok {
			b.adminMuteUserUGC(chatID, targetID, b.adminPackChatID(), 24)
			b.showAdminUserCard(chatID, targetID)
		}
		return true

	case strings.HasPrefix(data, "admin_user_mute_ugc_"):
		targetID, ok := parseTarget("admin_user_mute_ugc_")
		if ok {
			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf(
				"🔇 Give user %d a 24h UGC mute?\n\n"+
					"They won't be able to post in the feed, pack chat, or workout notes.",
				targetID,
			))
			msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("✅ Yes, mute", "admin_user_mute_ugc_yes_"+strconv.FormatInt(targetID, 10)),
					tgbotapi.NewInlineKeyboardButtonData("❌ Cancel", "admin_user_open_"+strconv.FormatInt(targetID, 10)),
				),
			)
			b.api.Send(msg)
		}
		return true
	}
	return false
}

func (b *Bot) handleAdminUserMgmtMessage(msg *tgbotapi.Message, session *adminSession) bool {
	if session == nil {
		return false
	}
	text := strings.TrimSpace(msg.Text)

	switch session.Step {
	case "await_user_id":
		b.clearAdminFlow(msg.From.ID)
		b.resolveAdminUserSearch(msg.Chat.ID, text)
		return true

	case "await_admin_id":
		adminID := msg.From.ID
		b.clearAdminFlow(adminID)
		b.adminAddDynamicAdmin(msg.Chat.ID, adminID, text)
		return true

	case "await_days":
		days, err := strconv.Atoi(text)
		if err != nil || days < 0 || days > 14 {
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "⚠️ Enter a number from 0 to 14."))
			return true
		}
		targetID := session.TargetUserID
		b.clearAdminFlow(msg.From.ID)
		b.adminSetDaysInactive(msg.Chat.ID, targetID, days)
		return true

	case "await_cups_set":
		amount, err := strconv.Atoi(text)
		if err != nil || amount < 0 || amount > 1000000 {
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "⚠️ Enter a number from 0 to 1000000."))
			return true
		}
		targetID := session.TargetUserID
		b.clearAdminFlow(msg.From.ID)
		packChatID := b.adminPackChatID()
		statsBefore := b.GetMiniappProfileStatsForAPI(targetID, packChatID)
		if err := b.db.SetCupsForUserScope(targetID, packChatID, amount); err != nil {
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ Error: "+err.Error()))
			return true
		}
		statsAfter := b.GetMiniappProfileStatsForAPI(targetID, packChatID)
		b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf(
			"✅ User %d cups: %d → %d.\nLevel: %d · %s.",
			targetID, statsBefore.XP, statsAfter.XP, statsAfter.Level, statsAfter.LevelName,
		)))
		b.showAdminUserCard(msg.Chat.ID, targetID)
		return true

	case "await_amount":
		amount, err := strconv.Atoi(text)
		if err != nil || amount <= 0 || amount > 100000 {
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "⚠️ Enter a positive number (up to 100000)."))
			return true
		}
		targetID := session.TargetUserID
		mode := session.Mode
		b.clearAdminFlow(msg.From.ID)
		packChatID := b.adminPackChatID()
		switch mode {
		case "user_add_cups":
			if err := b.db.AddCups(targetID, packChatID, amount); err != nil {
				b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ Error: "+err.Error()))
				return true
			}
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("✅ Added %d cups to user %d.", amount, targetID)))
		case "user_sub_cups":
			statsBefore := b.GetMiniappProfileStatsForAPI(targetID, packChatID)
			if err := b.db.SubtractCupsForUserScope(targetID, packChatID, amount); err != nil {
				b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ Error: "+err.Error()))
				return true
			}
			statsAfter := b.GetMiniappProfileStatsForAPI(targetID, packChatID)
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf(
				"✅ User %d cups: %d → %d (−%d).",
				targetID, statsBefore.XP, statsAfter.XP, statsBefore.XP-statsAfter.XP,
			)))
		case "user_add_streak":
			ml, err := b.db.GetMessageLogAnyState(targetID, packChatID)
			if err != nil || ml == nil {
				b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ User not found."))
				return true
			}
			oldStreak := ml.StreakDays
			newStreak := oldStreak + amount
			lastDate := ""
			if ml.LastTrainingDate != nil {
				lastDate = strings.TrimSpace(*ml.LastTrainingDate)
			}
			if lastDate == "" {
				lastDate = time.Now().UTC().Format("2006-01-02")
			}
			if err := b.db.UpdateStreak(targetID, packChatID, newStreak, lastDate); err != nil {
				b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ Error: "+err.Error()))
				return true
			}
			mlAfter, _ := b.db.GetMessageLogAnyState(targetID, packChatID)
			maxStreak := newStreak
			if mlAfter != nil && mlAfter.MaxStreakDays > maxStreak {
				maxStreak = mlAfter.MaxStreakDays
			}
			achCount, achErr := b.syncAchievementsFromStreak(targetID, packChatID, maxStreak)
			achNote := ""
			if achErr == nil {
				achNote = fmt.Sprintf(" Achievements: %d/%d.", achCount, leopardmoney.MaxAchievements)
			}
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("✅ User %d streak: %d → %d days.%s", targetID, oldStreak, newStreak, achNote)))
		case "user_sub_streak":
			statsBefore := b.GetMiniappProfileStatsForAPI(targetID, packChatID)
			if err := b.db.SubtractStreakDaysForUserScope(targetID, packChatID, amount); err != nil {
				b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ Error: "+err.Error()))
				return true
			}
			statsAfter := b.GetMiniappProfileStatsForAPI(targetID, packChatID)
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf(
				"✅ User %d streak: %d → %d days (record %d).",
				targetID, statsBefore.StreakDays, statsAfter.StreakDays, statsAfter.MaxStreakDays,
			)))
		}
		b.showAdminUserCard(msg.Chat.ID, targetID)
		return true

	}
	return false
}

func (b *Bot) resolveAdminUserSearch(chatID int64, query string) {
	packChatID := b.adminPackChatID()
	if packChatID == 0 {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ MonetizedChatID not configured."))
		return
	}
	query = strings.TrimSpace(query)
	if query == "" {
		b.api.Send(tgbotapi.NewMessage(chatID, "⚠️ Enter user_id, @username, or name."))
		return
	}

	hits, err := b.db.SearchPackUsersForAdmin(packChatID, query, 10, database.PackUserAdminFilterAll)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Search error: "+err.Error()))
		return
	}
	if len(hits) == 0 {
		// По числовому id даём вход в полное удаление: у юзера могло не остаться строки
		// в стае, но хвосты в других таблицах ещё лежат.
		if id, err := strconv.ParseInt(query, 10, 64); err == nil && id > 0 {
			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf(
				"❌ User %d is not in the pack.\n\nYou can check whether their data remains in other tables.", id))
			msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("☠️ Check and purge", "admin_user_purge_"+strconv.FormatInt(id, 10)),
				),
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("⬅️ Admin", "admin_open"),
				),
			)
			b.api.Send(msg)
			return
		}
		b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ User %q not found.", query)))
		return
	}
	if len(hits) == 1 {
		b.showAdminUserCard(chatID, hits[0].UserID)
		return
	}

	tbl := newAdminTable(
		[]string{"No.", "ID", "Username"},
		[]int{2, 11, 18},
	)
	rows := make([][]tgbotapi.InlineKeyboardButton, 0, 3)
	for i, hit := range hits {
		tbl.addRow(
			strconv.Itoa(i+1),
			strconv.FormatInt(hit.UserID, 10),
			adminPaywallPersonLabel(hit.Username, hit.DisplayName, hit.UserID),
		)
	}
	// Кнопки pick используют admin_user_pick_ вместо admin_user_open_
	const perRow = 5
	btnRow := make([]tgbotapi.InlineKeyboardButton, 0, perRow)
	for i, hit := range hits {
		btnRow = append(btnRow, tgbotapi.NewInlineKeyboardButtonData(
			strconv.Itoa(i+1),
			"admin_user_pick_"+strconv.FormatInt(hit.UserID, 10),
		))
		if len(btnRow) >= perRow {
			rows = append(rows, btnRow)
			btnRow = make([]tgbotapi.InlineKeyboardButton, 0, perRow)
		}
	}
	if len(btnRow) > 0 {
		rows = append(rows, btnRow)
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("⬅️ Admin", "admin_open"),
	))
	subtitle := fmt.Sprintf("Found %d for «%s» · tap No.", len(hits), query)
	kb := &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
	b.sendAdminHTMLPreTable(chatID, "🔍 User search", subtitle, tbl.render(), kb)
}

// adminSetDaysInactive — для UI-тестов: backdate last_training_date и timer_start_time.
func (b *Bot) adminSetDaysInactive(chatID, targetUserID int64, days int) {
	packChatID := b.adminPackChatID()
	if packChatID == 0 {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ MonetizedChatID not configured."))
		return
	}
	ml, err := b.db.GetMessageLogAnyState(targetUserID, packChatID)
	if err != nil || ml == nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ User not found."))
		return
	}
	if days < 0 {
		days = 0
	}
	if days > 14 {
		days = 14
	}

	tzOffset := ml.TimezoneOffsetFromMoscow
	today, err := time.Parse("2006-01-02", b.getUserLocalDate(tzOffset))
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Could not determine the user's local date."))
		return
	}
	targetDate := today.AddDate(0, 0, -days).Format("2006-01-02")

	if err := b.db.UpdateStreak(targetUserID, packChatID, ml.StreakDays, targetDate); err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Error: "+err.Error()))
		return
	}

	b.cancelTimer(targetUserID)
	timerStart := utils.FormatMoscowTime(utils.GetMoscowTime().Add(-time.Duration(days) * 24 * time.Hour))
	ml, err = b.db.GetMessageLogAnyState(targetUserID, packChatID)
	if err != nil || ml == nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ User not found after update."))
		return
	}
	ml.TimerStartTime = &timerStart
	if err := b.db.SaveMessageLog(ml); err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Could not save timer: "+err.Error()))
		return
	}

	ml, _ = b.db.GetMessageLogAnyState(targetUserID, packChatID)
	username := ml.Username
	if username == "" {
		username = fmt.Sprintf("User%d", targetUserID)
	}

	timerNote := ""
	if ml.HasSickLeave && !ml.HasHealthy {
		timerNote = " ⚠️ Sick leave active — kick deadline may differ from profile."
	} else if ml.IsExemptFromDeletion {
		timerNote = " ℹ️ User is exempt — kick timer not running."
	} else {
		remaining := b.calculateRemainingTime(ml)
		if remaining > 0 {
			b.restoreTimerWithDuration(targetUserID, packChatID, username, remaining, timerStart, tzOffset)
		} else {
			timerNote = " ⚠️ Kick deadline already passed — timer not restarted (to avoid removal)."
		}
	}

	stats := b.GetMiniappProfileStatsForAPI(targetUserID, packChatID)
	removalAt := b.GetMiniappInactivityRemovalDeadlineRFC3339(targetUserID, packChatID)
	msg := fmt.Sprintf(
		"✅ User %d: %d days without workout (last_training_date=%s).",
		targetUserID, stats.DaysSinceLastTraining, targetDate,
	)
	if removalAt != "" {
		msg += fmt.Sprintf("\nKick: %s.", removalAt)
	}
	msg += timerNote
	b.api.Send(tgbotapi.NewMessage(chatID, msg))
	b.showAdminUserCard(chatID, targetUserID)
}

func (b *Bot) adminGrantStreakSaveAttempt(chatID, targetUserID int64) {
	packChatID := b.adminPackChatID()
	used, err := b.db.DecrementStreakSaveAttemptsUsed(targetUserID, packChatID, 1)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Error: "+err.Error()))
		return
	}
	stats := b.GetMiniappProfileStatsForAPI(targetUserID, packChatID)
	avail := stats.StreakSaveAttemptsMax - used
	if avail < 0 {
		avail = 0
	}
	b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ User %d granted +1 streak save. Available: %d/%d.", targetUserID, avail, stats.StreakSaveAttemptsMax)))
	b.showAdminUserCard(chatID, targetUserID)
}

// sickLeaveAdminButton — контекстная кнопка больничного: активен/ожидает → «Отменить»,
// иначе → «Выставить» (даёт adminу оформить больничный за юзера, как если бы тот сам).
func sickLeaveAdminButton(targetUserID int64, sickActive bool) tgbotapi.InlineKeyboardButton {
	if sickActive {
		return tgbotapi.NewInlineKeyboardButtonData("🏥 Cancel sick leave", "admin_user_sick_"+strconv.FormatInt(targetUserID, 10))
	}
	return tgbotapi.NewInlineKeyboardButtonData("🏥 Set sick leave", "admin_user_sick_set_"+strconv.FormatInt(targetUserID, 10))
}

func (b *Bot) adminActivateSickLeave(chatID, targetUserID int64) {
	packChatID := b.adminPackChatID()
	if err := b.setSickLeaveForUser(targetUserID, packChatID); err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ "+err.Error()))
		return
	}
	b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Sick leave set for user %d; inactivity timer paused.", targetUserID)))
	b.notifyUserTextByID(targetUserID, packChatID, "🏥 An admin set sick leave for you. Inactivity timer paused — get well! When recovered — End sick leave in profile or #healthy in chat.", "", 0)
	b.showAdminUserCard(chatID, targetUserID)
}

// setSickLeaveForUser — admin/системная активация больничного по той же логике, что и у юзера
// (activateSickLeaveForUser): состояние выходит идентичным самостоятельному больничному.
func (b *Bot) setSickLeaveForUser(userID, packChatID int64) error {
	messageLog, err := b.db.GetMessageLogAnyState(userID, packChatID)
	if err != nil || messageLog == nil {
		return fmt.Errorf("user not found")
	}
	if messageLog.IsDeleted {
		return fmt.Errorf("user removed from pack")
	}
	if messageLog.HasSickLeave && !messageLog.HasHealthy {
		return fmt.Errorf("sick leave already active")
	}
	return b.activateSickLeaveForUser(userID, messageLog)
}

func (b *Bot) adminCancelSickLeave(chatID, targetUserID int64) {
	packChatID := b.adminPackChatID()
	if err := b.cancelSickLeaveForUser(targetUserID, packChatID); err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ "+err.Error()))
		return
	}
	b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Sick leave ended for user %d; timer resumed.", targetUserID)))
	b.notifyUserTextByID(targetUserID, packChatID, "🏥 Sick leave ended by an admin. Inactivity timer is on again — don't miss your workout!", "", 0)
	b.showAdminUserCard(chatID, targetUserID)
}

// cancelSickLeaveForUser — admin/системная отмена активного больничного (логика как #healthy).
func (b *Bot) cancelSickLeaveForUser(userID, packChatID int64) error {
	messageLog, err := b.db.GetMessageLogAnyState(userID, packChatID)
	if err != nil || messageLog == nil {
		return fmt.Errorf("user not found")
	}
	if !messageLog.HasSickLeave || messageLog.HasHealthy {
		if messageLog.SickApprovalPending {
			b.forceCancelSickLeave(userID, packChatID)
			return nil
		}
		return fmt.Errorf("no active sick leave")
	}

	b.cancelSickApprovalWatcher(userID)
	messageLog.SickApprovalPending = false
	messageLog.SickApprovalDeadline = nil
	messageLog.SickApprovalMessageID = nil

	sickLeaveEndTime := utils.FormatMoscowTime(utils.GetMoscowTime())
	messageLog.SickLeaveEndTime = &sickLeaveEndTime
	if messageLog.SickLeaveStartTime != nil {
		if sickStart, err := utils.ParseMoscowTime(*messageLog.SickLeaveStartTime); err == nil {
			if sickEnd, err := utils.ParseMoscowTime(sickLeaveEndTime); err == nil {
				sickTimeStr := sickEnd.Sub(sickStart).String()
				messageLog.SickTime = &sickTimeStr
			}
		}
	}
	messageLog.HasHealthy = true
	messageLog.HasSickLeave = false

	if err := b.db.SaveMessageLog(messageLog); err != nil {
		return err
	}
	b.trackSickLeaveEnded(userID, "manual") // §5: больничный снят (admin/#healthy-логика)

	remainingTime := b.calculateRemainingTime(messageLog)
	if remainingTime <= 0 {
		username := messageLog.Username
		if username == "" {
			username = fmt.Sprintf("User%d", userID)
		}
		b.removeUser(userID, packChatID, username)
		return nil
	}

	if messageLog.TimerStartTime == nil || strings.TrimSpace(*messageLog.TimerStartTime) == "" {
		b.startTimerWithDuration(userID, packChatID, messageLog.Username, remainingTime)
	} else {
		ts := strings.TrimSpace(*messageLog.TimerStartTime)
		b.restoreTimerWithDuration(userID, packChatID, messageLog.Username, remainingTime, ts, messageLog.TimezoneOffsetFromMoscow)
	}
	return nil
}

func (b *Bot) adminDeleteUser(chatID, targetUserID int64) {
	packChatID := b.adminPackChatID()
	ml, err := b.db.GetMessageLogAnyState(targetUserID, packChatID)
	if err != nil || ml == nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ User not found."))
		return
	}
	if ml.IsDeleted {
		b.api.Send(tgbotapi.NewMessage(chatID, "ℹ️ User is already removed from the pack."))
		b.showAdminUserCard(chatID, targetUserID)
		return
	}
	username := ml.Username
	if username == "" {
		username = fmt.Sprintf("User%d", targetUserID)
	}
	b.removeUserNow(targetUserID, packChatID, username)
	b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ User %d removed from the pack.", targetUserID)))
}

// adminRestoreUser restores удалённого Лео юзера. withProgress=true — с достижениями
// (ачивки/рекорд сохраняются, кубки восстанавливаются из снапшота cups_at_deletion);
// false — с нуля (как обычный return: ачивки и кубки обнулены). В обоих случаях we restore
// access к мини-аппу, делаем чистый старт таймера неактивности и обновляем меню-кнопку.
func (b *Bot) adminRestoreUser(chatID, targetUserID int64, withProgress bool) {
	if err := b.restoreUserInPack(targetUserID, withProgress); err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ "+err.Error()))
		if strings.Contains(err.Error(), "already active") {
			b.showAdminUserCard(chatID, targetUserID)
		}
		return
	}
	mode := "from scratch"
	if withProgress {
		mode = "with progress"
	}
	b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ User %d restored to the pack (%s). Access restored; inactivity timer restarted.", targetUserID, mode)))
	b.showAdminUserCard(chatID, targetUserID)
}

// restoreUserInPack — общая логика return кикнутого: прогресс/с нуля, paywall, таймер, меню.
func (b *Bot) restoreUserInPack(targetUserID int64, withProgress bool) error {
	packChatID := b.adminPackChatID()
	if packChatID == 0 {
		return fmt.Errorf("MonetizedChatID not configured")
	}
	ml, err := b.db.GetMessageLogAnyState(targetUserID, packChatID)
	if err != nil || ml == nil {
		return fmt.Errorf("user not found")
	}
	if !ml.IsDeleted {
		return fmt.Errorf("user already active in pack")
	}
	username := strings.TrimSpace(ml.Username)
	if username == "" {
		username = fmt.Sprintf("User%d", targetUserID)
	}

	var restored bool
	if withProgress {
		restored, err = b.db.RestoreDeletedUserWithProgress(targetUserID, packChatID, ml.Username)
	} else {
		restored, err = b.db.ReactivateReturnedUser(targetUserID, packChatID, ml.Username)
	}
	if err != nil {
		return fmt.Errorf("restore error: %w", err)
	}
	if !restored {
		return fmt.Errorf("could not restore (no user record)")
	}

	if _, accErr := b.db.RestorePaywallAccessForUser(targetUserID, packChatID); accErr != nil {
		b.logger.Warnf("admin restore: restore paywall access user=%d: %v", targetUserID, accErr)
	}
	b.startTimer(targetUserID, packChatID, username)
	invalidateMiniappMenuButtonCache(targetUserID)
	b.applyMiniappMenuButtonForUser(targetUserID)
	return nil
}

func (b *Bot) adminDeleteMessageByRef(ref string) (bool, string) {
	packChatID := b.adminPackChatID()
	ref = strings.TrimSpace(ref)
	if packChatID == 0 || ref == "" {
		return false, ""
	}

	lower := strings.ToLower(ref)
	switch {
	case strings.HasPrefix(lower, "t"):
		id, err := strconv.ParseInt(ref[1:], 10, 64)
		if err != nil || id <= 0 {
			return false, ""
		}
		ok, err := b.db.AdminDeleteTrainingFeedThreadReply(packChatID, id)
		if err != nil || !ok {
			return false, ""
		}
		return true, fmt.Sprintf("comment t%d", id)

	case strings.HasPrefix(lower, "g"):
		id, err := strconv.ParseInt(ref[1:], 10, 64)
		if err != nil || id <= 0 {
			return false, ""
		}
		ok, err := b.db.AdminDeletePackGroupMessage(packChatID, id)
		if err != nil || !ok {
			return false, ""
		}
		return true, fmt.Sprintf("chat message g%d", id)

	default:
		idStr := strings.TrimPrefix(lower, "#")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || id <= 0 {
			return false, ""
		}
		ok, err := b.db.AdminDeleteFeedUserMessage(packChatID, id)
		if err != nil || !ok {
			return false, ""
		}
		return true, fmt.Sprintf("post #%d", id)
	}
}

func (b *Bot) adminDeleteReportedContent(chatID, reportID int64) {
	if b == nil || b.db == nil {
		return
	}
	packChatID := b.adminPackChatID()
	item, err := b.db.GetMiniappFeedReport(packChatID, reportID)
	if err != nil || item == nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Report not found."))
		return
	}

	var ok bool
	var label string
	switch item.TargetType {
	case "pack_group_message":
		ok, err = b.db.AdminDeletePackGroupMessage(packChatID, item.UserMessageID)
		label = fmt.Sprintf("pack chat message #%d", item.UserMessageID)
	case "thread_reply":
		ok, err = b.db.AdminDeleteTrainingFeedThreadReply(packChatID, item.ThreadReplyID)
		label = fmt.Sprintf("comment t%d", item.ThreadReplyID)
	default:
		ok, err = b.db.AdminDeleteFeedUserMessage(packChatID, item.UserMessageID)
		label = fmt.Sprintf("post #%d", item.UserMessageID)
	}
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Error: "+err.Error()))
		return
	}
	if !ok {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Content already deleted or not found."))
		return
	}
	_, _ = b.db.DismissMiniappFeedReport(packChatID, reportID)
	b.trackReportResolved(reportID, chatID, item.TargetUserID, "delete")
	b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Deleted without hide mark: %s. Report #%d closed.", label, reportID)))
	b.showAdminFeedReportsInbox(chatID)
}

func (b *Bot) adminHideReportedContent(chatID, reportID int64) {
	if b == nil || b.db == nil {
		return
	}
	packChatID := b.adminPackChatID()
	item, err := b.db.GetMiniappFeedReport(packChatID, reportID)
	if err != nil || item == nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Report not found."))
		return
	}

	var ok bool
	var label string
	switch item.TargetType {
	case "pack_group_message":
		ok, err = b.db.AdminHidePackGroupMessage(packChatID, item.UserMessageID)
		label = fmt.Sprintf("pack chat message #%d", item.UserMessageID)
	case "thread_reply":
		ok, err = b.db.AdminHideTrainingFeedThreadReply(packChatID, item.ThreadReplyID)
		label = fmt.Sprintf("comment t%d", item.ThreadReplyID)
	default:
		ok, err = b.db.AdminHideFeedUserMessage(packChatID, item.UserMessageID, "report")
		label = fmt.Sprintf("post #%d", item.UserMessageID)
	}
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Error: "+err.Error()))
		return
	}
	if !ok {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Content already hidden or not found."))
		return
	}
	if item.TargetUserID > 0 {
		count := b.recordUGCViolation(item.TargetUserID, packChatID, false)
		b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf(
			"🙈 Hidden: %s. Author violations: %d. Report #%d closed.\n\nRestore: Support → Hidden.",
			label, count, reportID,
		)))
	} else {
		b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf(
			"🙈 Hidden: %s. Report #%d closed.\n\nRestore: Support → Hidden.",
			label, reportID,
		)))
	}
	_, _ = b.db.DismissMiniappFeedReport(packChatID, reportID)
	b.trackReportResolved(reportID, chatID, item.TargetUserID, "hide")
	b.showAdminFeedReportsInbox(chatID)
}

func (b *Bot) adminMuteReportedUser(chatID, reportID int64) {
	if b == nil || b.db == nil {
		return
	}
	packChatID := b.adminPackChatID()
	item, err := b.db.GetMiniappFeedReport(packChatID, reportID)
	if err != nil || item == nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Report not found."))
		return
	}
	if item.TargetUserID <= 0 {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ No user to mute."))
		return
	}
	b.adminMuteUserUGC(chatID, item.TargetUserID, packChatID, 24)
	_, _ = b.db.DismissMiniappFeedReport(packChatID, reportID)
	b.trackReportResolved(reportID, chatID, item.TargetUserID, "mute")
	b.showAdminFeedReportsInbox(chatID)
}
