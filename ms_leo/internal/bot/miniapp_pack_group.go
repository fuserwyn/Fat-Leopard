package bot

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"leo-bot/internal/database"
	"leo-bot/internal/domain"
	"leo-bot/internal/moderation"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	initdata "github.com/telegram-mini-apps/init-data-golang"
)

var ErrPackGroupInvalidReply = errors.New("pack group invalid reply reference")
var ErrPackGroupMessageNotFound = errors.New("pack group message not found")

// @leo или @<username_бота> (как в группе).
var reMentionLeo = regexp.MustCompile(`(?i)@leo\b`)

func textMentionsLeoForPackGroup(text, botUsername string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	if reMentionLeo.MatchString(t) {
		return true
	}
	bu := strings.TrimSpace(strings.ToLower(botUsername))
	if bu == "" {
		return false
	}
	lt := strings.ToLower(t)
	return strings.Contains(lt, "@"+bu)
}

// packGroupReplyTargetsLeo — true, если сообщение является reply на реплику Лео.
// Ответ на сообщение Лео считаем прямым обращением к нему, даже без @leo.
func (b *Bot) packGroupReplyTargetsLeo(chatID, replyToID int64) bool {
	if replyToID <= 0 {
		return false
	}
	parent, ok, err := b.db.GetMiniappPackGroupMessageInPack(chatID, replyToID)
	if err != nil || !ok {
		return false
	}
	return parent.IsLeo || parent.FromUserID == 0
}

func displayNameFromInitData(d initdata.InitData) string {
	u := d.User
	if u.Username != "" {
		return "@" + u.Username
	}
	s := u.FirstName
	if u.LastName != "" {
		s += " " + u.LastName
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return fmt.Sprintf("user%d", u.ID)
	}
	return s
}

// PackGroupChatForViewer — история общего чата (мини-апп), те же права, что и лента.
func (b *Bot) PackGroupChatForViewer(viewerUserID int64, initD initdata.InitData, initDataRaw string) ([]*domain.PackGroupChatMessage, error) {
	if err := b.AssertMiniAppPackChatAligns(initD); err != nil {
		return nil, err
	}
	chatID := b.config.MonetizedChatID
	if chatID == 0 {
		return []*domain.PackGroupChatMessage{}, nil
	}
	if b.config.IsAdminTelegramUser(viewerUserID) {
		// ok
	} else {
		ok, err := b.db.UserInPackOrPaid(viewerUserID, chatID, b.paywallEntryRequiresPayment())
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrPackFeedForbidden
		}
	}
	rows, err := b.db.ListMiniappPackGroupChatRows(chatID, 100, nil)
	if err != nil {
		return nil, err
	}
	msgs := b.packGroupRowsToMessages(rows)
	for _, m := range msgs {
		if m != nil && m.PhotoURL != "" {
			m.PhotoURL = b.canonicalMiniappTrainingPhotoURL(m.PhotoURL)
		}
	}
	msgs = b.enrichPackGroupChatAuthorPhotos(msgs, chatID, initDataRaw)
	return b.enrichPackGroupChatReactions(msgs, viewerUserID, chatID), nil
}

// PackGroupChatSearch — поиск по всей истории общего чата (текст сообщений).
// Возвращает совпадения (новые сверху) с цитатами reply; фото/реакции не обогащаем —
// результаты используются как список сниппетов.
func (b *Bot) PackGroupChatSearch(viewerUserID int64, initD initdata.InitData, query string, limit int) ([]*domain.PackGroupChatMessage, error) {
	if err := b.AssertMiniAppPackChatAligns(initD); err != nil {
		return nil, err
	}
	chatID := b.config.MonetizedChatID
	if chatID == 0 {
		return []*domain.PackGroupChatMessage{}, nil
	}
	q := strings.TrimSpace(query)
	if len([]rune(q)) < 2 {
		return []*domain.PackGroupChatMessage{}, nil
	}
	if b.config.IsAdminTelegramUser(viewerUserID) {
		// ok
	} else {
		ok, err := b.db.UserInPackOrPaid(viewerUserID, chatID, b.paywallEntryRequiresPayment())
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrPackFeedForbidden
		}
	}
	rows, err := b.db.SearchMiniappPackGroupChatRows(chatID, q, limit)
	if err != nil {
		return nil, err
	}
	return b.packGroupRowsToMessages(rows), nil
}

func (b *Bot) packGroupRowsToMessages(rows []database.PackGroupChatRow) []*domain.PackGroupChatMessage {
	out := make([]*domain.PackGroupChatMessage, 0, len(rows))
	if len(rows) == 0 {
		return out
	}
	var refIDs []int64
	seen := map[int64]struct{}{}
	for _, r := range rows {
		m := packGroupRowToMessage(r)
		if r.ReplyToID.Valid && r.ReplyToID.Int64 > 0 {
			rid := r.ReplyToID.Int64
			if _, ok := seen[rid]; !ok {
				seen[rid] = struct{}{}
				refIDs = append(refIDs, rid)
			}
		}
		out = append(out, &m)
	}
	parentByID := map[int64]database.PackGroupChatRow{}
	if len(refIDs) > 0 && b.db != nil && b.config.MonetizedChatID != 0 {
		m, err := b.db.ListMiniappPackGroupMessagesByIDs(b.config.MonetizedChatID, refIDs)
		if err != nil {
			b.logger.Warnf("pack group reply parents: %v", err)
		} else {
			parentByID = m
		}
	}
	for _, m := range out {
		if m == nil || m.ReplyToID == 0 {
			continue
		}
		if p, ok := parentByID[m.ReplyToID]; ok {
			m.ReplyToIsLeo = p.IsLeo
			if p.IsLeo {
				m.ReplyToUsername = "Лео"
			} else {
				m.ReplyToUsername = strings.TrimSpace(p.Username)
				if m.ReplyToUsername == "" {
					m.ReplyToUsername = fmt.Sprintf("Участник %d", p.FromUserID)
				}
			}
			m.ReplyToText = truncateForDM(p.MessageText, 100)
		}
	}
	return out
}

func packGroupRowToMessage(r database.PackGroupChatRow) domain.PackGroupChatMessage {
	m := domain.PackGroupChatMessage{
		ID:        r.ID,
		UserID:    r.FromUserID,
		Username:  r.Username,
		Text:      r.MessageText,
		CreatedAt: r.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		IsLeo:     r.IsLeo,
		PhotoURL:  r.PhotoURL,
	}
	if r.ReplyToID.Valid && r.ReplyToID.Int64 > 0 {
		m.ReplyToID = r.ReplyToID.Int64
	}
	if r.EditedAt.Valid {
		m.EditedAt = r.EditedAt.Time.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	return m
}

// ProcessMiniAppPackGroupMessage — сохраняет реплику (photoURL — опц. вложение);
// при @leo / @бот вызывает ИИ, без отправки в Telegram. Допускается сообщение только с фото (пустой текст).
func (b *Bot) ProcessMiniAppPackGroupMessage(d initdata.InitData, text string, replyToID int64, photoURL string) (MiniAppTextProcessResult, error) {
	out := MiniAppTextProcessResult{}
	photoURL = strings.TrimSpace(photoURL)
	if b == nil || (strings.TrimSpace(text) == "" && photoURL == "") {
		return out, nil
	}
	if d.User.ID == 0 {
		return out, nil
	}
	if err := b.AssertMiniAppPackChatAligns(d); err != nil {
		return out, err
	}
	chatID := b.config.MonetizedChatID
	if chatID == 0 {
		return out, nil
	}
	if b.config.IsAdminTelegramUser(d.User.ID) {
		// владелец
	} else {
		ok, err := b.db.UserInPackOrPaid(d.User.ID, chatID, b.paywallEntryRequiresPayment())
		if err != nil {
			return out, err
		}
		if !ok {
			return out, ErrPackFeedForbidden
		}
	}
	text = strings.TrimSpace(text)
	if text != "" {
		if _, err := b.enforceUGC(text, moderation.SurfacePackGroupChat, d.User.ID); err != nil {
			return out, err
		}
	}
	if replyToID > 0 {
		parent, ok, err := b.db.GetMiniappPackGroupMessageInPack(chatID, replyToID)
		if err != nil {
			return out, err
		}
		if !ok {
			return out, ErrPackGroupInvalidReply
		}
		_ = parent
	}
	uname := displayNameFromInitData(d)

	userMsgID, err := b.db.InsertMiniappPackGroupMessage(chatID, d.User.ID, uname, false, text, replyToID, photoURL)
	if err != nil {
		// Раньше ошибка проглатывалась и наружу отдавался успех — сообщение «терялось»
		// без уведомления. Теперь возвращаем ошибку, чтобы miniapp показал её пользователю.
		b.logger.Errorf("pack miniapp insert user row: %v", err)
		return out, err
	}
	if text != "" {
		b.indexPackGroupChatRAG(chatID, d.User.ID, "user", text, userMsgID)
	}
	if userMsgID > 0 {
		if replyToID > 0 {
			b.afterPackGroupReplyInserted(chatID, d.User.ID, uname, text, userMsgID, replyToID)
		} else if strings.TrimSpace(text) != "" {
			b.notifyPackMemberMentionsInPackGroup(chatID, d.User.ID, uname, text, userMsgID, nil)
		}
	}

	if reply := b.answerLeoInPackGroupChatIfMentioned(d, chatID, text, userMsgID, photoURL, replyToID); reply != "" {
		out.ReplyText = reply
	}
	return out, nil
}

// answerLeoInPackGroupChatIfMentioned — если текст призывает Лео (@leo/@бот) ИЛИ сообщение
// является reply на реплику Лео, генерирует ответ ИИ, вставляет строку Лео (reply на userMsgID)
// и возвращает текст. Иначе — пустая строка.
// Вынесено из ProcessMiniAppPackGroupMessage, чтобы вызывать и при правке сообщения (дописали @leo).
// photoURL — фото текущего сообщения (опц.); replyToID — id сообщения, на которое отвечают (опц.),
// нужны, чтобы Лео видел родителя реплая и анализировал прикреплённые фото (vision).
func (b *Bot) answerLeoInPackGroupChatIfMentioned(d initdata.InitData, chatID int64, text string, userMsgID int64, photoURL string, replyToID int64) string {
	botName := ""
	if b.api != nil && b.api.Self.ID != 0 {
		botName = b.api.Self.UserName
	}
	// Лео отвечает, если его явно позвали (@leo/@бот) ИЛИ если это reply на его сообщение.
	if !textMentionsLeoForPackGroup(text, botName) && !b.packGroupReplyTargetsLeo(chatID, replyToID) {
		return ""
	}
	tgU := tgbotapiUserFromInitData(d.User)
	if tgU == nil {
		return ""
	}
	// Контекст реплая + анализ фото добавляем прямо в текст вопроса — handleAIQuestion
	// уже подмешивает историю чата, но точного родителя и картинок у него нет.
	question := b.buildPackGroupLeoQuestion(chatID, text, photoURL, replyToID)
	msg := &tgbotapi.Message{
		MessageID: 0,
		From:      tgU,
		Chat: &tgbotapi.Chat{
			ID:    chatID,
			Type:  "supergroup",
			Title: "Staya",
		},
		Text: question,
		Date: int(time.Now().Unix()),
	}
	ch := make(chan string, 2)
	b.handleAIQuestion(msg, question, ch, true, true)
	var reply string
	select {
	case reply = <-ch:
	case <-time.After(3 * time.Minute):
		return ""
	}
	if strings.TrimSpace(reply) == "" {
		return ""
	}
	leoName := "Лео"
	if b.api != nil && b.api.Self.ID != 0 && b.api.Self.UserName != "" {
		leoName = "@" + b.api.Self.UserName
	}
	leoReplyToID := int64(0)
	if userMsgID > 0 {
		leoReplyToID = userMsgID
	}
	if id, err := b.db.InsertMiniappPackGroupMessage(chatID, 0, leoName, true, reply, leoReplyToID, ""); err != nil {
		b.logger.Warnf("pack miniapp insert Leo row: %v", err)
	} else {
		b.indexPackGroupChatRAG(chatID, 0, "leo", reply, id)
		b.afterLeoPackGroupReplyInserted(chatID, d.User.ID, reply, id)
	}
	return reply
}

// buildPackGroupLeoQuestion собирает вопрос для Лео: исходный текст + явный родитель реплая +
// описание прикреплённых фото (через vision-модель). handleAIQuestion дополнительно подмешает
// историю чата, так что Лео видит «весь контекст», на который отвечает.
func (b *Bot) buildPackGroupLeoQuestion(chatID int64, text, photoURL string, replyToID int64) string {
	var sb strings.Builder

	// 1) Явный родитель реплая (текст + фото) — точнее, чем общая история.
	var parentPhotoURL, parentLabel string
	if replyToID > 0 {
		if parent, ok, err := b.db.GetMiniappPackGroupMessageInPack(chatID, replyToID); err == nil && ok {
			who := strings.TrimSpace(parent.Username)
			if parent.IsLeo {
				who = "Лео"
			}
			if who == "" {
				who = "участник"
			}
			parentLabel = who
			ptxt := strings.TrimSpace(parent.MessageText)
			if len(ptxt) > 500 {
				ptxt = ptxt[:500] + "…"
			}
			parentPhotoURL = strings.TrimSpace(parent.PhotoURL)
			line := "[Участник отвечает на сообщение от «" + who + "»"
			if ptxt != "" {
				line += ": «" + ptxt + "»"
			}
			if parentPhotoURL != "" {
				line += " (с фото)"
			}
			line += "]\n"
			sb.WriteString(line)
		}
	}

	// 2) Анализ фото: текущее сообщение и/или фото родителя реплая.
	if b.aiClient != nil && b.aiClient.HasVision() {
		if desc := b.describeImageForLeo(strings.TrimSpace(photoURL), text); desc != "" {
			sb.WriteString("[Фото, приложенное к сообщению участника — что на нём: " + desc + "]\n")
		}
		if parentPhotoURL != "" {
			if desc := b.describeImageForLeo(parentPhotoURL, ""); desc != "" {
				lbl := parentLabel
				if lbl == "" {
					lbl = "участника"
				}
				sb.WriteString("[Фото из сообщения «" + lbl + "», на которое отвечают — что на нём: " + desc + "]\n")
			}
		}
	}

	if sb.Len() == 0 {
		return text
	}
	sb.WriteString("\nСообщение участника: ")
	sb.WriteString(text)
	return sb.String()
}

// describeImageForLeo просит vision-модель кратко описать фото для контекста ответа Лео
// (чат стаи и лента). userText — текст рядом с фото (помогает понять, на что смотреть). Ошибки гасим.
func (b *Bot) describeImageForLeo(photoURL, userText string) string {
	if photoURL == "" || b.aiClient == nil || !b.aiClient.HasVision() {
		return ""
	}
	sys := "Ты — ассистент, кратко и по делу описываешь фото для чата фитнес-стаи. " +
		"Опиши, что на изображении: кто/что, обстановка, если это тренировка/еда/экипировка/скрин — отметь детали, важные для дружеского ответа. " +
		"Про людей пиши объективно: сколько человек в кадре, их видимый пол (мужчина/женщина) и что каждый делает или держит. " +
		"ВАЖНО: не считай, что человек в кадре — это обязательно автор поста. Автор мог сам фотографировать и быть ЗА кадром, " +
		"а в кадре может быть его спутник/попутчик или посторонний. Если по фото не ясно, кто из людей автор, — так и скажи, не домысливай. " +
		"2–4 коротких предложения, без воды и без выдумок."
	hint := strings.TrimSpace(userText)
	if hint != "" {
		hint = "Подпись участника к фото: " + hint
	}
	desc, err := b.aiClient.AnalyzeImages(sys, hint, []string{photoURL})
	if err != nil {
		b.logger.Warnf("pack group vision describe: %v", err)
		return ""
	}
	desc = strings.TrimSpace(desc)
	if len([]rune(desc)) > 600 {
		desc = string([]rune(desc)[:600]) + "…"
	}
	return desc
}

func (b *Bot) afterLeoPackGroupReplyInserted(packChatID, recipientUserID int64, replyText string, leoMessageID int64) {
	if b == nil || b.db == nil || recipientUserID == 0 || leoMessageID == 0 {
		return
	}
	if err := b.db.InsertPackGroupUnread(recipientUserID, packChatID, leoMessageID); err != nil {
		b.logger.Warnf("pack group leo unread insert: %v", err)
	}
	preview := truncateForDM(replyText, 160)
	body := "🦁 Лео ответил в чате стаи.\n\n«" + preview + "»\n\nОткрой мини-апп → «Стая» → «Чат»."
	b.sendTrainingThreadCommentDM(recipientUserID, body)
}

func (b *Bot) afterPackGroupReplyInserted(packChatID, commenterUserID int64, commenterName, commentText string, messageID, replyToID int64) {
	if b == nil || b.db == nil || replyToID == 0 || messageID == 0 {
		return
	}
	cn := strings.TrimSpace(commenterName)
	if cn == "" {
		cn = "Участник стаи"
	}
	var notifyUserID int64
	defer func() {
		if notifyUserID > 0 {
			b.notifyPackMemberMentionsInPackGroup(packChatID, commenterUserID, cn, commentText, messageID, map[int64]struct{}{notifyUserID: {}})
			return
		}
		b.notifyPackMemberMentionsInPackGroup(packChatID, commenterUserID, cn, commentText, messageID, nil)
	}()
	parent, ok, err := b.db.GetMiniappPackGroupMessageInPack(packChatID, replyToID)
	if err != nil {
		b.logger.Warnf("pack group reply parent lookup: %v", err)
		return
	}
	if !ok {
		return
	}
	if parent.IsLeo {
		return
	}
	if parent.FromUserID == 0 || parent.FromUserID == commenterUserID {
		return
	}
	notifyUserID = parent.FromUserID
	if notifyUserID == 0 {
		return
	}
	if err := b.db.InsertPackGroupUnread(notifyUserID, packChatID, messageID); err != nil {
		b.logger.Warnf("pack group unread insert: %v", err)
	}
	preview := truncateForDM(commentText, 160)
	commenterGender, _, _ := b.GetMiniappUserProfileJSONForAPI(commenterUserID, packChatID)
	commenterGender = strings.TrimSpace(strings.ToLower(commenterGender))
	var body string
	verb := ""
	switch commenterGender {
	case "m":
		verb = "ответил"
	case "f":
		verb = "ответила"
	}
	if verb == "" {
		body = "↩️ Ответ от " + cn + " на твоё сообщение в чате стаи.\n\n«" + preview + "»\n\nОткрой мини-апп → «Стая» → «Чат»."
	} else {
		body = "↩️ " + cn + " " + verb + " на твоё сообщение в чате стаи.\n\n«" + preview + "»\n\nОткрой мини-апп → «Стая» → «Чат»."
	}
	b.sendTrainingThreadCommentDM(notifyUserID, body)
}

// MiniappPackGroupUnreadCount — для бейджа на вкладке «Стая».
func (b *Bot) MiniappPackGroupUnreadCount(initD initdata.InitData, viewerUserID int64) (int64, error) {
	summary, err := b.MiniappPackGroupUnreadSummary(initD, viewerUserID)
	if err != nil {
		return 0, err
	}
	return summary.Count, nil
}

// PackGroupUnreadSummary — счётчик и id сообщений с непрочитанными ответами.
type PackGroupUnreadSummary struct {
	Count      int64
	MessageIDs []int64
}

func (b *Bot) MiniappPackGroupUnreadSummary(initD initdata.InitData, viewerUserID int64) (PackGroupUnreadSummary, error) {
	out := PackGroupUnreadSummary{}
	if err := b.AssertMiniAppPackChatAligns(initD); err != nil {
		return out, err
	}
	chatID := b.config.MonetizedChatID
	if chatID == 0 || b.db == nil {
		return out, nil
	}
	n, err := b.db.CountPackGroupUnread(viewerUserID, chatID)
	if err != nil {
		return out, err
	}
	ids, err := b.db.ListPackGroupUnreadMessageIDs(viewerUserID, chatID)
	if err != nil {
		return out, err
	}
	out.Count = n
	out.MessageIDs = ids
	return out, nil
}

// MiniappPackGroupUnreadClear — сброс бейджа при открытии общего чата.
func (b *Bot) MiniappPackGroupUnreadClear(initD initdata.InitData, viewerUserID int64) error {
	if err := b.AssertMiniAppPackChatAligns(initD); err != nil {
		return err
	}
	chatID := b.config.MonetizedChatID
	if chatID == 0 || b.db == nil {
		return nil
	}
	return b.db.ClearPackGroupUnread(viewerUserID, chatID)
}

// DeleteMiniAppPackGroupMessage — удалить своё сообщение в общем чате мини-аппа.
func (b *Bot) DeleteMiniAppPackGroupMessage(viewerUserID int64, initD initdata.InitData, messageID int64) (bool, error) {
	if err := b.AssertMiniAppPackChatAligns(initD); err != nil {
		return false, err
	}
	chatID := b.config.MonetizedChatID
	if chatID == 0 {
		return false, nil
	}
	// Админ (в т.ч. динамический) удаляет ЛЮБОЕ сообщение в чате стаи (модерация).
	// Обычный участник — только своё.
	if b.isAdminTelegramUser(viewerUserID) {
		hidden, err := b.db.AdminHidePackGroupMessage(chatID, messageID)
		if err != nil {
			return false, err
		}
		if hidden {
			_ = b.db.DeletePackGroupUnreadByMessageID(messageID)
		}
		return hidden, nil
	}
	ok, err := b.db.UserInPackOrPaid(viewerUserID, chatID, b.paywallEntryRequiresPayment())
	if err != nil {
		return false, err
	}
	if !ok {
		return false, ErrPackFeedForbidden
	}
	deleted, err := b.db.DeleteMiniappPackGroupMessageByAuthor(chatID, messageID, viewerUserID)
	if err != nil {
		return false, err
	}
	if deleted {
		_ = b.db.DeletePackGroupUnreadByMessageID(messageID)
	}
	return deleted, nil
}

// EditMiniAppPackGroupMessage — отредактировать своё (не Лео) сообщение в общем чате мини-аппа.
func (b *Bot) EditMiniAppPackGroupMessage(viewerUserID int64, initD initdata.InitData, messageID int64, text string) (bool, error) {
	if err := b.AssertMiniAppPackChatAligns(initD); err != nil {
		return false, err
	}
	chatID := b.config.MonetizedChatID
	if chatID == 0 {
		return false, nil
	}
	if b.config.IsAdminTelegramUser(viewerUserID) {
		// владелец тоже правит только своё — на уровне SQL WHERE from_user_id.
	} else {
		ok, err := b.db.UserInPackOrPaid(viewerUserID, chatID, b.paywallEntryRequiresPayment())
		if err != nil {
			return false, err
		}
		if !ok {
			return false, ErrPackFeedForbidden
		}
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return false, nil
	}
	if _, err := b.enforceUGC(text, moderation.SurfacePackGroupChat, viewerUserID); err != nil {
		return false, err
	}
	updated, err := b.db.UpdateMiniappPackGroupMessageByAuthor(chatID, messageID, viewerUserID, text)
	if err != nil {
		return false, err
	}
	if updated {
		b.indexPackGroupChatRAG(chatID, viewerUserID, "user", text, messageID)
		// Правка дописала призыв @leo — Лео должен ответить, как при обычной отправке.
		// Но только если он ещё не отвечал на это сообщение (иначе повторная правка плодит ответы).
		botName := ""
		if b.api != nil && b.api.Self.ID != 0 {
			botName = b.api.Self.UserName
		}
		if textMentionsLeoForPackGroup(text, botName) {
			already, lerr := b.db.LeoAlreadyRepliedToPackGroupMessage(chatID, messageID)
			if lerr != nil {
				b.logger.Warnf("pack group edit: leo-replied check: %v", lerr)
			} else if !already {
				// Фото и адресата реплая правкой не меняем — берём из текущей строки сообщения.
				var msgPhotoURL string
				var replyToID int64
				if row, ok, gerr := b.db.GetMiniappPackGroupMessageInPack(chatID, messageID); gerr == nil && ok {
					msgPhotoURL = row.PhotoURL
					if row.ReplyToID.Valid {
						replyToID = row.ReplyToID.Int64
					}
				}
				go func() {
					defer func() {
						if r := recover(); r != nil {
							b.logger.Errorf("pack group edit leo answer panic: %v", r)
						}
					}()
					b.answerLeoInPackGroupChatIfMentioned(initD, chatID, text, messageID, msgPhotoURL, replyToID)
				}()
			}
		}
	}
	return updated, nil
}

func allowedPackGroupChatEmoji(emoji string) (string, bool) {
	return allowedTrainingFeedEmoji(emoji)
}

// PackGroupChatReact — эмодзи-реакция на сообщение общего чата (повтор с той же эмодзи снимает).
func (b *Bot) PackGroupChatReact(viewerUserID int64, initD initdata.InitData, messageID int64, emoji string) error {
	if err := b.AssertMiniAppPackChatAligns(initD); err != nil {
		return err
	}
	chatID := b.config.MonetizedChatID
	if chatID == 0 {
		return ErrPackFeedForbidden
	}
	if b.config.IsAdminTelegramUser(viewerUserID) {
		// ok
	} else {
		ok, err := b.db.UserInPackOrPaid(viewerUserID, chatID, b.paywallEntryRequiresPayment())
		if err != nil {
			return err
		}
		if !ok {
			return ErrPackFeedForbidden
		}
	}
	if messageID <= 0 {
		return ErrPackGroupMessageNotFound
	}
	row, ok, err := b.db.GetMiniappPackGroupMessageInPack(chatID, messageID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrPackGroupMessageNotFound
	}
	_ = row
	em, ok := allowedPackGroupChatEmoji(emoji)
	if !ok {
		return ErrTrainingFeedInvalidEmoji
	}
	uname := displayNameFromInitData(initD)
	return b.db.SetPackGroupReaction(chatID, messageID, viewerUserID, uname, em)
}

// PackGroupChatReport — жалоба на сообщение в общем чате стаи.
func (b *Bot) PackGroupChatReport(viewerUserID int64, initD initdata.InitData, messageID int64) error {
	if err := b.AssertMiniAppPackChatAligns(initD); err != nil {
		return err
	}
	if err := b.assertPackFeedSocialViewer(viewerUserID); err != nil {
		return err
	}
	chatID := b.config.MonetizedChatID
	if messageID <= 0 {
		return ErrPackGroupMessageNotFound
	}
	row, ok, err := b.db.GetMiniappPackGroupMessageInPack(chatID, messageID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrPackGroupMessageNotFound
	}
	if row.IsLeo || row.FromUserID == 0 {
		return ErrFeedReportLeo
	}
	if row.FromUserID == viewerUserID {
		return ErrFeedReportSelf
	}
	reportID, err := b.db.InsertMiniappFeedReport(
		chatID, viewerUserID, "pack_group_message", messageID, 0, row.FromUserID, row.MessageText,
	)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
			return ErrFeedReportAlreadyExists
		}
		return err
	}
	b.notifyAdminsAboutFeedReport(reportID, viewerUserID, "pack_group_message", messageID, 0, row.FromUserID, row.MessageText)
	b.trackComplaintFiled(viewerUserID, "pack_group_message", messageID)
	return nil
}

func (b *Bot) enrichPackGroupChatReactions(msgs []*domain.PackGroupChatMessage, viewerUserID, chatID int64) []*domain.PackGroupChatMessage {
	if b == nil || b.db == nil || chatID == 0 || len(msgs) == 0 {
		return msgs
	}
	ids := make([]int64, 0, len(msgs))
	for _, m := range msgs {
		if m != nil && m.ID > 0 {
			ids = append(ids, m.ID)
		}
	}
	if len(ids) == 0 {
		return msgs
	}
	aggsMap, meMap, err := b.db.ListPackGroupReactionAggs(chatID, ids, viewerUserID)
	if err != nil {
		b.logger.Warnf("pack group reaction aggs: %v", err)
		return msgs
	}
	for _, m := range msgs {
		if m == nil {
			continue
		}
		meEmoji := meMap[m.ID]
		if aggs, ok := aggsMap[m.ID]; ok {
			feedAggs := make([]database.TrainingFeedReactionAgg, len(aggs))
			for i, a := range aggs {
				feedAggs[i] = database.TrainingFeedReactionAgg{Emoji: a.Emoji, Count: a.Count, Voters: a.Voters}
			}
			for _, a := range database.SortReactionAggsForDisplay(feedAggs, trainingFeedAllowedEmojis) {
				voters := make([]domain.PackGroupChatVoter, len(a.Voters))
				for i, v := range a.Voters {
					voters[i] = domain.PackGroupChatVoter{Name: v.Name, PhotoURL: v.PhotoURL}
				}
				m.Reactions = append(m.Reactions, domain.PackGroupChatReaction{
					Emoji:  a.Emoji,
					Count:  a.Count,
					Me:     meEmoji == a.Emoji,
					Voters: voters,
				})
			}
		}
	}
	return msgs
}

func (b *Bot) enrichPackGroupChatAuthorPhotos(msgs []*domain.PackGroupChatMessage, chatID int64, initDataRaw string) []*domain.PackGroupChatMessage {
	if b == nil || b.db == nil || chatID == 0 || len(msgs) == 0 {
		return msgs
	}
	publicBase := ""
	if b.config != nil {
		publicBase = strings.TrimSpace(b.config.MiniappPublicBaseURL)
	}
	var ids []int64
	seen := map[int64]struct{}{}
	for _, m := range msgs {
		if m == nil || m.IsLeo || m.UserID == 0 {
			continue
		}
		if _, ok := seen[m.UserID]; ok {
			continue
		}
		seen[m.UserID] = struct{}{}
		ids = append(ids, m.UserID)
	}
	if len(ids) == 0 {
		return msgs
	}
	urlMap, err := b.db.MiniappTelegramPhotoURLMap(chatID, ids)
	if err != nil {
		b.logger.Warnf("pack group chat author photos: %v", err)
		urlMap = map[int64]string{}
	}
	for _, m := range msgs {
		if m == nil || m.IsLeo || m.UserID == 0 {
			continue
		}
		m.AuthorPhotoURL = packFeedResolveAuthorPhoto(urlMap[m.UserID], publicBase, m.UserID, initDataRaw)
	}
	return msgs
}
