package bot

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"leo-bot/internal/domain"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	initdata "github.com/telegram-mini-apps/init-data-golang"
)

var (
	ErrFeedReportSelf          = errors.New("cannot report own content")
	ErrFeedReportLeo           = errors.New("cannot report leo content")
	ErrFeedReportAlreadyExists = errors.New("feed report already exists")
	ErrFeedReportNotFound      = errors.New("feed report not found")
)

// PackFeedReport — жалоба на пост или комментарий в ленте.
func (b *Bot) PackFeedReport(viewerUserID int64, initD initdata.InitData, userMessageID, threadReplyID int64) error {
	if err := b.AssertMiniAppPackChatAligns(initD); err != nil {
		return err
	}
	if err := b.assertPackFeedSocialViewer(viewerUserID); err != nil {
		return err
	}
	if userMessageID == 0 {
		return ErrTrainingFeedParentNotFound
	}
	chatID := b.config.MonetizedChatID
	typ, has, err := b.db.GetUserMessageTypeByIDForChat(userMessageID, chatID)
	if err != nil {
		return err
	}
	if !has || !packFeedSupportsThread(typ) {
		return ErrTrainingFeedParentNotFound
	}

	targetType := "feed_post"
	var targetUserID int64
	var targetText string

	if threadReplyID > 0 {
		row, ok, err := b.db.GetTrainingFeedThreadRowInPack(threadReplyID, chatID)
		if err != nil {
			return err
		}
		if !ok || row.UserMessageID != userMessageID {
			return ErrTrainingFeedThreadDeleteNotFound
		}
		if row.FromUserID == 0 {
			return ErrFeedReportLeo
		}
		targetType = "thread_reply"
		targetUserID = row.FromUserID
		targetText = row.MessageText
	} else {
		authorID, ok, err := b.db.GetUserMessageAuthorUserID(chatID, userMessageID)
		if err != nil {
			return err
		}
		if !ok {
			return ErrTrainingFeedParentNotFound
		}
		targetUserID = authorID
		targetText, err = b.db.GetUserMessageTextByIDForChat(userMessageID, chatID)
		if err != nil {
			return err
		}
	}

	if targetUserID == 0 {
		return ErrFeedReportLeo
	}
	if targetUserID == viewerUserID {
		return ErrFeedReportSelf
	}

	reportID, err := b.db.InsertMiniappFeedReport(
		chatID, viewerUserID, targetType, userMessageID, threadReplyID, targetUserID, targetText,
	)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
			return ErrFeedReportAlreadyExists
		}
		return err
	}
	b.notifyAdminsAboutFeedReport(reportID, viewerUserID, targetType, userMessageID, threadReplyID, targetUserID, targetText)
	complaintTargetID := userMessageID
	if threadReplyID > 0 {
		complaintTargetID = threadReplyID
	}
	b.trackComplaintFiled(viewerUserID, targetType, complaintTargetID)
	return nil
}

func (b *Bot) buildAdminFeedReportNotifyText(
	reportID, reporterUserID, targetUserID, userMessageID, threadReplyID int64,
	targetType, targetText, resolvedBy string,
) string {
	kind := "пост в ленте"
	if targetType == "thread_reply" {
		kind = "комментарий"
	} else if targetType == "pack_group_message" {
		kind = "сообщение в чате"
	}
	title := fmt.Sprintf("🚨 Жалоба на %s · #%d", kind, reportID)
	if resolvedBy != "" {
		title = fmt.Sprintf("✅ Решено · жалоба на %s · #%d", kind, reportID)
	}
	body := fmt.Sprintf(
		"От: %s\nНа: %s\nОтчёт #%d",
		b.supportDisplayName(reporterUserID),
		b.supportDisplayName(targetUserID),
		userMessageID,
	)
	if threadReplyID > 0 {
		body += fmt.Sprintf("\nКомментарий #%d", threadReplyID)
	}
	body += "\n\n«" + clipAdminSupportText(targetText, 400) + "»"
	footer := "\n\n«⚙️ Админ-панель» внизу → Поддержка → Жалобы."
	if resolvedBy != "" {
		footer = "\n\n✅ Решено · " + resolvedBy
	}
	return title + "\n\n" + body + footer
}

func (b *Bot) notifyAdminsAboutFeedReport(
	reportID, reporterUserID int64,
	targetType string,
	userMessageID, threadReplyID, targetUserID int64,
	targetText string,
) {
	if b == nil || b.api == nil || b.config == nil {
		return
	}
	adminIDs := b.adminNotifyIDs()
	if len(adminIDs) == 0 {
		return
	}

	text := b.buildAdminFeedReportNotifyText(
		reportID, reporterUserID, targetUserID, userMessageID, threadReplyID, targetType, targetText, "",
	)

	for _, adminID := range adminIDs {
		msg := tgbotapi.NewMessage(adminID, text)
		msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("🚨 Открыть", "admin_feed_report_"+strconv.FormatInt(reportID, 10)),
				tgbotapi.NewInlineKeyboardButtonData("✅ Решено", "admin_feed_report_dismiss_"+strconv.FormatInt(reportID, 10)),
			),
		)
		sent, err := b.api.Send(msg)
		if err != nil {
			b.logger.Warnf("feed report notify admin=%d report=%d: %v", adminID, reportID, err)
			continue
		}
		if b.db != nil && sent.MessageID != 0 {
			if err := b.db.InsertMiniappFeedReportAdminNotify(reportID, adminID, adminID, int64(sent.MessageID)); err != nil {
				b.logger.Warnf("feed report notify store admin=%d report=%d: %v", adminID, reportID, err)
			}
		}
	}
}

func (b *Bot) markFeedReportNotifyMessagesResolved(reportID int64, resolverUserID int64, fallbackMsg *tgbotapi.Message) {
	if b == nil || b.api == nil || b.db == nil || b.config == nil || reportID == 0 {
		return
	}
	item, err := b.db.GetMiniappFeedReport(b.config.MonetizedChatID, reportID)
	if err != nil || item == nil {
		return
	}
	resolverName := b.supportDisplayName(resolverUserID)
	text := b.buildAdminFeedReportNotifyText(
		item.ID,
		item.ReporterUserID,
		item.TargetUserID,
		item.UserMessageID,
		item.ThreadReplyID,
		item.TargetType,
		item.TargetText,
		resolverName,
	)
	text = clipAdminSupportText(text, 3500)
	emptyMarkup := &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}}

	seen := map[string]struct{}{}
	editOne := func(chatID, messageID int64) {
		if chatID == 0 || messageID == 0 {
			return
		}
		key := fmt.Sprintf("%d:%d", chatID, messageID)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		edit := tgbotapi.NewEditMessageText(chatID, int(messageID), text)
		edit.ReplyMarkup = emptyMarkup
		if _, err := b.api.Send(edit); err != nil {
			b.logger.Warnf("feed report resolve edit chat=%d msg=%d report=%d: %v", chatID, messageID, reportID, err)
		}
	}

	notifies, err := b.db.ListMiniappFeedReportAdminNotifies(reportID)
	if err != nil {
		b.logger.Warnf("feed report resolve list notifies report=%d: %v", reportID, err)
	}
	for _, n := range notifies {
		editOne(n.ChatID, n.MessageID)
	}
	if fallbackMsg != nil {
		editOne(fallbackMsg.Chat.ID, int64(fallbackMsg.MessageID))
	}
}

func (b *Bot) adminNotifyIDs() []int64 {
	if b == nil || b.config == nil {
		return nil
	}
	adminIDs := make([]int64, 0, len(b.config.AdminIDs)+1)
	seen := map[int64]struct{}{}
	if b.config.OwnerID != 0 {
		seen[b.config.OwnerID] = struct{}{}
		adminIDs = append(adminIDs, b.config.OwnerID)
	}
	for _, id := range b.config.AdminIDs {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		adminIDs = append(adminIDs, id)
	}
	return adminIDs
}

func feedReportTargetLabel(item *domain.MiniappFeedReport) string {
	if item == nil {
		return ""
	}
	switch item.TargetType {
	case "thread_reply":
		return "Комментарий"
	case "pack_group_message":
		return "Сообщение в чате"
	default:
		return "Пост"
	}
}

func feedReportPersonLabel(name string, userID int64) string {
	return adminSupportTitle(name, userID)
}
