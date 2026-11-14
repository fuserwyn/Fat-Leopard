package bot

import (
	"fmt"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"leo-bot/internal/database"
)

func (b *Bot) showAdminWipePackPrompt(chatID int64) {
	if b == nil || b.db == nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ База недоступна."))
		return
	}
	packChatID := b.adminPackChatID()
	if packChatID == 0 {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Не настроен MONETIZED_CHAT_ID (pack chat)."))
		return
	}

	counts, err := b.db.AdminCountPackFeedAndChat(packChatID)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Не удалось посчитать записи: "+err.Error()))
		return
	}

	text := adminWipePackPromptText(counts)
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Да, удалить всё", "admin_wipe_pack_yes"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❎ Отмена", "admin_cancel"),
		),
	)
	b.api.Send(msg)
}

func (b *Bot) executeAdminWipePack(chatID int64) {
	if b == nil || b.db == nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ База недоступна."))
		return
	}
	packChatID := b.adminPackChatID()
	if packChatID == 0 {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Не настроен MONETIZED_CHAT_ID (pack chat)."))
		return
	}

	deleted, err := b.db.AdminClearPackFeedAndChat(packChatID)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Очистка не удалась: "+err.Error()))
		return
	}

	var sb strings.Builder
	sb.WriteString("✅ Лента и общий чат стаи очищены.\n\n")
	sb.WriteString(adminWipePackDeletedSummary(deleted))
	sb.WriteString("\n\nПрофили, стрики и кубки пользователей не тронуты.")
	msg := tgbotapi.NewMessage(chatID, sb.String())
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ К админке", "admin_open"),
		),
	)
	b.api.Send(msg)
}

func adminWipePackPromptText(c database.AdminPackWipeCounts) string {
	var sb strings.Builder
	sb.WriteString("⚠️ Очистить ленту и общий чат стаи?\n\n")
	sb.WriteString("Будет удалено безвозвратно:\n")
	sb.WriteString(fmt.Sprintf("• посты в ленте: %d\n", c.FeedPosts))
	sb.WriteString(fmt.Sprintf("• комментарии к постам: %d\n", c.FeedThreads))
	sb.WriteString(fmt.Sprintf("• жалобы на контент: %d\n", c.FeedReports))
	sb.WriteString(fmt.Sprintf("• сообщения общего чата: %d\n", c.PackChatMessages))
	sb.WriteString("\nНе удаляется: профили, стрики, кубки, paywall, training_sessions.\n\n")
	sb.WriteString("Точно очистить?")
	return sb.String()
}

func adminWipePackDeletedSummary(c database.AdminPackWipeCounts) string {
	return fmt.Sprintf(
		"Удалено:\n• постов ленты: %d\n• комментариев: %d\n• жалоб: %d\n• сообщений чата: %d",
		c.FeedPosts, c.FeedThreads, c.FeedReports, c.PackChatMessages,
	)
}
