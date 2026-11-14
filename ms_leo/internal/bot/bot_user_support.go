package bot

import (
	"strings"

	"leo-bot/internal/database"
	"leo-bot/internal/i18n"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const botSupportCallbackStart = "bot_support_start"
const botSupportCallbackCancel = "bot_support_cancel"

const botSupportReplyButtonText = "💬 Support"
const botSupportReplyButtonTextRU = "💬 Поддержка"
const botAdminReplyButtonText = "⚙️ Админ-панель"

func (b *Bot) botSupportAvailable() bool {
	return b != nil && b.config != nil && b.config.MonetizedChatID != 0 && b.db != nil
}

func (b *Bot) isAdminTelegramUser(userID int64) bool {
	if b == nil || b.config == nil {
		return false
	}
	if b.config.IsAdminTelegramUser(userID) {
		return true
	}
	b.dynamicAdminsMu.RLock()
	_, ok := b.dynamicAdmins[userID]
	b.dynamicAdminsMu.RUnlock()
	return ok
}

// IsMiniappViewerAdmin — экспорт для miniapp API (владелец, ADMIN_IDS, динамические админы).
func (b *Bot) IsMiniappViewerAdmin(userID int64) bool {
	return b.isAdminTelegramUser(userID)
}

// reloadDynamicAdmins — перезагружает кэш динамических администраторов из БД.
func (b *Bot) reloadDynamicAdmins() {
	if b == nil || b.db == nil {
		return
	}
	ids, err := b.db.LoadDynamicAdminIDs()
	if err != nil {
		if b.logger != nil {
			b.logger.Warnf("reloadDynamicAdmins: %v", err)
		}
		return
	}
	m := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		m[id] = struct{}{}
	}
	b.dynamicAdminsMu.Lock()
	b.dynamicAdmins = m
	b.dynamicAdminsMu.Unlock()
}

// privateBottomReplyKeyboard — постоянные кнопки внизу лички с ботом.
func (b *Bot) privateBottomReplyKeyboard(userID int64) *tgbotapi.ReplyKeyboardMarkup {
	if userID <= 0 {
		return nil
	}
	if !b.isAdminTelegramUser(userID) && !b.botSupportAvailable() {
		return nil
	}
	var rows [][]tgbotapi.KeyboardButton
	if b.isAdminTelegramUser(userID) {
		rows = append(rows, tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton(botAdminReplyButtonText),
		))
	}
	if b.botSupportAvailable() {
		rows = append(rows, tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton(b.userText(userID, botSupportReplyButtonTextRU, botSupportReplyButtonText)),
		))
	}
	if len(rows) == 0 {
		return nil
	}
	kb := tgbotapi.ReplyKeyboardMarkup{
		Keyboard:        rows,
		ResizeKeyboard:  true,
		OneTimeKeyboard: false,
	}
	return &kb
}

// syncPrivateBottomKeyboard обновляет reply-клавиатуру внизу (без лишних сообщений при повторе).
func (b *Bot) syncPrivateBottomKeyboard(chatID, userID int64) {
	if chatID == 0 || userID == 0 {
		return
	}
	kb := b.privateBottomReplyKeyboard(userID)
	if kb == nil {
		return
	}
	kind := "support"
	switch {
	case b.isAdminTelegramUser(userID) && b.botSupportAvailable():
		kind = "admin+support"
	case b.isAdminTelegramUser(userID):
		kind = "admin"
	}
	// Язык в ключе: после смены ui_lang кнопка «Поддержка/Support» перерисуется.
	kind += ":" + string(i18n.Or(b.EffectiveUILang(userID), i18n.EN))
	if prev, ok := b.privateBottomKeyboardKind.Load(userID); ok && prev == kind {
		return
	}
	b.privateBottomKeyboardKind.Store(userID, kind)
	msg := tgbotapi.NewMessage(chatID, " ")
	msg.ReplyMarkup = kb
	_, _ = b.api.Send(msg)
}

// openAdminPanelForUser — админ-панель + гарантированная кнопка «Admin» внизу.
func (b *Bot) openAdminPanelForUser(chatID, userID int64) {
	b.syncPrivateBottomKeyboard(chatID, userID)
	b.showAdminMenuForUser(chatID)
}

func (b *Bot) botSupportPromptInlineKeyboard(lang i18n.Lang) *tgbotapi.InlineKeyboardMarkup {
	return &tgbotapi.InlineKeyboardMarkup{
		InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(pickLang(lang, "❎ Выйти из поддержки", "❎ Exit support"), botSupportCallbackCancel),
			),
		},
	}
}

func botSupportPromptText(lang i18n.Lang) string {
	if socialRU(lang) {
		return `💬 Поддержка

Опиши вопрос об оплате, доступе после оплаты, возврате или ошибке в боте — одним или несколькими сообщениями.

Ответ придёт в этот же чат. Выйти — кнопка «Выйти из поддержки» ниже.`
	}
	return `💬 Support

Describe a question about payment, access after payment, return, or a bot error — in one or more messages.

We'll reply in this chat. To exit — tap «Exit support» below.`
}

func (b *Bot) startUserSupportSession(userID int64) {
	if !b.botSupportAvailable() || userID == 0 {
		return
	}
	b.userSupportSessionsMutex.Lock()
	defer b.userSupportSessionsMutex.Unlock()
	if b.userSupportSessions == nil {
		b.userSupportSessions = make(map[int64]struct{})
	}
	b.userSupportSessions[userID] = struct{}{}
}

func (b *Bot) clearUserSupportSession(userID int64) bool {
	if userID == 0 {
		return false
	}
	b.userSupportSessionsMutex.Lock()
	defer b.userSupportSessionsMutex.Unlock()
	if b.userSupportSessions == nil {
		return false
	}
	if _, ok := b.userSupportSessions[userID]; !ok {
		return false
	}
	delete(b.userSupportSessions, userID)
	return true
}

func (b *Bot) userInSupportSession(userID int64) bool {
	if userID == 0 {
		return false
	}
	b.userSupportSessionsMutex.Lock()
	defer b.userSupportSessionsMutex.Unlock()
	if b.userSupportSessions == nil {
		return false
	}
	_, ok := b.userSupportSessions[userID]
	return ok
}

func supportUnavailableText(lang i18n.Lang) string {
	return pickLang(lang, "⚠️ Поддержка временно недоступна. Попробуй позже.", "⚠️ Support is temporarily unavailable. Try later.")
}

// sendUserSupportPrompt — chatID лички совпадает с userID получателя.
func (b *Bot) sendUserSupportPrompt(chatID int64) {
	lang := b.EffectiveUILang(chatID)
	if !b.botSupportAvailable() {
		_, _ = b.api.Send(tgbotapi.NewMessage(chatID, supportUnavailableText(lang)))
		return
	}
	msg := tgbotapi.NewMessage(chatID, botSupportPromptText(lang))
	msg.ReplyMarkup = b.botSupportPromptInlineKeyboard(lang)
	_, _ = b.api.Send(msg)
}

func (b *Bot) handleBotSupportCallback(callback *tgbotapi.CallbackQuery) {
	if callback == nil || callback.From == nil || callback.Message == nil {
		return
	}
	answer := tgbotapi.NewCallback(callback.ID, "")
	lang := b.EffectiveUILang(callback.From.ID)
	switch callback.Data {
	case botSupportCallbackStart:
		b.startUserSupportSession(callback.From.ID)
		b.sendUserSupportPrompt(callback.Message.Chat.ID)
		answer.Text = pickLang(lang, "Напиши вопрос в чат", "Type your question in the chat")
		b.db.TrackEvent(database.AnalyticsEvent{
			Name:       database.EventSupportButtonClicked,
			TelegramID: callback.From.ID,
			Payload:    map[string]any{"from": "bot"},
		})
	case botSupportCallbackCancel:
		if b.clearUserSupportSession(callback.From.ID) {
			answer.Text = pickLang(lang, "Вышли из поддержки", "Left support")
			m := tgbotapi.NewMessage(callback.Message.Chat.ID, pickLang(lang, "❎ Режим поддержки выключен.", "❎ Support mode off."))
			b.syncPrivateBottomKeyboard(callback.Message.Chat.ID, callback.From.ID)
			_, _ = b.api.Send(m)
		} else {
			answer.Text = pickLang(lang, "Режим поддержки не был активен", "Support mode wasn't active")
		}
	default:
		return
	}
	_, _ = b.api.Request(answer)
}

func isSupportReplyButtonText(text string) bool {
	t := strings.TrimSpace(text)
	return t == botSupportReplyButtonText || t == botSupportReplyButtonTextRU ||
		strings.EqualFold(t, "Support") || strings.EqualFold(t, "Поддержка")
}

func isAdminReplyButtonText(text string) bool {
	t := strings.TrimSpace(text)
	switch t {
	case botAdminReplyButtonText, "⚙️ Админка", "Админ-панель", "Админка",
		// Кнопка, которую успели выдать пока админка была на английском.
		"⚙️ Admin panel", "⚙️ Admin", "Admin panel", "Admin":
		return true
	default:
		return false
	}
}

// handleUserSupportFlowMessage — личка: режим поддержки (до мини-аппа), без ответа Лео.
func (b *Bot) handleUserSupportFlowMessage(msg *tgbotapi.Message) bool {
	if msg == nil || msg.From == nil || msg.Chat == nil || !msg.Chat.IsPrivate() {
		return false
	}

	text := strings.TrimSpace(msg.Text)
	lang := b.EffectiveUILang(msg.From.ID)
	if msg.IsCommand() {
		if b.userInSupportSession(msg.From.ID) {
			_, _ = b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, pickLang(lang,
				"⚠️ Сейчас активна поддержка. Напиши текст вопроса или нажми «Выйти из поддержки».",
				"⚠️ Support mode is on. Type your question or tap «Exit support».")))
			return true
		}
		return false
	}

	if b.isAdminTelegramUser(msg.From.ID) && isAdminReplyButtonText(text) {
		b.openAdminPanelForUser(msg.Chat.ID, msg.From.ID)
		return true
	}

	if isSupportReplyButtonText(text) {
		if !b.botSupportAvailable() {
			_, _ = b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, supportUnavailableText(lang)))
			return true
		}
		b.startUserSupportSession(msg.From.ID)
		b.sendUserSupportPrompt(msg.Chat.ID)
		return true
	}

	if !b.botSupportAvailable() || !b.userInSupportSession(msg.From.ID) {
		return false
	}

	if text == "" && msg.Caption != "" {
		text = strings.TrimSpace(msg.Caption)
	}
	if text == "" {
		_, _ = b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, pickLang(lang, "⚠️ Отправь вопрос текстом (оплата, доступ, ошибка).", "⚠️ Send your question as text (billing, access, bug).")))
		return true
	}

	if err := b.MiniappSupportSendFromUser(msg.From.ID, text, ""); err != nil {
		b.logger.Warnf("bot support send user=%d: %v", msg.From.ID, err)
		_, _ = b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, pickLang(lang, "❌ Не удалось отправить. Попробуй ещё раз.", "❌ Couldn't send. Please try again.")))
		return true
	}

	_, _ = b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, pickLang(lang, "✅ Отправлено в поддержку. Ответ придёт сюда. Можешь дописать уточнение.", "✅ Sent to support. The reply will appear here. You can add more details anytime.")))
	return true
}
