package bot

import (
	"fmt"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Список adminов: owner и ADMIN_IDS живут в .env и из бота не снимаются,
// динамические adminы лежат в dynamic_admins — их можно добавлять и удалять,
// в том числе самому себя.
func (b *Bot) showAdminAdminsList(chatID, viewerID int64) {
	if b == nil || b.db == nil || b.config == nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Database unavailable."))
		return
	}
	dynamic, err := b.db.ListDynamicAdmins()
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Could not load admin list: "+err.Error()))
		return
	}

	var body strings.Builder
	body.WriteString("<b>🛡 Admins</b>\n\n")

	body.WriteString("<b>From .env</b> (change via OWNER_ID/ADMIN_IDS only):\n")
	envIDs := b.config.AdminTelegramUserIDs()
	if len(envIDs) == 0 {
		body.WriteString("— empty\n")
	}
	for _, id := range envIDs {
		mark := ""
		if id == viewerID {
			mark = " · you"
		}
		if b.config.OwnerID != 0 && id == b.config.OwnerID {
			mark += " · owner"
		}
		body.WriteString(fmt.Sprintf("🔒 %s%s\n", adminEscapeHTML(b.supportDisplayName(id)), mark))
	}

	body.WriteString("\n<b>Added via bot:</b>\n")
	if len(dynamic) == 0 {
		body.WriteString("— empty\n")
	}
	rows := make([][]tgbotapi.InlineKeyboardButton, 0, len(dynamic)+2)
	for _, a := range dynamic {
		mark := ""
		if a.UserID == viewerID {
			mark = " · you"
		}
		body.WriteString(fmt.Sprintf("👤 %s%s\n", adminEscapeHTML(adminRosterLabel(a.Username, a.UserID)), mark))
		btnText := "🗑 Remove " + adminTruncateRunes(adminRosterLabel(a.Username, a.UserID), 24)
		if a.UserID == viewerID {
			btnText = "🚪 Remove yourself"
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(btnText, "admin_admin_del_"+strconv.FormatInt(a.UserID, 10)),
		))
	}

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("➕ Add admin", "admin_admin_add"),
	))
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("⬅️ Admin panel", "admin_open"),
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
		b.api.Send(tgbotapi.NewMessage(chatID, "⚠️ Send user_id or @username."))
		return
	}

	targetID, err := strconv.ParseInt(strings.TrimSpace(query), 10, 64)
	if err != nil || targetID <= 0 {
		targetID, err = b.db.FindUserIDByUsername(query)
		if err != nil || targetID <= 0 {
			b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf(
				"❌ User «%s» not found. Send numeric user_id — usernames only work if they already messaged the bot.",
				query)))
			return
		}
	}

	if b.config.IsAdminTelegramUser(targetID) {
		b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("ℹ️ %d is already an env admin — no need to add to DB.", targetID)))
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
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Could not add: "+err.Error()))
		return
	}
	b.reloadDynamicAdmins()
	// Нижняя reply-клавиатура в личке зависит от прав — сбрасываем кэш, чтобы
	// у нового админа при следующем сообщении появилась кнопка «Админ-панель».
	b.privateBottomKeyboardKind.Delete(targetID)

	b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf(
		"✅ %s is now an admin. They get /admin in DM.",
		adminRosterLabel(username, targetID))))
	b.showAdminAdminsList(chatID, adderID)
}

func (b *Bot) showAdminRemoveAdminConfirm(chatID, viewerID, targetID int64) {
	isDynamic, err := b.db.IsDynamicAdmin(targetID)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Permission check error: "+err.Error()))
		return
	}
	if !isDynamic {
		b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf(
			"ℹ️ %d is not in the bot-added list. Remove env admins via .env.", targetID)))
		return
	}

	text := fmt.Sprintf("🗑 Remove admin rights from %d?\n\nPack profile, cups, and streak stay — only permissions go away.", targetID)
	if viewerID == targetID {
		text = "🚪 Remove your own admin rights?\n\n/admin access ends immediately — only another admin can restore it."
	}
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Yes, remove", "admin_admin_del_yes_"+strconv.FormatInt(targetID, 10)),
			tgbotapi.NewInlineKeyboardButtonData("❌ Cancel", "admin_admins"),
		),
	)
	b.api.Send(msg)
}

func (b *Bot) adminRemoveDynamicAdmin(chatID, viewerID, targetID int64) {
	removed, err := b.db.RemoveDynamicAdmin(targetID)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Could not remove admin: "+err.Error()))
		return
	}
	if !removed {
		b.api.Send(tgbotapi.NewMessage(chatID, "ℹ️ That admin is not in the DB."))
		return
	}
	b.reloadDynamicAdmins()
	b.privateBottomKeyboardKind.Delete(targetID)

	if viewerID == targetID {
		b.api.Send(tgbotapi.NewMessage(chatID, "✅ You removed your own admin. Another admin can add you back via 🛡 Admins."))
		return
	}
	b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Admin removed from %d.", targetID)))
	b.showAdminAdminsList(chatID, viewerID)
}

// handleAdminAdminsCallback — ветка «🛡 Admins». Порядок префиксов важен:
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
		b.api.Send(tgbotapi.NewMessage(chatID, "👤 Send new admin Telegram user_id (or @username if they messaged the bot). Cancel — /cancel."))
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
