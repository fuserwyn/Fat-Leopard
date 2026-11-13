package bot

import (
	"fmt"
	"strconv"
	"strings"

	"leo-bot/internal/database"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func hiddenModerationKindLabel(kind string) string {
	switch kind {
	case "feed_post":
		return "Post"
	case "thread_reply":
		return "Comment"
	case "pack_group_message":
		return "Chat message"
	default:
		return "Content"
	}
}

func hiddenModerationRestoreCallback(kind string, id int64) string {
	switch kind {
	case "feed_post":
		return "admin_hidden_restore_post_" + strconv.FormatInt(id, 10)
	case "thread_reply":
		return "admin_hidden_restore_thread_" + strconv.FormatInt(id, 10)
	case "pack_group_message":
		return "admin_hidden_restore_chat_" + strconv.FormatInt(id, 10)
	default:
		return ""
	}
}

func (b *Bot) showAdminHiddenContentInbox(chatID int64) {
	if b == nil || b.db == nil || b.config == nil || b.config.MonetizedChatID == 0 {
		return
	}
	packChatID := b.config.MonetizedChatID
	items, err := b.db.ListHiddenModerationItems(packChatID, 20)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Could not load hidden content."))
		return
	}
	// Два бакета для постов: удалённые adminом (кнопкой «удалить») и скрытые по жалобе.
	// Комментарии/сообщения чата — отдельной группой «прочее».
	var adminDeleted, reported, other []database.HiddenModerationItem
	for _, item := range items {
		switch {
		case item.Kind == "feed_post" && item.Reason == "report":
			reported = append(reported, item)
		case item.Kind == "feed_post":
			adminDeleted = append(adminDeleted, item) // admin_delete + легаси без причины
		default:
			other = append(other, item)
		}
	}

	var text strings.Builder
	text.WriteString("🙈 Hidden and deleted\n\n")
	text.WriteString("You can restore to the feed or chat. Permanent delete only via «Clear feed & chat» or the user card.\n\n")
	if len(items) == 0 {
		text.WriteString("Empty — no hidden or deleted content.")
	}

	rows := make([][]tgbotapi.InlineKeyboardButton, 0, len(items)+1)
	renderGroup := func(title, btnIcon string, group []database.HiddenModerationItem) {
		if len(group) == 0 {
			return
		}
		text.WriteString(title + "\n")
		for i, item := range group {
			author := adminSupportTitle(item.AuthorName, item.AuthorUserID)
			text.WriteString(fmt.Sprintf(
				"%d. %s · %s · %s\n",
				i+1,
				hiddenModerationKindLabel(item.Kind),
				author,
				clipAdminSupportText(item.Text, 64),
			))
			if item.Kind == "thread_reply" && item.ParentID > 0 {
				text.WriteString(fmt.Sprintf("   Report #%d\n", item.ParentID))
			}
			cb := hiddenModerationRestoreCallback(item.Kind, item.ID)
			if cb != "" {
				rows = append(rows, tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData(
						fmt.Sprintf("%s ↩️ #%d · %s", btnIcon, item.ID, clipAdminSupportText(hiddenModerationKindLabel(item.Kind), 10)),
						cb,
					),
				))
			}
		}
		text.WriteString("\n")
	}
	renderGroup("🗑 Deleted by admin:", "🗑", adminDeleted)
	renderGroup("🚩 Hidden by report:", "🚩", reported)
	renderGroup("💬 Comments & pack chat:", "💬", other)
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("⬅️ Support", "admin_support_back"),
	))
	msg := tgbotapi.NewMessage(chatID, clipAdminSupportText(text.String(), 3500))
	msg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
	b.api.Send(msg)
}

func (b *Bot) adminRestoreHiddenContent(chatID int64, kind string, id int64) {
	if b == nil || b.db == nil || b.config == nil || id == 0 {
		return
	}
	packChatID := b.config.MonetizedChatID
	var ok bool
	var err error
	var label string
	switch kind {
	case "feed_post":
		ok, err = b.db.AdminUnhideFeedUserMessage(packChatID, id)
		label = fmt.Sprintf("post #%d", id)
	case "thread_reply":
		parentPostID := int64(0)
		if row, found, rerr := b.db.GetTrainingFeedThreadRowInPack(id, packChatID); rerr == nil && found {
			parentPostID = row.UserMessageID
		}
		ok, err = b.db.AdminUnhideTrainingFeedThreadReply(packChatID, id)
		label = fmt.Sprintf("comment t%d", id)
		if ok && parentPostID > 0 {
			label = fmt.Sprintf("comment t%d under post #%d", id, parentPostID)
		}
	case "pack_group_message":
		ok, err = b.db.AdminUnhidePackGroupMessage(packChatID, id)
		label = fmt.Sprintf("pack chat message #%d", id)
	default:
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Unknown content type."))
		return
	}
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Error: "+err.Error()))
		return
	}
	if !ok {
		b.api.Send(tgbotapi.NewMessage(chatID, "ℹ️ Already restored or not found."))
	} else {
		msg := "✅ Restored: " + label + ".\n\nPull to refresh the feed in the mini app and open Comments under this post."
		b.api.Send(tgbotapi.NewMessage(chatID, msg))
	}
	b.showAdminHiddenContentInbox(chatID)
}
