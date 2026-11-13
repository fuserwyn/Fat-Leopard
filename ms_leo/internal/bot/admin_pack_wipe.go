package bot

import (
	"fmt"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"leo-bot/internal/database"
)

func (b *Bot) showAdminWipePackPrompt(chatID int64) {
	if b == nil || b.db == nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Database unavailable."))
		return
	}
	packChatID := b.adminPackChatID()
	if packChatID == 0 {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ MONETIZED_CHAT_ID (pack chat) is not configured."))
		return
	}

	counts, err := b.db.AdminCountPackFeedAndChat(packChatID)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Could not count rows: "+err.Error()))
		return
	}

	text := adminWipePackPromptText(counts)
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Yes, delete all", "admin_wipe_pack_yes"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❎ Cancel", "admin_cancel"),
		),
	)
	b.api.Send(msg)
}

func (b *Bot) executeAdminWipePack(chatID int64) {
	if b == nil || b.db == nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Database unavailable."))
		return
	}
	packChatID := b.adminPackChatID()
	if packChatID == 0 {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ MONETIZED_CHAT_ID (pack chat) is not configured."))
		return
	}

	deleted, err := b.db.AdminClearPackFeedAndChat(packChatID)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Wipe failed: "+err.Error()))
		return
	}

	var sb strings.Builder
	sb.WriteString("✅ Pack feed and group chat cleared.\n\n")
	sb.WriteString(adminWipePackDeletedSummary(deleted))
	sb.WriteString("\n\nMember profiles, streaks, and cups were not touched.")
	msg := tgbotapi.NewMessage(chatID, sb.String())
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Admin panel", "admin_open"),
		),
	)
	b.api.Send(msg)
}

func adminWipePackPromptText(c database.AdminPackWipeCounts) string {
	var sb strings.Builder
	sb.WriteString("⚠️ Clear the pack feed and group chat?\n\n")
	sb.WriteString("Will be deleted permanently:\n")
	sb.WriteString(fmt.Sprintf("• feed posts: %d\n", c.FeedPosts))
	sb.WriteString(fmt.Sprintf("• post comments: %d\n", c.FeedThreads))
	sb.WriteString(fmt.Sprintf("• content reports: %d\n", c.FeedReports))
	sb.WriteString(fmt.Sprintf("• group chat messages: %d\n", c.PackChatMessages))
	sb.WriteString("\nNot deleted: profiles, streaks, cups, paywall, training_sessions.\n\n")
	sb.WriteString("Clear for sure?")
	return sb.String()
}

func adminWipePackDeletedSummary(c database.AdminPackWipeCounts) string {
	return fmt.Sprintf(
		"Deleted:\n• feed posts: %d\n• comments: %d\n• reports: %d\n• chat messages: %d",
		c.FeedPosts, c.FeedThreads, c.FeedReports, c.PackChatMessages,
	)
}
