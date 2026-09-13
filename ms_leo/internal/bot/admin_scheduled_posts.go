package bot

import (
	"context"
	"fmt"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// moscowLocation — Europe/Moscow с фоллбэком на фиксированный UTC+3.
func moscowLocation() *time.Location {
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		return time.FixedZone("MSK", 3*60*60)
	}
	return loc
}

// parseAdminScheduleTime разбирает «ДД.ММ ЧЧ:ММ» (или «ДД.ММ.ГГГГ ЧЧ:ММ») в МСК.
// Если год не указан и дата уже прошла — переносим на следующий год. Время должно быть в будущем.
func parseAdminScheduleTime(s string) (time.Time, error) {
	loc := moscowLocation()
	now := time.Now().In(loc)
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("пустое время")
	}
	// Полный формат с годом.
	if t, err := time.ParseInLocation("02.01.2006 15:04", s, loc); err == nil {
		if !t.After(now) {
			return time.Time{}, fmt.Errorf("это время уже прошло")
		}
		return t, nil
	}
	// Короткий формат без года.
	t, err := time.ParseInLocation("02.01 15:04", s, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("не понял дату/время")
	}
	t = time.Date(now.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, loc)
	if !t.After(now) {
		t = t.AddDate(1, 0, 0)
	}
	return t, nil
}

// showAdminFeedPostOptions — выбор автора и времени публикации после ввода текста.
func (b *Bot) showAdminFeedPostOptions(chatID int64) {
	rows := [][]tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🐆 Сейчас от Лео", "admin_feed_pub_leo"),
			tgbotapi.NewInlineKeyboardButtonData("👤 Сейчас от Админа", "admin_feed_pub_admin"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⏰ Отложить (Лео)", "admin_feed_sch_leo"),
			tgbotapi.NewInlineKeyboardButtonData("⏰ Отложить (Админ)", "admin_feed_sch_admin"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❎ Отмена", "admin_cancel"),
		),
	}
	msg := tgbotapi.NewMessage(chatID, "Как опубликовать пост?")
	msg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
	b.api.Send(msg)
}

// finishAdminFeedPostNow — немедленная публикация черновика поста от выбранного автора.
func (b *Bot) finishAdminFeedPostNow(callback *tgbotapi.CallbackQuery, author string) {
	chatID := callback.Message.Chat.ID
	session, ok := b.getAdminSession(callback.From.ID)
	if !ok || session == nil || session.Mode != "feed_text" || strings.TrimSpace(session.FeedText) == "" {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Черновик поста не найден. Начни заново: /admin"))
		return
	}
	text := session.FeedText
	if err := b.saveAdminCustomPackFeed(callback.From.ID, author, text); err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Не удалось опубликовать пост: "+err.Error()))
		return
	}
	b.clearAdminFlow(callback.From.ID)
	b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Пост опубликован в ленте стаи от имени «%s».", adminPostAuthorUsername(author))))
}

// startAdminFeedSchedule — запрашивает дату/время для отложенной публикации черновика.
func (b *Bot) startAdminFeedSchedule(callback *tgbotapi.CallbackQuery, author string) {
	chatID := callback.Message.Chat.ID
	session, ok := b.getAdminSession(callback.From.ID)
	if !ok || session == nil || session.Mode != "feed_text" || strings.TrimSpace(session.FeedText) == "" {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Черновик поста не найден. Начни заново: /admin"))
		return
	}
	session.PostAuthor = author
	session.Step = "await_schedule_time"
	b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf(
		"⏰ Когда опубликовать (от имени «%s»)?\nПришли дату и время в формате `ДД.ММ ЧЧ:ММ` (МСК), например `15.06 09:30`.",
		adminPostAuthorUsername(author),
	)))
}

// showAdminScheduledPosts — список запланированных постов с кнопками отмены.
func (b *Bot) showAdminScheduledPosts(chatID int64) {
	if b.config == nil || b.config.MonetizedChatID == 0 {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Лента стаи не настроена."))
		return
	}
	posts, err := b.db.ListPendingScheduledAdminPosts(b.config.MonetizedChatID, 20)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Не удалось загрузить отложенные посты."))
		return
	}
	if len(posts) == 0 {
		b.api.Send(tgbotapi.NewMessage(chatID, "📅 Отложенных постов нет."))
		return
	}
	loc := moscowLocation()
	var text strings.Builder
	text.WriteString("📅 Отложенные посты:\n\n")
	rows := make([][]tgbotapi.InlineKeyboardButton, 0, len(posts))
	for _, p := range posts {
		preview := truncateForDM(p.MessageText, 80)
		when := p.ScheduledAt.In(loc).Format("02.01 15:04")
		text.WriteString(fmt.Sprintf("• %s · «%s» · %s\n«%s»\n\n", when, adminPostAuthorUsername(p.Author), "МСК", preview))
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("🗑 Отменить %s", when),
				fmt.Sprintf("admin_sched_cancel_%d", p.ID),
			),
		))
	}
	msg := tgbotapi.NewMessage(chatID, text.String())
	msg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
	b.api.Send(msg)
}

// cancelAdminScheduledPost — отмена запланированного поста по id.
func (b *Bot) cancelAdminScheduledPost(chatID, postID int64) {
	if b.config == nil || b.config.MonetizedChatID == 0 {
		return
	}
	ok, err := b.db.CancelScheduledAdminPost(b.config.MonetizedChatID, postID)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Не удалось отменить пост."))
		return
	}
	if !ok {
		b.api.Send(tgbotapi.NewMessage(chatID, "⚠️ Пост уже опубликован или отменён."))
		return
	}
	b.api.Send(tgbotapi.NewMessage(chatID, "✅ Отложенный пост отменён."))
	b.showAdminScheduledPosts(chatID)
}

// startScheduledAdminPostsWorker — раз в минуту публикует созревшие отложенные посты.
func (b *Bot) startScheduledAdminPostsWorker(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.publishDueScheduledAdminPosts()
		}
	}
}

func (b *Bot) publishDueScheduledAdminPosts() {
	if b == nil || b.db == nil || b.config == nil || b.config.MonetizedChatID == 0 {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			b.logger.Errorf("scheduled admin posts worker panic: %v", r)
		}
	}()
	due, err := b.db.ClaimDueScheduledAdminPosts(time.Now())
	if err != nil {
		b.logger.Warnf("claim due scheduled admin posts: %v", err)
		return
	}
	for _, p := range due {
		if p.ChatID != b.config.MonetizedChatID {
			continue
		}
		if _, err := b.publishAdminPackFeedPost(p.CreatedBy, p.Author, p.MessageText); err != nil {
			b.logger.Errorf("publish scheduled admin post id=%d: %v", p.ID, err)
			continue
		}
		b.logger.Infof("scheduled admin post id=%d published (author=%s, by=%d)", p.ID, p.Author, p.CreatedBy)
	}
}
