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

// parseAdminScheduleTime разбирает «DD.MM HH:MM» (или «ДД.ММ.ГГГГ ЧЧ:ММ») в МСК.
// Если год не указан и дата уже прошла — переносим на следующий год. Время должно быть в будущем.
func parseAdminScheduleTime(s string) (time.Time, error) {
	loc := moscowLocation()
	now := time.Now().In(loc)
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	// Полный формат с годом.
	if t, err := time.ParseInLocation("02.01.2006 15:04", s, loc); err == nil {
		if !t.After(now) {
			return time.Time{}, fmt.Errorf("time is in the past")
		}
		return t, nil
	}
	// Короткий формат без года.
	t, err := time.ParseInLocation("02.01 15:04", s, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("could not parse date/time")
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
			tgbotapi.NewInlineKeyboardButtonData("🐆 Publish now as Leo", "admin_feed_pub_leo"),
			tgbotapi.NewInlineKeyboardButtonData("👤 Publish now as Admin", "admin_feed_pub_admin"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⏰ Schedule (Leo)", "admin_feed_sch_leo"),
			tgbotapi.NewInlineKeyboardButtonData("⏰ Schedule (Admin)", "admin_feed_sch_admin"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❎ Cancel", "admin_cancel"),
		),
	}
	msg := tgbotapi.NewMessage(chatID, "How to publish the post?")
	msg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
	b.api.Send(msg)
}

// finishAdminFeedPostNow — немедленная публикация черновика поста от выбранного автора.
func (b *Bot) finishAdminFeedPostNow(callback *tgbotapi.CallbackQuery, author string) {
	chatID := callback.Message.Chat.ID
	session, ok := b.getAdminSession(callback.From.ID)
	if !ok || session == nil || session.Mode != "feed_text" || strings.TrimSpace(session.FeedText) == "" {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Post draft not found. Start over: /admin"))
		return
	}
	text := session.FeedText
	if err := b.saveAdminCustomPackFeed(callback.From.ID, author, text); err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Could not publish post: "+err.Error()))
		return
	}
	b.clearAdminFlow(callback.From.ID)
	b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Post published to pack feed as «%s».", adminPostAuthorUsername(author))))
}

// startAdminFeedSchedule — запрашивает дату/время для отложенной публикации черновика.
func (b *Bot) startAdminFeedSchedule(callback *tgbotapi.CallbackQuery, author string) {
	chatID := callback.Message.Chat.ID
	session, ok := b.getAdminSession(callback.From.ID)
	if !ok || session == nil || session.Mode != "feed_text" || strings.TrimSpace(session.FeedText) == "" {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Post draft not found. Start over: /admin"))
		return
	}
	session.PostAuthor = author
	session.Step = "await_schedule_time"
	b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf(
		"⏰ When to publish (as «%s»)?\nSend date/time as `DD.MM HH:MM` (MSK), e.g. `15.06 09:30`.",
		adminPostAuthorUsername(author),
	)))
}

// showAdminScheduledPosts — список запланированных постов с кнопками отмены.
func (b *Bot) showAdminScheduledPosts(chatID int64) {
	if b.config == nil || b.config.MonetizedChatID == 0 {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Pack feed not configured."))
		return
	}
	posts, err := b.db.ListPendingScheduledAdminPosts(b.config.MonetizedChatID, 20)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Could not load scheduled posts."))
		return
	}
	if len(posts) == 0 {
		b.api.Send(tgbotapi.NewMessage(chatID, "📅 No scheduled posts."))
		return
	}
	loc := moscowLocation()
	var text strings.Builder
	text.WriteString("📅 Scheduled posts:\n\n")
	rows := make([][]tgbotapi.InlineKeyboardButton, 0, len(posts))
	for _, p := range posts {
		preview := truncateForDM(p.MessageText, 80)
		when := p.ScheduledAt.In(loc).Format("02.01 15:04")
		text.WriteString(fmt.Sprintf("• %s · «%s» · %s\n«%s»\n\n", when, adminPostAuthorUsername(p.Author), "MSK", preview))
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("🗑 Cancel %s", when),
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
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Could not cancel post."))
		return
	}
	if !ok {
		b.api.Send(tgbotapi.NewMessage(chatID, "⚠️ Post already published or cancelled."))
		return
	}
	b.api.Send(tgbotapi.NewMessage(chatID, "✅ Scheduled post cancelled."))
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
