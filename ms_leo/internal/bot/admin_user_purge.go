package bot

import (
	"fmt"
	"strconv"
	"strings"

	"leo-bot/internal/database"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// showAdminUserPurgePrompt — шаг 1: показываем реальный след юзера в БД по таблицам.
func (b *Bot) showAdminUserPurgePrompt(chatID, targetUserID int64) {
	if b == nil || b.db == nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Database unavailable."))
		return
	}
	report, err := b.db.AdminCountUserFootprint(targetUserID)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Could not count user data: "+err.Error()))
		return
	}

	var body strings.Builder
	body.WriteString(fmt.Sprintf("☠️ <b>Full delete for user %d</b>\n\n", targetUserID))
	body.WriteString("Found in database:\n")
	rows := report.NonEmpty()
	if len(rows) == 0 {
		body.WriteString("• nothing — user is already gone from the database\n")
	} else {
		for _, row := range rows {
			body.WriteString(fmt.Sprintf("• %s: %d\n", adminEscapeHTML(row.Label), row.Rows))
		}
	}
	body.WriteString(fmt.Sprintf("\nTotal rows: <b>%d</b>\n\n", report.Total))
	body.WriteString("This permanently deletes: pack profile, cups, streak, achievements, ")
	body.WriteString("posts and comments, chats, payments, analytics, and admin rights.\n")
	body.WriteString("Nothing left to restore with ♻️ Restore.\n\n")
	body.WriteString("After deletion they start fresh: /start and the mini app run the newcomer flow.")

	msg := tgbotapi.NewMessage(chatID, body.String())
	msg.ParseMode = "HTML"
	msg.DisableWebPagePreview = true
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("☠️ Delete completely", "admin_user_purge_go_"+strconv.FormatInt(targetUserID, 10)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ Cancel", "admin_user_open_"+strconv.FormatInt(targetUserID, 10)),
		),
	)
	b.api.Send(msg)
}

// showAdminUserPurgeConfirm — шаг 2: последнее подтверждение, действие необратимо.
func (b *Bot) showAdminUserPurgeConfirm(chatID, adminID, targetUserID int64) {
	text := fmt.Sprintf(
		"⚠️ Final confirmation.\n\nUser %d will be erased from all tables with no undo.",
		targetUserID,
	)
	if adminID == targetUserID {
		text += "\n\n‼️ This is YOUR account: you will lose your pack profile and admin rights stored in the database."
	}
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("☠️ Yes, delete forever", "admin_user_purge_yes_"+strconv.FormatInt(targetUserID, 10)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ Cancel", "admin_user_open_"+strconv.FormatInt(targetUserID, 10)),
		),
	)
	b.api.Send(msg)
}

// executeAdminUserPurge — сам каскад плюс сброс всего, что бот держал по юзеру в памяти.
func (b *Bot) executeAdminUserPurge(chatID, adminID, targetUserID int64) {
	if b == nil || b.db == nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Database unavailable."))
		return
	}

	// Таймер гасим до удаления: иначе висящая горутина может дописать строки обратно.
	b.cancelTimer(targetUserID)
	b.cancelSickApprovalWatcher(targetUserID)

	report, err := b.db.AdminPurgeUserEverywhere(targetUserID)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Deletion failed: "+err.Error()))
		return
	}

	b.forgetUserRuntimeState(targetUserID)
	b.logger.Infof("admin %d purged user %d completely: %d rows", adminID, targetUserID, report.Total)

	var body strings.Builder
	body.WriteString(fmt.Sprintf("✅ User %d fully removed from the database.\n\n", targetUserID))
	body.WriteString(adminUserPurgeSummary(report))
	body.WriteString("\n\nThey no longer exist for the bot — they will go through the newcomer flow again.")
	if adminID == targetUserID {
		body.WriteString("\n\n⚠️ You deleted your own account. Rights from OWNER_ID/ADMIN_IDS remain (they live in .env).")
	}

	msg := tgbotapi.NewMessage(chatID, body.String())
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📋 Back to list", "admin_users_list_0"),
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Admin", "admin_open"),
		),
	)
	b.api.Send(msg)
}

func adminUserPurgeSummary(report database.AdminUserPurgeReport) string {
	rows := report.NonEmpty()
	if len(rows) == 0 {
		return "Nothing to delete — no rows were found in the database."
	}
	var sb strings.Builder
	sb.WriteString("Deleted:\n")
	for _, row := range rows {
		sb.WriteString(fmt.Sprintf("• %s: %d\n", row.Label, row.Rows))
	}
	sb.WriteString(fmt.Sprintf("Total rows: %d", report.Total))
	return sb.String()
}

// forgetUserRuntimeState — сбрасывает всё, что бот держит по юзеру в памяти.
// Без этого удалённый из БД юзер остался бы «наполовину живым» до перезапуска процесса.
func (b *Bot) forgetUserRuntimeState(userID int64) {
	if b == nil || userID == 0 {
		return
	}
	b.cancelTimer(userID)
	b.cancelSickApprovalWatcher(userID)
	b.clearAdminFlow(userID)
	b.clearUserSupportSession(userID)
	b.miniappPersonalClear(userID)
	b.privateBottomKeyboardKind.Delete(userID)
	b.miniappReplyOrigin.Delete(userID)
	b.miniappTrainingPhotoURL.Delete(userID)
	// User мог быть динамическим adminом — перечитываем кэш прав из БД.
	b.reloadDynamicAdmins()
	// Accessа больше нет: прячем web_app-кнопку мини-аппа в личке.
	invalidateMiniappMenuButtonCache(userID)
	b.applyMiniappMenuButtonForUser(userID)
}
