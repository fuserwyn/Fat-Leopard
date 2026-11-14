package bot

import (
	"fmt"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Список админов: владелец и ADMIN_IDS живут в .env и из бота не снимаются,
// динамические админы лежат в dynamic_admins — их можно добавлять и удалять,
// в том числе самому себя.
func (b *Bot) showAdminAdminsList(chatID, viewerID int64) {
	if b == nil || b.db == nil || b.config == nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ База недоступна."))
		return
	}
	dynamic, err := b.db.ListDynamicAdmins()
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Не удалось загрузить список админов: "+err.Error()))
		return
	}

	var body strings.Builder
	body.WriteString("<b>🛡 Админы</b>\n\n")

	body.WriteString("<b>Из .env</b> (снимаются только правкой OWNER_ID/ADMIN_IDS):\n")
	envIDs := b.config.AdminTelegramUserIDs()
	if len(envIDs) == 0 {
		body.WriteString("— пусто\n")
	}
	for _, id := range envIDs {
		mark := ""
		if id == viewerID {
			mark = " · это ты"
		}
		if b.config.OwnerID != 0 && id == b.config.OwnerID {
			mark += " · владелец"
		}
		body.WriteString(fmt.Sprintf("🔒 %s%s\n", adminEscapeHTML(b.supportDisplayName(id)), mark))
	}

	body.WriteString("\n<b>Добавленные через бота:</b>\n")
	if len(dynamic) == 0 {
		body.WriteString("— пусто\n")
	}
	rows := make([][]tgbotapi.InlineKeyboardButton, 0, len(dynamic)+2)
	for _, a := range dynamic {
		mark := ""
		if a.UserID == viewerID {
			mark = " · это ты"
		}
		body.WriteString(fmt.Sprintf("👤 %s%s\n", adminEscapeHTML(adminRosterLabel(a.Username, a.UserID)), mark))
		btnText := "🗑 Снять " + adminTruncateRunes(adminRosterLabel(a.Username, a.UserID), 24)
		if a.UserID == viewerID {
			btnText = "🚪 Снять себя"
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(btnText, "admin_admin_del_"+strconv.FormatInt(a.UserID, 10)),
		))
	}

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("➕ Добавить админа", "admin_admin_add"),
	))
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("⬅️ К админке", "admin_open"),
	))

	msg := tgbotapi.NewMessage(chatID, body.String())
	msg.ParseMode = "HTML"
	msg.DisableWebPagePreview = true
	msg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
	b.api.Send(msg)
}

func (b *Bot) startAdminAddAdmin(adminID int64) {
	b.adminSessionsMutex.Lock()
	b.adminSessions[adminID] = &adminSession{Mode: "admin_add", Step: "await_admin_id"}
	b.adminSessionsMutex.Unlock()
}

// adminAddDynamicAdmin — принимает Telegram user_id или @ник (ник ищем в training_state).
func (b *Bot) adminAddDynamicAdmin(chatID, adderID int64, query string) {
	query = strings.TrimSpace(query)
	if query == "" {
		b.api.Send(tgbotapi.NewMessage(chatID, "⚠️ Отправь user_id или @ник."))
		return
	}

	targetID, err := strconv.ParseInt(strings.TrimSpace(query), 10, 64)
	if err != nil || targetID <= 0 {
		targetID, err = b.db.FindUserIDByUsername(query)
		if err != nil || targetID <= 0 {
			b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf(
				"❌ Не нашёл пользователя «%s». Отправь числовой user_id — по нику ищем только тех, кто уже писал боту.",
				query)))
			return
		}
	}

	if b.config.IsAdminTelegramUser(targetID) {
		b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("ℹ️ %d уже админ из .env — добавлять в базу не нужно.", targetID)))
		b.showAdminAdminsList(chatID, adderID)
		return
	}

	username := ""
	if ml, err := b.db.GetMessageLogAnyState(targetID, b.adminPackChatID()); err == nil && ml != nil {
		username = strings.TrimSpace(ml.Username)
	}
	if username == "" && strings.HasPrefix(query, "@") {
		username = query
	}

	if err := b.db.AddDynamicAdmin(targetID, username, adderID); err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Не удалось добавить: "+err.Error()))
		return
	}
	b.reloadDynamicAdmins()
	// Нижняя reply-клавиатура в личке зависит от прав — сбрасываем кэш, чтобы
	// у нового админа при следующем сообщении появилась кнопка «Админ-панель».
	b.privateBottomKeyboardKind.Delete(targetID)

	b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf(
		"✅ %s теперь админ. Панель откроется у него по /admin в личке с ботом.",
		adminRosterLabel(username, targetID))))
	b.showAdminAdminsList(chatID, adderID)
}

func (b *Bot) showAdminRemoveAdminConfirm(chatID, viewerID, targetID int64) {
	isDynamic, err := b.db.IsDynamicAdmin(targetID)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка проверки прав: "+err.Error()))
		return
	}
	if !isDynamic {
		b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf(
			"ℹ️ %d нет в списке добавленных через бота. Админов из OWNER_ID/ADMIN_IDS снимают правкой .env.", targetID)))
		return
	}

	text := fmt.Sprintf("🗑 Снять админку с %d?\n\nПрофиль стаи, кубки и стрик останутся — уйдут только права.", targetID)
	if viewerID == targetID {
		text = "🚪 Снять админку с себя?\n\nДоступ к /admin пропадёт сразу — вернуть сможет только другой админ."
	}
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Да, снять", "admin_admin_del_yes_"+strconv.FormatInt(targetID, 10)),
			tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", "admin_admins"),
		),
	)
	b.api.Send(msg)
}

func (b *Bot) adminRemoveDynamicAdmin(chatID, viewerID, targetID int64) {
	removed, err := b.db.RemoveDynamicAdmin(targetID)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Не удалось снять админку: "+err.Error()))
		return
	}
	if !removed {
		b.api.Send(tgbotapi.NewMessage(chatID, "ℹ️ Такого админа в базе уже нет."))
		return
	}
	b.reloadDynamicAdmins()
	b.privateBottomKeyboardKind.Delete(targetID)

	if viewerID == targetID {
		b.api.Send(tgbotapi.NewMessage(chatID, "✅ Ты снял админку с себя. Вернуть сможет другой админ через 🛡 Админы."))
		return
	}
	b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Админка снята с %d.", targetID)))
	b.showAdminAdminsList(chatID, viewerID)
}

// handleAdminAdminsCallback — ветка «🛡 Админы». Порядок префиксов важен:
// admin_admin_del_yes_ специфичнее, чем admin_admin_del_.
func (b *Bot) handleAdminAdminsCallback(callback *tgbotapi.CallbackQuery) bool {
	if callback == nil || callback.Message == nil || callback.From == nil {
		return false
	}
	data := callback.Data
	chatID := callback.Message.Chat.ID
	viewerID := callback.From.ID

	parseTarget := func(prefix string) (int64, bool) {
		id, err := strconv.ParseInt(strings.TrimPrefix(data, prefix), 10, 64)
		return id, err == nil && id > 0
	}

	switch {
	case data == "admin_admins":
		b.clearAdminFlow(viewerID)
		b.showAdminAdminsList(chatID, viewerID)
		return true

	case data == "admin_admin_add":
		b.startAdminAddAdmin(viewerID)
		b.api.Send(tgbotapi.NewMessage(chatID, "👤 Отправь Telegram user_id нового админа (или @ник, если он писал боту). Отмена — /cancel."))
		return true

	case strings.HasPrefix(data, "admin_admin_del_yes_"):
		if targetID, ok := parseTarget("admin_admin_del_yes_"); ok {
			b.adminRemoveDynamicAdmin(chatID, viewerID, targetID)
		}
		return true

	case strings.HasPrefix(data, "admin_admin_del_"):
		if targetID, ok := parseTarget("admin_admin_del_"); ok {
			b.showAdminRemoveAdminConfirm(chatID, viewerID, targetID)
		}
		return true
	}
	return false
}

func adminRosterLabel(username string, userID int64) string {
	u := strings.TrimSpace(username)
	if u == "" {
		return strconv.FormatInt(userID, 10)
	}
	return fmt.Sprintf("%s · %d", u, userID)
}
