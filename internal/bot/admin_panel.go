package bot

import (
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type adminSession struct {
	Mode        string // text | photo | video | poll
	Step        string // await_chat_id | await_text | await_photo | await_video | await_poll_question | await_poll_options
	TargetChatID int64
	PollQuestion string
}

func (b *Bot) isOwnerPrivateChat(msg *tgbotapi.Message) bool {
	return msg != nil && msg.From != nil && msg.Chat != nil && msg.From.ID == b.config.OwnerID && msg.Chat.IsPrivate()
}

func (b *Bot) handleAdmin(msg *tgbotapi.Message) {
	if !b.isOwnerPrivateChat(msg) {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Админ-панель доступна только владельцу в личном чате с ботом.")
		b.api.Send(reply)
		return
	}
	b.showAdminMenu(msg.Chat.ID)
}

func (b *Bot) showAdminMenu(chatID int64) {
	text := "⚙️ Админ-панель\n\nВыбери действие:"
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📝 Текст", "admin_mode_text"),
			tgbotapi.NewInlineKeyboardButtonData("🖼 Фото", "admin_mode_photo"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🎬 Видео", "admin_mode_video"),
			tgbotapi.NewInlineKeyboardButtonData("🗳 Опрос", "admin_mode_poll"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❎ Отмена", "admin_cancel"),
		),
	)
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = keyboard
	b.api.Send(msg)
}

func (b *Bot) handleAdminCallbackQuery(callback *tgbotapi.CallbackQuery) {
	if callback == nil || callback.Message == nil || callback.From == nil {
		return
	}
	if callback.From.ID != b.config.OwnerID || !callback.Message.Chat.IsPrivate() {
		callbackConfig := tgbotapi.NewCallback(callback.ID, "Недостаточно прав")
		b.api.Request(callbackConfig)
		return
	}

	switch callback.Data {
	case "admin_open":
		b.showAdminMenu(callback.Message.Chat.ID)
	case "admin_mode_text":
		b.startAdminFlow(callback.From.ID, "text")
		b.api.Send(tgbotapi.NewMessage(callback.Message.Chat.ID, "📝 Введи chat_id для отправки текста."))
	case "admin_mode_photo":
		b.startAdminFlow(callback.From.ID, "photo")
		b.api.Send(tgbotapi.NewMessage(callback.Message.Chat.ID, "🖼 Введи chat_id для отправки фото."))
	case "admin_mode_video":
		b.startAdminFlow(callback.From.ID, "video")
		b.api.Send(tgbotapi.NewMessage(callback.Message.Chat.ID, "🎬 Введи chat_id для отправки видео."))
	case "admin_mode_poll":
		b.startAdminFlow(callback.From.ID, "poll")
		b.api.Send(tgbotapi.NewMessage(callback.Message.Chat.ID, "🗳 Введи chat_id для опроса."))
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
	b.adminSessions[userID] = &adminSession{
		Mode: mode,
		Step: "await_chat_id",
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
			b.showAdminMenu(msg.Chat.ID)
		default:
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "⚠️ Сначала заверши текущий мастер или отправь /cancel"))
		}
		return true
	}

	switch session.Step {
	case "await_chat_id":
		chatID, err := parseAdminChatID(msg.Text)
		if err != nil {
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ Неверный chat_id. Пример: -1003246054143"))
			return true
		}
		session.TargetChatID = chatID
		switch session.Mode {
		case "text":
			session.Step = "await_text"
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "✍️ Теперь отправь текст сообщения."))
		case "photo":
			session.Step = "await_photo"
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "🖼 Теперь отправь фото (подпись можно добавить сразу)."))
		case "video":
			session.Step = "await_video"
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "🎬 Теперь отправь видео (подпись можно добавить сразу)."))
		case "poll":
			session.Step = "await_poll_question"
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "❓ Отправь вопрос для опроса."))
		default:
			b.clearAdminFlow(msg.From.ID)
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ Неизвестный режим. Начни заново: /admin"))
		}
		return true

	case "await_text":
		out := tgbotapi.NewMessage(session.TargetChatID, msg.Text)
		if _, err := b.api.Send(out); err != nil {
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ Не удалось отправить текст: "+err.Error()))
			return true
		}
		b.clearAdminFlow(msg.From.ID)
		b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "✅ Текст отправлен."))
		return true

	case "await_photo":
		if len(msg.Photo) == 0 {
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "⚠️ Ожидаю фото. Отправь именно фото-сообщение."))
			return true
		}
		photo := msg.Photo[len(msg.Photo)-1]
		out := tgbotapi.NewPhoto(session.TargetChatID, tgbotapi.FileID(photo.FileID))
		out.Caption = msg.Caption
		if _, err := b.api.Send(out); err != nil {
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ Не удалось отправить фото: "+err.Error()))
			return true
		}
		b.clearAdminFlow(msg.From.ID)
		b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "✅ Фото отправлено."))
		return true

	case "await_video":
		if msg.Video == nil {
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "⚠️ Ожидаю видео. Отправь именно видео-сообщение."))
			return true
		}
		out := tgbotapi.NewVideo(session.TargetChatID, tgbotapi.FileID(msg.Video.FileID))
		out.Caption = msg.Caption
		if _, err := b.api.Send(out); err != nil {
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ Не удалось отправить видео: "+err.Error()))
			return true
		}
		b.clearAdminFlow(msg.From.ID)
		b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "✅ Видео отправлено."))
		return true

	case "await_poll_question":
		question := strings.TrimSpace(msg.Text)
		if question == "" {
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "⚠️ Вопрос пустой, отправь текст вопроса."))
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
		poll := tgbotapi.NewPoll(session.TargetChatID, session.PollQuestion, options...)
		if _, err := b.api.Send(poll); err != nil {
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ Не удалось отправить опрос: "+err.Error()))
			return true
		}
		b.clearAdminFlow(msg.From.ID)
		b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "✅ Опрос отправлен."))
		return true
	}

	return false
}

func parseAdminChatID(raw string) (int64, error) {
	idRaw := strings.TrimSpace(raw)
	idRaw = strings.ReplaceAll(idRaw, "–", "-")
	idRaw = strings.ReplaceAll(idRaw, "—", "-")
	idRaw = strings.ReplaceAll(idRaw, "\u00A0", " ")

	var filtered strings.Builder
	for i, r := range idRaw {
		if i == 0 && r == '-' {
			filtered.WriteRune(r)
			continue
		}
		if r >= '0' && r <= '9' {
			filtered.WriteRune(r)
		}
	}
	return strconv.ParseInt(filtered.String(), 10, 64)
}
