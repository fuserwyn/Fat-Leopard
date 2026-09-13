package bot

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"leo-bot/internal/database"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const (
	trackerApprovalCallbackPrefix = "admin_tracker_"
	trackerApprovalReminderAfter  = 1 * time.Hour
)

func trackerApprovalTargets(b *Bot, authorID int64) []int64 {
	if b == nil {
		return nil
	}
	seen := make(map[int64]struct{})
	out := make([]int64, 0, 8)
	for _, id := range b.trackerAdminNotifyIDs() {
		if id <= 0 || id == authorID {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func trackerHasApproval(t database.TrackerTask, adminID int64) bool {
	for _, id := range t.Approvals {
		if id == adminID {
			return true
		}
	}
	return false
}

func trackerAppendApproval(t *database.TrackerTask, adminID int64) bool {
	if t == nil || adminID <= 0 || trackerHasApproval(*t, adminID) {
		return false
	}
	t.Approvals = append(t.Approvals, adminID)
	return true
}

func trackerApprovalPendingTargets(b *Bot, t database.TrackerTask) []int64 {
	authorID := int64(0)
	if t.HasAuthor {
		authorID = t.AuthorID
	}
	targets := trackerApprovalTargets(b, authorID)
	pending := make([]int64, 0, len(targets))
	for _, id := range targets {
		if !trackerHasApproval(t, id) {
			pending = append(pending, id)
		}
	}
	return pending
}

func trackerApprovalReminderDue(t database.TrackerTask, now time.Time) bool {
	if !t.NeedsApproval || t.DevColumn != trackerColApprove {
		return false
	}
	if len(t.Approvals) >= trackerApprovalRequired {
		return false
	}
	if !t.HasApprovalNotified || t.ApprovalNotifiedAt.IsZero() {
		return false
	}
	if t.HasApprovalReminderSent {
		return false
	}
	return !t.ApprovalNotifiedAt.Add(trackerApprovalReminderAfter).After(now)
}

func (b *Bot) notifyTrackerApprovalsNeeded(t database.TrackerTask) {
	b.sendTrackerApprovalNotifications(t, false)
}

func (b *Bot) sendTrackerApprovalReminder(t database.TrackerTask) {
	b.sendTrackerApprovalNotifications(t, true)
}

func (b *Bot) sendTrackerApprovalNotifications(t database.TrackerTask, reminder bool) {
	if b == nil || !t.NeedsApproval || t.DevColumn != trackerColApprove {
		return
	}
	authorID := int64(0)
	if t.HasAuthor {
		authorID = t.AuthorID
	}
	targets := trackerApprovalTargets(b, authorID)
	if reminder {
		targets = trackerApprovalPendingTargets(b, t)
	}
	if len(targets) == 0 {
		return
	}
	text := b.trackerApprovalNotifyText(t, reminder)
	markup := trackerApprovalNotifyKeyboard(t.ID)
	for _, id := range targets {
		msg := tgbotapi.NewMessage(id, text)
		msg.DisableWebPagePreview = true
		msg.ReplyMarkup = markup
		if _, err := b.api.Send(msg); err != nil && b.logger != nil {
			b.logger.Warnf("трекер: не отправить аппрув #%d админу %d: %v", trackerDueNum(t), id, err)
		}
	}
	if b.db == nil || t.ID <= 0 {
		return
	}
	if reminder {
		if err := b.db.MarkTrackerApprovalReminderSent(t.ID); err != nil && b.logger != nil {
			b.logger.Warnf("трекер: не отметить напоминание аппрува #%d: %v", trackerDueNum(t), err)
		}
		return
	}
	if err := b.db.MarkTrackerApprovalNotified(t.ID); err != nil && b.logger != nil {
		b.logger.Warnf("трекер: не отметить уведомление аппрува #%d: %v", trackerDueNum(t), err)
	}
}

func (b *Bot) trackerApprovalNotifyText(t database.TrackerTask, reminder bool) string {
	prompt := strings.TrimSpace(t.Prompt)
	if runes := []rune(prompt); len(runes) > 400 {
		prompt = string(runes[:400]) + "…"
	}
	text := ""
	if reminder {
		text = "⏰ Напоминание: "
	}
	text += fmt.Sprintf("📋 %s ждёт аппрува\n\n", trackerNotifyHeading(t))
	text += fmt.Sprintf("Поставил: %s\n\n", b.trackerTaskAuthorLabel(t))
	if prompt != "" {
		text += prompt + "\n\n"
	}
	text += fmt.Sprintf("Аппрувы: %d/%d", len(t.Approvals), trackerApprovalRequired)
	return text
}

func trackerApprovalNotifyKeyboard(taskID int64) tgbotapi.InlineKeyboardMarkup {
	id := strconv.FormatInt(taskID, 10)
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Аппрув", trackerApprovalCallbackPrefix+"approve_"+id),
			tgbotapi.NewInlineKeyboardButtonData("❌ Не аппрув", trackerApprovalCallbackPrefix+"reject_"+id),
		),
	)
}

func trackerApprovalRejectKeyboard(taskID int64) tgbotapi.InlineKeyboardMarkup {
	id := strconv.FormatInt(taskID, 10)
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✏️ Изменить описание", trackerApprovalCallbackPrefix+"edit_"+id),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🗑 Отменить с комментарием", trackerApprovalCallbackPrefix+"cancel_"+id),
			tgbotapi.NewInlineKeyboardButtonData("⛔ Отменить без комментария", trackerApprovalCallbackPrefix+"dismiss_"+id),
		),
	)
}

func (b *Bot) handleTrackerApprovalCallback(callback *tgbotapi.CallbackQuery) bool {
	if callback == nil || callback.Message == nil || callback.From == nil {
		return false
	}
	data := callback.Data
	if !strings.HasPrefix(data, trackerApprovalCallbackPrefix) {
		return false
	}
	if !b.isAdminTelegramUser(callback.From.ID) || !callback.Message.Chat.IsPrivate() {
		b.api.Request(tgbotapi.NewCallback(callback.ID, "Недостаточно прав"))
		return true
	}
	rest := strings.TrimPrefix(data, trackerApprovalCallbackPrefix)
	parts := strings.SplitN(rest, "_", 2)
	if len(parts) != 2 {
		b.api.Request(tgbotapi.NewCallback(callback.ID, ""))
		return true
	}
	action := parts[0]
	taskID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || taskID <= 0 {
		b.api.Request(tgbotapi.NewCallback(callback.ID, "Задача не найдена"))
		return true
	}
	adminID := callback.From.ID
	switch action {
	case "approve":
		toast, err := b.approveTrackerTask(taskID, adminID)
		if err != nil {
			b.api.Request(tgbotapi.NewCallback(callback.ID, err.Error()))
			return true
		}
		b.api.Request(tgbotapi.NewCallback(callback.ID, toast))
		b.editTrackerApprovalMessage(callback.Message, taskID)
		return true
	case "reject":
		edit := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID,
			fmt.Sprintf("Что сделать с задачей #%d?", taskID))
		kb := trackerApprovalRejectKeyboard(taskID)
		edit.ReplyMarkup = &kb
		b.api.Send(edit)
		b.api.Request(tgbotapi.NewCallback(callback.ID, ""))
		return true
	case "edit":
		b.startTrackerApprovalEdit(adminID, taskID)
		b.api.Send(tgbotapi.NewMessage(callback.Message.Chat.ID,
			fmt.Sprintf("✏️ Пришли новое описание для задачи #%d. /cancel — отмена.", taskID)))
		b.api.Request(tgbotapi.NewCallback(callback.ID, ""))
		return true
	case "cancel":
		b.startTrackerApprovalCancel(adminID, taskID, true)
		b.api.Send(tgbotapi.NewMessage(callback.Message.Chat.ID,
			fmt.Sprintf("🗑 Пришли комментарий для автора задачи #%d или «-» без комментария. /cancel — отмена.", taskID)))
		b.api.Request(tgbotapi.NewCallback(callback.ID, ""))
		return true
	case "dismiss":
		if err := b.dismissTrackerApproval(taskID, adminID, ""); err != nil {
			b.api.Request(tgbotapi.NewCallback(callback.ID, err.Error()))
			return true
		}
		b.api.Request(tgbotapi.NewCallback(callback.ID, "Задача отменена"))
		b.editTrackerApprovalMessage(callback.Message, taskID)
		return true
	default:
		b.api.Request(tgbotapi.NewCallback(callback.ID, ""))
		return true
	}
}

func (b *Bot) editTrackerApprovalMessage(msg *tgbotapi.Message, taskID int64) {
	if b == nil || msg == nil || b.db == nil {
		return
	}
	t, err := b.db.GetTrackerTask(taskID)
	if err != nil {
		edit := tgbotapi.NewEditMessageText(msg.Chat.ID, msg.MessageID, "Задача обновлена.")
		edit.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}}
		b.api.Send(edit)
		return
	}
	text := b.trackerApprovalNotifyText(t, false)
	var markup *tgbotapi.InlineKeyboardMarkup
	if t.DevColumn == trackerColApprove && t.NeedsApproval {
		kb := trackerApprovalNotifyKeyboard(taskID)
		markup = &kb
	} else if t.DevColumn == trackerColDoing {
		text = fmt.Sprintf("✅ %s одобрена и ушла в работу.", trackerNotifyHeading(t))
		markup = &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}}
	} else if t.Status == "canceled" || t.DevColumn == trackerColCanceled {
		text = fmt.Sprintf("⛔ %s отменена.", trackerNotifyHeading(t))
		markup = &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}}
	}
	edit := tgbotapi.NewEditMessageText(msg.Chat.ID, msg.MessageID, text)
	edit.ReplyMarkup = markup
	b.api.Send(edit)
}

func (b *Bot) approveTrackerTask(taskID, adminID int64) (string, error) {
	if b == nil || b.db == nil {
		return "", fmt.Errorf("база недоступна")
	}
	t, err := b.db.GetTrackerTask(taskID)
	if err != nil {
		return "", err
	}
	if !t.NeedsApproval || t.DevColumn != trackerColApprove {
		return "", fmt.Errorf("задача не ждёт аппрува")
	}
	if t.HasAuthor && t.AuthorID == adminID {
		return "", fmt.Errorf("автор не может аппрувить свою задачу")
	}
	if !trackerAppendApproval(&t, adminID) {
		return "Вы уже аппрувнули", nil
	}
	appendTrackerStep(&t, fmt.Sprintf("Аппрув от админа %d (%d/%d)", adminID, len(t.Approvals), trackerApprovalRequired))
	if len(t.Approvals) >= trackerApprovalRequired {
		if err := applyTrackerColumn(&t, trackerColDoing); err != nil {
			return "", err
		}
		appendTrackerStep(&t, "Два аппрува — в работу")
	}
	if err := b.db.SaveTrackerTask(t); err != nil {
		return "", err
	}
	if t.DevColumn == trackerColDoing {
		b.dispatchTrackerAgent(t, "doing")
		return "Задача ушла в работу", nil
	}
	return fmt.Sprintf("Аппрув учтён (%d/%d)", len(t.Approvals), trackerApprovalRequired), nil
}

func (b *Bot) dismissTrackerApproval(taskID, adminID int64, comment string) error {
	if b == nil || b.db == nil {
		return fmt.Errorf("база недоступна")
	}
	t, err := b.db.GetTrackerTask(taskID)
	if err != nil {
		return err
	}
	if t.DevColumn != trackerColApprove {
		return fmt.Errorf("задача уже не на аппруве")
	}
	if err := applyTrackerColumn(&t, trackerColCanceled); err != nil {
		return err
	}
	step := fmt.Sprintf("Отклонена админом %d", adminID)
	if c := strings.TrimSpace(comment); c != "" && c != "-" {
		step += ": " + c
	}
	appendTrackerStep(&t, step)
	if err := b.db.SaveTrackerTask(t); err != nil {
		return err
	}
	if t.HasAuthor && t.AuthorID > 0 {
		note := fmt.Sprintf("⛔ %s отменена после аппрува.", trackerNotifyHeading(t))
		if c := strings.TrimSpace(comment); c != "" && c != "-" {
			note += "\n\nКомментарий: " + c
		}
		_ = b.NotifyTrackerAuthor(t.AuthorID, note)
	}
	return nil
}

func (b *Bot) startTrackerApprovalEdit(adminID, taskID int64) {
	b.adminSessionsMutex.Lock()
	defer b.adminSessionsMutex.Unlock()
	b.adminSessions[adminID] = &adminSession{
		Mode:   "tracker_approval_edit",
		Step:   "await_prompt",
		TaskID: taskID,
	}
}

func (b *Bot) startTrackerApprovalCancel(adminID, taskID int64, withComment bool) {
	step := "await_comment"
	if !withComment {
		step = "await_dismiss"
	}
	b.adminSessionsMutex.Lock()
	defer b.adminSessionsMutex.Unlock()
	b.adminSessions[adminID] = &adminSession{
		Mode:   "tracker_approval_cancel",
		Step:   step,
		TaskID: taskID,
	}
}

func (b *Bot) handleTrackerApprovalFlowMessage(msg *tgbotapi.Message, session *adminSession) bool {
	if session == nil || msg == nil || msg.From == nil {
		return false
	}
	switch session.Mode {
	case "tracker_approval_edit":
		if session.Step != "await_prompt" {
			return false
		}
		prompt := strings.TrimSpace(msg.Text)
		if prompt == "" {
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "⚠️ Описание пустое. Пришли текст или /cancel."))
			return true
		}
		t, err := b.db.GetTrackerTask(session.TaskID)
		if err != nil {
			b.clearAdminFlow(msg.From.ID)
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ Задача не найдена."))
			return true
		}
		if t.DevColumn != trackerColApprove {
			b.clearAdminFlow(msg.From.ID)
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "Задача уже не на аппруве."))
			return true
		}
		t.Prompt = prompt
		t.Approvals = nil
		appendTrackerStep(&t, "Описание обновили после отказа")
		appendTrackerStep(&t, "Аппрувы сброшены")
		if err := b.db.SaveTrackerTask(t); err != nil {
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ Не сохранилось: "+err.Error()))
			return true
		}
		b.clearAdminFlow(msg.From.ID)
		b.notifyTrackerApprovalsNeeded(t)
		b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("✅ Описание задачи #%d обновлено, админам снова ушло уведомление.", trackerDueNum(t))))
		return true
	case "tracker_approval_cancel":
		if session.Step != "await_comment" {
			return false
		}
		comment := strings.TrimSpace(msg.Text)
		if err := b.dismissTrackerApproval(session.TaskID, msg.From.ID, comment); err != nil {
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ "+err.Error()))
			return true
		}
		b.clearAdminFlow(msg.From.ID)
		b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "✅ Задача отменена."))
		return true
	default:
		return false
	}
}

func (b *Bot) localTrackerApprove(taskID int64, payload map[string]any, adminID int64) (json.RawMessage, error) {
	action := payloadString(payload, "action")
	if action == "" {
		action = "approve"
	}
	switch strings.ToLower(action) {
	case "approve":
		_, err := b.approveTrackerTask(trackerPayloadTaskID(taskID, payload), adminID)
		if err != nil {
			return nil, err
		}
	case "reject":
		comment := payloadString(payload, "comment")
		if err := b.dismissTrackerApproval(trackerPayloadTaskID(taskID, payload), adminID, comment); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("такое действие доске недоступно")
	}
	t, err := b.localTrackerLoad(taskID, payload)
	if err != nil {
		return nil, err
	}
	return trackerJSON(map[string]any{"ok": true, "task": trackerTaskView(t, false)})
}
