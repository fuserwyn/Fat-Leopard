package bot

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"leo-bot/internal/moderation"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type adminSession struct {
	Mode         string // feed_text | poll | support | user_mgmt | ... | tracker_approval_edit | tracker_approval_cancel
	Step         string // await_text | ... | await_prompt | await_comment
	TargetUserID int64
	TaskID       int64
	PollQuestion string
	FeedText     string // черновик текста админского поста (между шагами выбора автора/времени)
	PostAuthor   string // adminPostAuthorLeo | adminPostAuthorAdmin — от чьего имени публиковать
}

// isOwnerPrivateChat — админ (владелец, ADMIN_IDS или добавленный через 🛡 Админы) в личке с ботом.
// Проверка идёт через isAdminTelegramUser, как и у callback-ов панели: иначе динамический админ
// видел бы кнопки, но его текстовые ответы в мастерах не обрабатывались бы.
func (b *Bot) isOwnerPrivateChat(msg *tgbotapi.Message) bool {
	return msg != nil && msg.From != nil && msg.Chat != nil && b.isAdminTelegramUser(msg.From.ID) && msg.Chat.IsPrivate()
}

func (b *Bot) handleAdmin(msg *tgbotapi.Message) {
	if !b.isOwnerPrivateChat(msg) {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Админ-панель доступна только админам в личном чате с ботом.")
		b.api.Send(reply)
		return
	}
	b.openAdminPanelForUser(msg.Chat.ID, msg.From.ID)
}

func (b *Bot) showAdminMenu(chatID int64) {
	b.showAdminMenuForUser(chatID)
}

func (b *Bot) showAdminMenuForUser(chatID int64) {
	text := "⚙️ Админ-панель\n\nВыбери действие:"
	rows := [][]tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("💬 Поддержка", "admin_support_inbox"),
			tgbotapi.NewInlineKeyboardButtonData("📋 Список юзеров", "admin_users_list_0"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("💳 Оплаты", "admin_payments_0"),
			tgbotapi.NewInlineKeyboardButtonData("🔍 Найти юзера", "admin_users"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📝 Текст", "admin_mode_feed_text"),
			tgbotapi.NewInlineKeyboardButtonData("🗳 Опрос", "admin_mode_poll"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📅 Отложенные", "admin_sched_list"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📊 Посещения бота", "admin_visit_stats"),
			tgbotapi.NewInlineKeyboardButtonData("📈 Аналитика", "admin_analytics"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🛡 Админы", "admin_admins"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🗑 Очистить ленту и чат", "admin_wipe_pack_prompt"),
		),
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("❎ Отмена", "admin_cancel"),
	))
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
	b.api.Send(msg)
}

func (b *Bot) handleAdminCallbackQuery(callback *tgbotapi.CallbackQuery) {
	if callback == nil || callback.Message == nil || callback.From == nil {
		return
	}
	if !b.isAdminTelegramUser(callback.From.ID) || !callback.Message.Chat.IsPrivate() {
		callbackConfig := tgbotapi.NewCallback(callback.ID, "Недостаточно прав")
		b.api.Request(callbackConfig)
		return
	}

	if b.handleTrackerApprovalCallback(callback) {
		return
	}

	if strings.HasPrefix(callback.Data, "admin_support_user_") {
		userID, err := strconv.ParseInt(strings.TrimPrefix(callback.Data, "admin_support_user_"), 10, 64)
		if err == nil && userID > 0 {
			b.showAdminSupportThread(callback.Message.Chat.ID, userID)
		}
		callbackConfig := tgbotapi.NewCallback(callback.ID, "")
		b.api.Request(callbackConfig)
		return
	}
	if strings.HasPrefix(callback.Data, "admin_support_reply_") {
		userID, err := strconv.ParseInt(strings.TrimPrefix(callback.Data, "admin_support_reply_"), 10, 64)
		if err == nil && userID > 0 {
			b.startAdminSupportReply(callback.From.ID, userID)
			b.api.Send(tgbotapi.NewMessage(callback.Message.Chat.ID, "✍️ Напиши ответ пользователю. Он уйдёт в поддержку внутри мини-аппа."))
		}
		callbackConfig := tgbotapi.NewCallback(callback.ID, "")
		b.api.Request(callbackConfig)
		return
	}
	if strings.HasPrefix(callback.Data, "admin_sched_cancel_") {
		postID, err := strconv.ParseInt(strings.TrimPrefix(callback.Data, "admin_sched_cancel_"), 10, 64)
		if err == nil && postID > 0 {
			b.cancelAdminScheduledPost(callback.Message.Chat.ID, postID)
		}
		b.api.Request(tgbotapi.NewCallback(callback.ID, ""))
		return
	}
	if strings.HasPrefix(callback.Data, "admin_feed_report_dismiss_") {
		reportID, err := strconv.ParseInt(strings.TrimPrefix(callback.Data, "admin_feed_report_dismiss_"), 10, 64)
		if err == nil && reportID > 0 {
			fromPanel := strings.Contains(callback.Message.Text, "Тип:")
			toast := b.dismissAdminFeedReport(callback.From.ID, callback.Message.Chat.ID, reportID, callback.Message, fromPanel)
			callbackConfig := tgbotapi.NewCallback(callback.ID, toast)
			b.api.Request(callbackConfig)
			return
		}
		callbackConfig := tgbotapi.NewCallback(callback.ID, "")
		b.api.Request(callbackConfig)
		return
	}
	if strings.HasPrefix(callback.Data, "admin_users_list_") ||
		strings.HasPrefix(callback.Data, "admin_payments_") {
		if b.handleAdminDirectoryCallback(callback) {
			callbackConfig := tgbotapi.NewCallback(callback.ID, "")
			b.api.Request(callbackConfig)
			return
		}
	}
	if callback.Data == "admin_analytics" || strings.HasPrefix(callback.Data, "admin_an_") {
		if b.handleAdminAnalyticsCallback(callback) {
			callbackConfig := tgbotapi.NewCallback(callback.ID, "")
			b.api.Request(callbackConfig)
			return
		}
	}
	if callback.Data == "admin_admins" || strings.HasPrefix(callback.Data, "admin_admin_") {
		if b.handleAdminAdminsCallback(callback) {
			callbackConfig := tgbotapi.NewCallback(callback.ID, "")
			b.api.Request(callbackConfig)
			return
		}
	}
	if strings.HasPrefix(callback.Data, "admin_feed_report_del_") ||
		strings.HasPrefix(callback.Data, "admin_feed_report_hide_") ||
		strings.HasPrefix(callback.Data, "admin_feed_report_mute_") ||
		strings.HasPrefix(callback.Data, "admin_feed_report_unmute_") ||
		strings.HasPrefix(callback.Data, "admin_hidden_restore_") ||
		callback.Data == "admin_hidden_inbox" ||
		strings.HasPrefix(callback.Data, "admin_user_") ||
		callback.Data == "admin_users" {
		if b.handleAdminUserMgmtCallback(callback) {
			callbackConfig := tgbotapi.NewCallback(callback.ID, "")
			b.api.Request(callbackConfig)
			return
		}
	}
	if strings.HasPrefix(callback.Data, "admin_feed_report_") {
		suffix := strings.TrimPrefix(callback.Data, "admin_feed_report_")
		reportID, err := strconv.ParseInt(suffix, 10, 64)
		if err == nil && reportID > 0 {
			b.showAdminFeedReport(callback.Message.Chat.ID, reportID)
			callbackConfig := tgbotapi.NewCallback(callback.ID, "")
			b.api.Request(callbackConfig)
			return
		}
	}

	switch callback.Data {
	case "admin_open":
		b.openAdminPanelForUser(callback.Message.Chat.ID, callback.From.ID)
	case "admin_visit_stats":
		b.showAdminVisitStats(callback.Message.Chat.ID)
	case "admin_visit_recent":
		b.showAdminRecentVisits(callback.Message.Chat.ID)
	case "admin_support_inbox":
		b.showAdminSupportInbox(callback.Message.Chat.ID)
	case "admin_support_back":
		b.showAdminSupportInbox(callback.Message.Chat.ID)
	case "admin_feed_reports":
		b.showAdminFeedReportsInbox(callback.Message.Chat.ID)
	case "admin_feed_reports_back":
		b.showAdminFeedReportsInbox(callback.Message.Chat.ID)
	case "admin_mode_feed_text":
		b.startAdminFlow(callback.From.ID, "feed_text")
		b.api.Send(tgbotapi.NewMessage(callback.Message.Chat.ID, "📝 Напиши текст для ленты стаи. Дальше выберешь автора (Лео/Админ) и время публикации."))
	case "admin_feed_pub_leo":
		b.finishAdminFeedPostNow(callback, adminPostAuthorLeo)
	case "admin_feed_pub_admin":
		b.finishAdminFeedPostNow(callback, adminPostAuthorAdmin)
	case "admin_feed_sch_leo":
		b.startAdminFeedSchedule(callback, adminPostAuthorLeo)
	case "admin_feed_sch_admin":
		b.startAdminFeedSchedule(callback, adminPostAuthorAdmin)
	case "admin_sched_list":
		b.showAdminScheduledPosts(callback.Message.Chat.ID)
	case "admin_mode_poll":
		b.startAdminFlow(callback.From.ID, "poll")
		b.api.Send(tgbotapi.NewMessage(callback.Message.Chat.ID, "🗳 Напиши вопрос для опроса в ленте miniapp."))
	case "admin_wipe_pack_prompt":
		b.showAdminWipePackPrompt(callback.Message.Chat.ID)
	case "admin_wipe_pack_yes":
		b.executeAdminWipePack(callback.Message.Chat.ID)
	case "admin_cancel":
		b.clearAdminFlow(callback.From.ID)
		b.api.Send(tgbotapi.NewMessage(callback.Message.Chat.ID, "❎ Действие отменено."))
	}

	callbackConfig := tgbotapi.NewCallback(callback.ID, "")
	b.api.Request(callbackConfig)
}

func (b *Bot) startAdminFlow(userID int64, mode string) {
	b.adminSessionsMutex.Lock()
	defer b.adminSessionsMutex.Unlock()
	step := "await_text"
	if mode == "poll" {
		step = "await_poll_question"
	} else if mode == "feed_text" {
		step = "await_text"
	}
	b.adminSessions[userID] = &adminSession{
		Mode: mode,
		Step: step,
	}
}

func (b *Bot) startAdminSupportReply(userID, targetUserID int64) {
	b.adminSessionsMutex.Lock()
	defer b.adminSessionsMutex.Unlock()
	b.adminSessions[userID] = &adminSession{
		Mode:         "support",
		Step:         "await_support_text",
		TargetUserID: targetUserID,
	}
}

func (b *Bot) clearAdminFlow(userID int64) {
	b.adminSessionsMutex.Lock()
	defer b.adminSessionsMutex.Unlock()
	delete(b.adminSessions, userID)
}

func (b *Bot) getAdminSession(userID int64) (*adminSession, bool) {
	b.adminSessionsMutex.Lock()
	defer b.adminSessionsMutex.Unlock()
	s, ok := b.adminSessions[userID]
	return s, ok
}

func (b *Bot) handleAdminFlowMessage(msg *tgbotapi.Message) bool {
	if !b.isOwnerPrivateChat(msg) {
		return false
	}

	session, ok := b.getAdminSession(msg.From.ID)
	if !ok || session == nil {
		return false
	}

	// Управление сессией командами
	if msg.IsCommand() {
		switch msg.Command() {
		case "cancel":
			b.clearAdminFlow(msg.From.ID)
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "❎ Админ-действие отменено."))
		case "admin":
			b.openAdminPanelForUser(msg.Chat.ID, msg.From.ID)
		default:
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "⚠️ Сначала заверши текущий мастер или отправь /cancel"))
		}
		return true
	}

	if b.handleAdminUserMgmtMessage(msg, session) {
		return true
	}

	if b.handleTrackerApprovalFlowMessage(msg, session) {
		return true
	}

	switch session.Step {
	case "await_support_text":
		reply := strings.TrimSpace(msg.Text)
		if reply == "" {
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "⚠️ Ответ пустой. Отправь текст или /cancel."))
			return true
		}
		if err := b.AdminSupportReply(session.TargetUserID, reply, ""); err != nil {
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ Не удалось отправить ответ: "+err.Error()))
			return true
		}
		targetUserID := session.TargetUserID
		b.clearAdminFlow(msg.From.ID)
		b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "✅ Ответ отправлен пользователю."))
		b.showAdminSupportThread(msg.Chat.ID, targetUserID)
		return true

	case "await_text":
		text := strings.TrimSpace(msg.Text)
		if text == "" {
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "⚠️ Текст пустой. Отправь текст поста или /cancel."))
			return true
		}
		if session.Mode != "feed_text" {
			b.clearAdminFlow(msg.From.ID)
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ Неизвестный текстовый режим. Начни заново: /admin"))
			return true
		}
		session.FeedText = text
		session.Step = "await_post_options"
		b.showAdminFeedPostOptions(msg.Chat.ID)
		return true

	case "await_schedule_time":
		when, perr := parseAdminScheduleTime(strings.TrimSpace(msg.Text))
		if perr != nil {
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "⚠️ "+perr.Error()+"\n\nФормат: `ДД.ММ ЧЧ:ММ` (МСК), например `15.06 09:30`. Или /cancel."))
			return true
		}
		text := strings.TrimSpace(session.FeedText)
		if text == "" {
			b.clearAdminFlow(msg.From.ID)
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ Текст поста потерян. Начни заново: /admin"))
			return true
		}
		// Модерируем заранее — чтобы админ сразу увидел отказ, а не в момент публикации.
		if err := b.enforceAdminBroadcast(text, moderation.SurfaceAdminPost); err != nil {
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ Текст не прошёл модерацию: "+err.Error()))
			return true
		}
		if _, err := b.db.InsertScheduledAdminPost(b.config.MonetizedChatID, session.PostAuthor, text, when, msg.From.ID); err != nil {
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ Не удалось запланировать пост: "+err.Error()))
			return true
		}
		author := adminPostAuthorUsername(session.PostAuthor)
		b.clearAdminFlow(msg.From.ID)
		b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf(
			"✅ Пост запланирован на %s (МСК), от имени «%s». Управление: /admin → 📅 Отложенные.",
			when.In(moscowLocation()).Format("02.01 15:04"), author,
		)))
		return true

	case "await_poll_question":
		question := strings.TrimSpace(msg.Text)
		if question == "" {
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "⚠️ Вопрос пустой, отправь текст вопроса."))
			return true
		}
		if len([]rune(question)) > 300 {
			b.api.Send(tgbotapi.NewMessage(
				msg.Chat.ID,
				"⚠️ Вопрос слишком длинный для Telegram-опроса (макс. 300 символов). Сократи текст и отправь снова.",
			))
			return true
		}
		session.PollQuestion = question
		session.Step = "await_poll_options"
		b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "🗳 Отправь варианты через `|`, например:\nДа | Нет | Нужно доработать"))
		return true

	case "await_poll_options":
		raw := strings.Split(msg.Text, "|")
		options := make([]string, 0, len(raw))
		for _, opt := range raw {
			v := strings.TrimSpace(opt)
			if v != "" {
				options = append(options, v)
			}
		}
		if len(options) < 2 {
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "⚠️ Нужно минимум 2 варианта. Пример: Да | Нет"))
			return true
		}
		if len(options) > 10 {
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "⚠️ Максимум 10 вариантов."))
			return true
		}
		for _, opt := range options {
			if len([]rune(opt)) > 100 {
				b.api.Send(tgbotapi.NewMessage(
					msg.Chat.ID,
					"⚠️ Один из вариантов слишком длинный (макс. 100 символов). Сократи варианты и отправь снова.",
				))
				return true
			}
		}
		if err := b.saveAdminPollPackFeed(msg.From.ID, session.PollQuestion, options); err != nil {
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ Не удалось опубликовать опрос: "+err.Error()))
			return true
		}
		b.clearAdminFlow(msg.From.ID)
		b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "✅ Опрос опубликован в ленте miniapp."))
		return true
	}

	return false
}

func (b *Bot) showAdminSupportInbox(chatID int64) {
	if b == nil || b.db == nil || b.config == nil || b.config.MonetizedChatID == 0 {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Поддержка недоступна: не настроен pack chat."))
		return
	}
	items, err := b.db.ListMiniappSupportConversations(b.config.MonetizedChatID, 20)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Не удалось загрузить диалоги поддержки."))
		return
	}

	var text strings.Builder
	text.WriteString("💬 Поддержка\n\n")
	openReports := 0
	if n, err := b.db.CountOpenMiniappFeedReports(b.config.MonetizedChatID); err == nil {
		openReports = n
	}
	hiddenCount := 0
	if n, err := b.db.CountHiddenModerationItems(b.config.MonetizedChatID); err == nil {
		hiddenCount = n
	}
	if len(items) == 0 {
		text.WriteString("Диалогов пока нет.")
		rows := make([][]tgbotapi.InlineKeyboardButton, 0, 3)
		if openReports > 0 {
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(
					fmt.Sprintf("🚨 Жалобы на ленту (%d)", openReports),
					"admin_feed_reports",
				),
			))
		}
		if hiddenCount > 0 {
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(
					fmt.Sprintf("🙈 Скрытое (%d)", hiddenCount),
					"admin_hidden_inbox",
				),
			))
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ К админке", "admin_open"),
		))
		msg := tgbotapi.NewMessage(chatID, text.String())
		msg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
		b.api.Send(msg)
		return
	}

	text.WriteString("Последние диалоги:\n\n")
	rows := make([][]tgbotapi.InlineKeyboardButton, 0, len(items)+3)
	if openReports > 0 {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("🚨 Жалобы на ленту (%d)", openReports),
				"admin_feed_reports",
			),
		))
	}
	if hiddenCount > 0 {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("🙈 Скрытое (%d)", hiddenCount),
				"admin_hidden_inbox",
			),
		))
	}
	for i, item := range items {
		prefix := "•"
		if item.NeedsReply {
			prefix = "●"
		}
		text.WriteString(fmt.Sprintf("%d. %s %s\n", i+1, prefix, adminSupportTitle(item.DisplayName, item.UserID)))
		text.WriteString(fmt.Sprintf("   %s: %s\n\n", adminSupportRoleLabel(item.LastRole), clipAdminSupportText(item.LastText, 80)))
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				adminSupportButtonLabel(item.DisplayName, item.UserID, item.NeedsReply),
				"admin_support_user_"+strconv.FormatInt(item.UserID, 10),
			),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("⬅️ К админке", "admin_open"),
	))
	msg := tgbotapi.NewMessage(chatID, clipAdminSupportText(text.String(), 3500))
	msg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
	b.api.Send(msg)
}

func (b *Bot) showAdminSupportThread(chatID, targetUserID int64) {
	if b == nil || b.db == nil || b.config == nil || b.config.MonetizedChatID == 0 || targetUserID == 0 {
		return
	}
	items, err := b.AdminSupportChatHistory(targetUserID)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Не удалось загрузить диалог."))
		return
	}
	var text strings.Builder
	text.WriteString("💬 Диалог поддержки\n")
	text.WriteString(adminSupportTitle("", targetUserID))
	text.WriteString("\n\n")
	if len(items) == 0 {
		text.WriteString("Сообщений пока нет.")
	} else {
		for _, item := range items {
			if item == nil {
				continue
			}
			text.WriteString(adminSupportRoleLabel(item.Role))
			text.WriteString(": ")
			body := clipAdminSupportText(item.Text, 240)
			if body == "" && strings.TrimSpace(item.PhotoURL) != "" {
				body = "📷 Фото"
			} else if strings.TrimSpace(item.PhotoURL) != "" {
				body = body + " 📷"
			}
			text.WriteString(body)
			text.WriteString("\n\n")
		}
	}
	msg := tgbotapi.NewMessage(chatID, clipAdminSupportText(text.String(), 3500))
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✍️ Ответить", "admin_support_reply_"+strconv.FormatInt(targetUserID, 10)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ К диалогам", "admin_support_back"),
		),
	)
	b.api.Send(msg)
}

func adminSupportRoleLabel(role string) string {
	if role == "user" {
		return "Юзер"
	}
	return "Поддержка"
}

func adminSupportTitle(displayName string, userID int64) string {
	name := strings.TrimSpace(displayName)
	if name == "" {
		name = "user" + strconv.FormatInt(userID, 10)
	}
	return name + " · " + strconv.FormatInt(userID, 10)
}

func adminSupportButtonLabel(displayName string, userID int64, needsReply bool) string {
	prefix := "💬 "
	if needsReply {
		prefix = "🆕 "
	}
	return clipAdminSupportText(prefix+adminSupportTitle(displayName, userID), 56)
}

func (b *Bot) showAdminFeedReportsInbox(chatID int64) {
	if b == nil || b.db == nil || b.config == nil || b.config.MonetizedChatID == 0 {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Жалобы недоступны: не настроен pack chat."))
		return
	}
	items, err := b.db.ListOpenMiniappFeedReports(b.config.MonetizedChatID, 20)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Не удалось загрузить жалобы."))
		return
	}
	var text strings.Builder
	text.WriteString("🚨 Жалобы на контент\n\n")
	if len(items) == 0 {
		text.WriteString("Открытых жалоб нет.")
	} else {
		text.WriteString("Открытые жалобы:\n\n")
		for i, item := range items {
			if item == nil {
				continue
			}
			text.WriteString(fmt.Sprintf("%d. %s · %s\n", i+1, feedReportTargetLabel(item), feedReportPersonLabel(item.TargetName, item.TargetUserID)))
			text.WriteString(fmt.Sprintf("   От %s\n", feedReportPersonLabel(item.ReporterName, item.ReporterUserID)))
			text.WriteString(fmt.Sprintf("   «%s»\n\n", clipAdminSupportText(item.TargetText, 72)))
		}
	}
	rows := make([][]tgbotapi.InlineKeyboardButton, 0, len(items)+1)
	for _, item := range items {
		if item == nil {
			continue
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("#%d · %s", item.ID, clipAdminSupportText(feedReportTargetLabel(item), 12)),
				"admin_feed_report_"+strconv.FormatInt(item.ID, 10),
			),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("⬅️ К поддержке", "admin_support_back"),
	))
	msg := tgbotapi.NewMessage(chatID, clipAdminSupportText(text.String(), 3500))
	msg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
	b.api.Send(msg)
}

func (b *Bot) showAdminFeedReport(chatID, reportID int64) {
	if b == nil || b.db == nil || b.config == nil || b.config.MonetizedChatID == 0 || reportID == 0 {
		return
	}
	item, err := b.db.GetMiniappFeedReport(b.config.MonetizedChatID, reportID)
	if err != nil || item == nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Жалоба не найдена."))
		return
	}
	var text strings.Builder
	text.WriteString("🚨 Жалоба #")
	text.WriteString(strconv.FormatInt(item.ID, 10))
	text.WriteString("\n\n")
	text.WriteString("Тип: ")
	text.WriteString(feedReportTargetLabel(item))
	text.WriteString("\n")
	text.WriteString("Кто пожаловался: ")
	text.WriteString(feedReportPersonLabel(item.ReporterName, item.ReporterUserID))
	text.WriteString("\n")
	text.WriteString("На кого: ")
	text.WriteString(feedReportPersonLabel(item.TargetName, item.TargetUserID))
	text.WriteString("\n")
	if item.TargetType == "pack_group_message" {
		text.WriteString("Сообщение в чате: #")
		text.WriteString(strconv.FormatInt(item.UserMessageID, 10))
		text.WriteString("\n")
	} else {
		text.WriteString("Отчёт в ленте: #")
		text.WriteString(strconv.FormatInt(item.UserMessageID, 10))
		text.WriteString("\n")
	}
	if item.ThreadReplyID > 0 {
		text.WriteString("Комментарий: #")
		text.WriteString(strconv.FormatInt(item.ThreadReplyID, 10))
		text.WriteString("\n")
	}
	text.WriteString("\nТекст:\n«")
	text.WriteString(clipAdminSupportText(item.TargetText, 900))
	text.WriteString("»")
	reportTargetMuted := false
	if item.TargetUserID > 0 {
		if ugc, err := b.db.GetUGCModerationState(item.TargetUserID, b.config.MonetizedChatID); err == nil {
			text.WriteString(fmt.Sprintf("\n\nUGC-нарушения автора: %d", ugc.ViolationCount))
			if ugc.MutedUntil != nil && ugc.MutedUntil.After(time.Now()) {
				reportTargetMuted = true
				text.WriteString(fmt.Sprintf("\nUGC-мьют до: %s", ugc.MutedUntil.UTC().Format("2006-01-02 15:04 UTC")))
			}
		}
	}
	msg := tgbotapi.NewMessage(chatID, clipAdminSupportText(text.String(), 3500))
	if item.Status == "open" {
		muteBtn := tgbotapi.NewInlineKeyboardButtonData("🔇 Mute 24ч", "admin_feed_report_mute_"+strconv.FormatInt(item.ID, 10))
		if reportTargetMuted {
			muteBtn = tgbotapi.NewInlineKeyboardButtonData("🔊 Unmute", "admin_feed_report_unmute_"+strconv.FormatInt(item.ID, 10))
		}
		msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("🙈 Скрыть", "admin_feed_report_hide_"+strconv.FormatInt(item.ID, 10)),
				muteBtn,
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ Обработано", "admin_feed_report_dismiss_"+strconv.FormatInt(item.ID, 10)),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("⬅️ К жалобам", "admin_feed_reports_back"),
			),
		)
	} else {
		msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("⬅️ К жалобам", "admin_feed_reports_back"),
			),
		)
	}
	b.api.Send(msg)
}

func (b *Bot) dismissAdminFeedReport(resolverUserID, chatID, reportID int64, triggerMsg *tgbotapi.Message, fromPanel bool) string {
	if b == nil || b.db == nil || b.config == nil || b.config.MonetizedChatID == 0 {
		return ""
	}
	ok, err := b.db.DismissMiniappFeedReport(b.config.MonetizedChatID, reportID)
	if err != nil {
		if fromPanel {
			b.api.Send(tgbotapi.NewMessage(chatID, "❌ Не удалось закрыть жалобу."))
		}
		return "Не удалось закрыть жалобу"
	}
	if !ok {
		if fromPanel {
			b.api.Send(tgbotapi.NewMessage(chatID, "Жалоба уже обработана или не найдена."))
		}
		return "Жалоба уже обработана"
	}
	targetUserID := int64(0)
	if item, err := b.db.GetMiniappFeedReport(b.config.MonetizedChatID, reportID); err == nil && item != nil {
		targetUserID = item.TargetUserID
	}
	b.trackReportResolved(reportID, resolverUserID, targetUserID, "no_action")
	b.markFeedReportNotifyMessagesResolved(reportID, resolverUserID, triggerMsg)
	if fromPanel {
		if triggerMsg != nil {
			detailText := clipAdminSupportText(triggerMsg.Text+"\n\n✅ Обработано · "+b.supportDisplayName(resolverUserID), 3500)
			edit := tgbotapi.NewEditMessageText(triggerMsg.Chat.ID, triggerMsg.MessageID, detailText)
			edit.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{
				InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("⬅️ К жалобам", "admin_feed_reports_back"),
					),
				},
			}
			if _, err := b.api.Send(edit); err != nil {
				b.logger.Warnf("feed report panel dismiss edit report=%d: %v", reportID, err)
			}
		}
		b.api.Send(tgbotapi.NewMessage(chatID, "✅ Жалоба отмечена обработанной."))
		b.showAdminFeedReportsInbox(chatID)
		return ""
	}
	return "✅ Отмечено решённым"
}

// ─── Visit stats (доступно всем админам) ─────────────────────────────────────

func (b *Bot) showAdminVisitStats(chatID int64) {
	stats, err := b.db.GetBotVisitStats()
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Не удалось загрузить статистику: "+err.Error()))
		return
	}

	var sb strings.Builder
	sb.WriteString("📊 Статистика посещений бота\n\n")
	sb.WriteString(fmt.Sprintf("Всего визитов:     %d\n", stats.TotalVisits))
	sb.WriteString(fmt.Sprintf("Уникальных юзеров: %d\n", stats.UniqueUsers))
	sb.WriteString(fmt.Sprintf("Сегодня:           %d\n", stats.TodayVisits))
	sb.WriteString(fmt.Sprintf("За 7 дней:         %d\n", stats.WeekVisits))
	sb.WriteString(fmt.Sprintf("За 30 дней:        %d\n", stats.MonthVisits))

	if len(stats.TopVisitors) > 0 {
		sb.WriteString("\n🏆 Топ по визитам:\n")
		for i, u := range stats.TopVisitors {
			name := adminVisitDisplayName(u.Username, u.FirstName, u.UserID)
			sb.WriteString(fmt.Sprintf("%d. %s — %d\n", i+1, name, u.Visits))
		}
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🕓 Последние визиты", "admin_visit_recent"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ К админке", "admin_open"),
		),
	)
	msg := tgbotapi.NewMessage(chatID, sb.String())
	msg.ReplyMarkup = keyboard
	b.api.Send(msg)
}

func (b *Bot) showAdminRecentVisits(chatID int64) {
	visits, err := b.db.GetRecentBotVisits(30)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Не удалось загрузить визиты: "+err.Error()))
		return
	}

	var sb strings.Builder
	sb.WriteString("🕓 Последние 30 визитов\n\n")
	if len(visits) == 0 {
		sb.WriteString("Визитов пока нет.")
	} else {
		for i, v := range visits {
			name := adminVisitDisplayName(v.Username, v.FirstName, v.UserID)
			sb.WriteString(fmt.Sprintf("%d. %s · %s\n",
				i+1, name, v.VisitedAt.Format("02.01 15:04")))
		}
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ К статистике", "admin_visit_stats"),
		),
	)
	msg := tgbotapi.NewMessage(chatID, clipAdminSupportText(sb.String(), 3800))
	msg.ReplyMarkup = keyboard
	b.api.Send(msg)
}

func adminVisitDisplayName(username, firstName string, userID int64) string {
	if username != "" {
		return "@" + username
	}
	if firstName != "" {
		return firstName
	}
	return strconv.FormatInt(userID, 10)
}

func clipAdminSupportText(s string, limit int) string {
	s = strings.TrimSpace(s)
	if limit <= 0 || len([]rune(s)) <= limit {
		return s
	}
	r := []rune(s)
	if limit <= 1 {
		return string(r[:limit])
	}
	return string(r[:limit-1]) + "…"
}

