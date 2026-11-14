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
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ База недоступна."))
		return
	}
	report, err := b.db.AdminCountUserFootprint(targetUserID)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Не удалось посчитать данные юзера: "+err.Error()))
		return
	}

	var body strings.Builder
	body.WriteString(fmt.Sprintf("☠️ <b>Полное удаление пользователя %d</b>\n\n", targetUserID))
	body.WriteString("Найдено в базе:\n")
	rows := report.NonEmpty()
	if len(rows) == 0 {
		body.WriteString("• ничего — юзера в базе уже нет\n")
	} else {
		for _, row := range rows {
			body.WriteString(fmt.Sprintf("• %s: %d\n", adminEscapeHTML(row.Label), row.Rows))
		}
	}
	body.WriteString(fmt.Sprintf("\nВсего строк: <b>%d</b>\n\n", report.Total))
	body.WriteString("Удалится безвозвратно и целиком: профиль стаи, кубки, стрик, ачивки, ")
	body.WriteString("посты и комментарии, чаты, оплаты, аналитика и права админа.\n")
	body.WriteString("Восстановить через «♻️ Вернуть» будет уже нечего.\n\n")
	body.WriteString("После удаления юзер придёт как новый: /start и мини-апп проведут его по пути новичка.")

	msg := tgbotapi.NewMessage(chatID, body.String())
	msg.ParseMode = "HTML"
	msg.DisableWebPagePreview = true
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("☠️ Стереть полностью", "admin_user_purge_go_"+strconv.FormatInt(targetUserID, 10)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", "admin_user_open_"+strconv.FormatInt(targetUserID, 10)),
		),
	)
	b.api.Send(msg)
}

// showAdminUserPurgeConfirm — шаг 2: последнее подтверждение, действие необратимо.
func (b *Bot) showAdminUserPurgeConfirm(chatID, adminID, targetUserID int64) {
	text := fmt.Sprintf(
		"⚠️ Последнее подтверждение.\n\nПользователь %d будет стёрт из всех таблиц без возможности отката.",
		targetUserID,
	)
	if adminID == targetUserID {
		text += "\n\n‼️ Это ТВОЙ аккаунт: ты потеряешь профиль стаи и права админа из базы."
	}
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("☠️ Да, стереть навсегда", "admin_user_purge_yes_"+strconv.FormatInt(targetUserID, 10)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", "admin_user_open_"+strconv.FormatInt(targetUserID, 10)),
		),
	)
	b.api.Send(msg)
}

// executeAdminUserPurge — сам каскад плюс сброс всего, что бот держал по юзеру в памяти.
func (b *Bot) executeAdminUserPurge(chatID, adminID, targetUserID int64) {
	if b == nil || b.db == nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ База недоступна."))
		return
	}

	// Таймер гасим до удаления: иначе висящая горутина может дописать строки обратно.
	b.cancelTimer(targetUserID)
	b.cancelSickApprovalWatcher(targetUserID)

	report, err := b.db.AdminPurgeUserEverywhere(targetUserID)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Удаление не удалось: "+err.Error()))
		return
	}

	b.forgetUserRuntimeState(targetUserID)
	b.logger.Infof("admin %d purged user %d completely: %d rows", adminID, targetUserID, report.Total)

	var body strings.Builder
	body.WriteString(fmt.Sprintf("✅ Пользователь %d полностью удалён из базы.\n\n", targetUserID))
	body.WriteString(adminUserPurgeSummary(report))
	body.WriteString("\n\nОн больше не существует для бота — заново пройдёт путь новичка.")
	if adminID == targetUserID {
		body.WriteString("\n\n⚠️ Ты стёр свой аккаунт. Права из OWNER_ID/ADMIN_IDS остаются (они в .env).")
	}

	msg := tgbotapi.NewMessage(chatID, body.String())
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📋 К списку", "admin_users_list_0"),
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Админка", "admin_open"),
		),
	)
	b.api.Send(msg)
}

func adminUserPurgeSummary(report database.AdminUserPurgeReport) string {
	rows := report.NonEmpty()
	if len(rows) == 0 {
		return "Удалять было нечего — записей в базе не нашлось."
	}
	var sb strings.Builder
	sb.WriteString("Удалено:\n")
	for _, row := range rows {
		sb.WriteString(fmt.Sprintf("• %s: %d\n", row.Label, row.Rows))
	}
	sb.WriteString(fmt.Sprintf("Всего строк: %d", report.Total))
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
	// Юзер мог быть динамическим админом — перечитываем кэш прав из БД.
	b.reloadDynamicAdmins()
	// Доступа больше нет: прячем web_app-кнопку мини-аппа в личке.
	invalidateMiniappMenuButtonCache(userID)
	b.applyMiniappMenuButtonForUser(userID)
}
