package bot

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"leo-bot/internal/database"

	"leo-bot/internal/moderation"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	initdata "github.com/telegram-mini-apps/init-data-golang"
)

// Допустимые реакции по типам карточек в ленте мини-аппа (порядок — отображение).
var trainingFeedAllowedEmojis = []string{
	"🔥", "💪", "👏", "❤️", "🎉", "🦁", "⭐", "👍", "🙌", "✨", "🤝", "⚡", "🎯", "😤", "👀", "🙏", "😱",
	"🏆", "💯", "🥳", "🤩", "😮", "💦", "🤗", "👌", "🫶", "🧡", "💜", "🤙", "🫡", "🤘", "🏃", "🧘", "🤯", "💤",
}
var sickLeaveAllowedEmojis = []string{"😢", "😔", "🥺", "🤒", "🫂", "🙏", "❤️", "💙", "🌧️", "💤"}
var healthyAllowedEmojis = []string{"🎉", "🥳", "😄", "💚", "❤️", "👏", "🙌", "✨", "🌟", "💪"}
var packJoinAllowedEmojis = []string{"👋", "🎉", "❤️", "👏", "🙌"}

var (
	// ErrTrainingFeedSocialForbidden — нет доступа к ленте.
	ErrTrainingFeedSocialForbidden = errors.New("training feed social forbidden")
	// ErrTrainingFeedInvalidEmoji — эмодзи не из списка.
	ErrTrainingFeedInvalidEmoji = errors.New("invalid emoji")
	// ErrTrainingFeedParentNotFound — нет такого отчёта в стае.
	ErrTrainingFeedParentNotFound = errors.New("parent message not found")
	// ErrTrainingFeedThreadEmpty — пустой текст треда.
	ErrTrainingFeedThreadEmpty = errors.New("thread text empty")
	// ErrTrainingFeedThreadTooLong — слишком длинный комментарий.
	ErrTrainingFeedThreadTooLong = errors.New("thread text too long")
	// ErrTrainingFeedThreadDeleteNotFound — не найден комментарий или чужой / Лео.
	ErrTrainingFeedThreadDeleteNotFound = errors.New("training thread reply not found or forbidden")
	// ErrTrainingFeedThreadInvalidReply — reply_to не из этого отчёта / не найден.
	ErrTrainingFeedThreadInvalidReply = errors.New("training thread invalid reply reference")
)

func packFeedSupportsThread(messageType string) bool {
	switch messageType {
	case "training_done", "sick_leave", "healthy", userMessageTypeAdminPost, userMessageTypeAdminPoll, userMessageTypeDailyWisdom:
		return true
	default:
		return false
	}
}

// packFeedPostIsLeo — карточка ленты опубликована от имени Лео (мудрость дня или объявление/опрос).
func packFeedPostIsLeo(messageType, username string) bool {
	switch messageType {
	case userMessageTypeDailyWisdom:
		return true
	case userMessageTypeAdminPost, userMessageTypeAdminPoll:
		return strings.TrimSpace(username) == "Лео"
	default:
		return false
	}
}

// trainingThreadParentIsLeo — системный Лео (from_user_id=0) или голос «от имени Лео».
func trainingThreadParentIsLeo(row database.TrainingFeedThreadRow) bool {
	if row.FromUserID == 0 {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(row.PostedAs), "leo")
}

// shouldLeoReplyInFeedThread — ответ на Лео, @leo или комментарий под постом Лео.
func shouldLeoReplyInFeedThread(typ string, officialVoice, replyingToLeo, mentionsLeo, commentingOnLeoPost bool) bool {
	if officialVoice {
		return false
	}
	if !replyingToLeo && !mentionsLeo && !commentingOnLeoPost {
		return false
	}
	return packFeedSupportsThread(typ)
}

func feedThreadLeoPromptKind(typ string) string {
	switch typ {
	case "sick_leave":
		return "заявкой на больничный"
	case "healthy":
		return "сообщением о возвращении с больничного"
	case userMessageTypeAdminPost, userMessageTypeAdminPoll:
		return "объявлением стаи"
	case userMessageTypeDailyWisdom:
		return "мудростью дня"
	default:
		return "отчётом о тренировке"
	}
}

func packFeedSupportsReactions(messageType string) bool {
	switch messageType {
	case "training_done", "sick_leave", "healthy",
		userMessageTypeAdminPost, userMessageTypeAdminPoll,
		userMessageTypePackJoin, userMessageTypePackRejoin, userMessageTypeDailyWisdom:
		return true
	default:
		return false
	}
}

func allowedTrainingFeedEmoji(s string) (string, bool) {
	s = strings.TrimSpace(s)
	for _, e := range trainingFeedAllowedEmojis {
		if s == e {
			return e, true
		}
	}
	return "", false
}

func allowedEmojiForType(messageType, emoji string) (string, bool) {
	emoji = strings.TrimSpace(emoji)
	var allowed []string
	switch messageType {
	case "training_done", userMessageTypeDailyWisdom:
		allowed = trainingFeedAllowedEmojis
	case userMessageTypePackJoin, userMessageTypePackRejoin:
		allowed = packJoinAllowedEmojis
	case "sick_leave":
		allowed = sickLeaveAllowedEmojis
	case "healthy", userMessageTypeAdminPost, userMessageTypeAdminPoll:
		allowed = healthyAllowedEmojis
	default:
		return "", false
	}
	for _, e := range allowed {
		if emoji == e {
			return e, true
		}
	}
	return "", false
}

func (b *Bot) assertPackFeedSocialViewer(viewerUserID int64) error {
	if b == nil {
		return ErrTrainingFeedSocialForbidden
	}
	chatID := b.config.MonetizedChatID
	if chatID == 0 {
		return ErrTrainingFeedSocialForbidden
	}
	if b.config.IsAdminTelegramUser(viewerUserID) {
		return nil
	}
	ok, err := b.db.UserInPackOrPaid(viewerUserID, chatID, b.paywallEntryRequiresPayment())
	if err != nil {
		return err
	}
	if !ok {
		return ErrTrainingFeedSocialForbidden
	}
	return nil
}

// leoTrainingFeedReactionEmoji — эмодзи реакции Лео на отчёт: стабильно для одного поста,
// но разное для разных тренировок (из trainingFeedAllowedEmojis).
func leoTrainingFeedReactionEmoji(userMessageID int64) string {
	list := trainingFeedAllowedEmojis
	n := len(list)
	if n == 0 {
		return "👍"
	}
	if userMessageID < 0 {
		userMessageID = -userMessageID
	}
	idx := int((userMessageID*7919 + 104729) % int64(n))
	return list[idx]
}

// ensureLeoDefaultTrainingFeedReaction — автоматическая реакция от Лео на отчёт о тренировке в ленте стаи.
func (b *Bot) ensureLeoDefaultTrainingFeedReaction(packChatID, userMessageID int64) {
	if b == nil || b.db == nil || packChatID == 0 || userMessageID == 0 {
		return
	}
	emoji := leoTrainingFeedReactionEmoji(userMessageID)
	if _, err := b.db.SetTrainingFeedReaction(packChatID, userMessageID, 0, "Лео", emoji); err != nil {
		b.logger.Warnf("training feed leo default reaction: %v", err)
	}
}

// PackTrainingFeedReact — реакция на карточку ленты с соц. активностью (повтор с той же эмодзи снимает).
func (b *Bot) PackTrainingFeedReact(viewerUserID int64, initD initdata.InitData, userMessageID int64, emoji string) error {
	if err := b.AssertMiniAppPackChatAligns(initD); err != nil {
		return err
	}
	if err := b.assertPackFeedSocialViewer(viewerUserID); err != nil {
		return err
	}
	chatID := b.config.MonetizedChatID
	typ, has, err := b.db.GetUserMessageTypeByIDForChat(userMessageID, chatID)
	if err != nil {
		return err
	}
	if !has || !packFeedSupportsReactions(typ) {
		return ErrTrainingFeedParentNotFound
	}
	em, ok := allowedEmojiForType(typ, emoji)
	if !ok {
		return ErrTrainingFeedInvalidEmoji
	}
	uname := displayNameFromInitData(initD)
	added, err := b.db.SetTrainingFeedReaction(chatID, userMessageID, viewerUserID, uname, em)
	if err != nil {
		return err
	}
	if added {
		b.trackFeedReactionAdded(viewerUserID, "workout", em)
		b.notifyFeedPostLiked(chatID, viewerUserID, uname, userMessageID, em)
	}
	return nil
}

// PackTrainingFeedThreadPost — комментарий в треде под training_done/sick_leave/healthy.
// replyToThreadID — id строки miniapp_training_feed_thread, на которую отвечаем (как Reply в Telegram).
// photoURL — опциональное фото к комментарию (пусто, если без фото). При наличии фото текст может быть пустым.
// postAs — голос комментария: "self" (обычный), "leo" (от имени Лео) или "admin"
// (от имени админов). Голоса leo/admin доступны только админам в админских постах.
func (b *Bot) PackTrainingFeedThreadPost(viewerUserID int64, initD initdata.InitData, userMessageID int64, text string, replyToThreadID int64, photoURL string, postAs string) error {
	if err := b.AssertMiniAppPackChatAligns(initD); err != nil {
		return err
	}
	if err := b.assertPackFeedSocialViewer(viewerUserID); err != nil {
		return err
	}
	text = strings.TrimSpace(text)
	photoURL = strings.TrimSpace(photoURL)
	if text == "" && photoURL == "" {
		return ErrTrainingFeedThreadEmpty
	}
	chatID := b.config.MonetizedChatID
	typ, has, err := b.db.GetUserMessageTypeByIDForChat(userMessageID, chatID)
	if err != nil {
		return err
	}
	if !has || !packFeedSupportsThread(typ) {
		return ErrTrainingFeedParentNotFound
	}
	// Голоса leo/admin — только админ и только в админских постах (объявление/опрос).
	postAs = strings.TrimSpace(strings.ToLower(postAs))
	officialVoice := (postAs == "leo" || postAs == "admin") &&
		b.isAdminTelegramUser(viewerUserID) &&
		(typ == userMessageTypeAdminPost || typ == userMessageTypeAdminPoll)
	if !officialVoice {
		postAs = "self"
	}
	// Официальный голос (Лео/Админ) модерацию не проходит — это контент команды.
	if text != "" && !officialVoice {
		if _, err := b.enforceUGC(text, moderation.SurfaceFeedComment, viewerUserID); err != nil {
			return err
		}
	}
	replyingToLeo := false
	if replyToThreadID != 0 {
		parent, ok, err := b.db.GetTrainingFeedThreadRowInPack(replyToThreadID, chatID)
		if err != nil {
			return err
		}
		if !ok || parent.UserMessageID != userMessageID {
			return ErrTrainingFeedThreadInvalidReply
		}
		replyingToLeo = trainingThreadParentIsLeo(parent)
	}
	// Лео отвечает в треде, если его явно позвали через @leo (даже если реплай был на
	// сообщение другого участника) ИЛИ если это reply на сообщение самого Лео.
	botName := ""
	if b.api != nil && b.api.Self.ID != 0 {
		botName = b.api.Self.UserName
	}
	mentionsLeo := textMentionsLeoForPackGroup(text, botName)
	parentUsername := ""
	if packFeedPostIsLeo(typ, "") || typ == userMessageTypeAdminPost || typ == userMessageTypeAdminPoll {
		if u, uerr := b.db.GetUserMessageUsernameByIDForChat(userMessageID, chatID); uerr == nil {
			parentUsername = u
		}
	}
	commentingOnLeoPost := packFeedPostIsLeo(typ, parentUsername)
	uname := displayNameFromInitData(initD)
	// Голоса leo/admin сохраняют реального автора (from_user_id), чтобы другие
	// админы видели, кто комментил; отображение голоса решает posted_as.
	threadID, err := b.db.InsertTrainingFeedThreadReply(chatID, userMessageID, viewerUserID, uname, text, replyToThreadID, photoURL, postAs)
	if err != nil {
		return err
	}
	b.afterPackTrainingThreadInserted(chatID, userMessageID, viewerUserID, uname, text, threadID, replyToThreadID, typ)
	b.trackFeedCommentPosted(viewerUserID, utf8.RuneCountInString(text))
	// Автоответ Лео не нужен, если комментарий уже опубликован официальным голосом.
	// Ответ на Лео / @leo / комментарий под постом Лео (мудрость дня, объявление от Лео).
	if shouldLeoReplyInFeedThread(typ, officialVoice, replyingToLeo, mentionsLeo, commentingOnLeoPost) && threadID != 0 {
		txt := text
		uid := viewerUserID
		tid := threadID
		umid := userMessageID
		cid := chatID
		rtt := replyToThreadID
		mention := mentionsLeo
		onLeoPost := commentingOnLeoPost
		parentTyp := typ
		go func() {
			defer func() {
				if r := recover(); r != nil {
					b.logger.Errorf("leo feed thread reply panic: %v", r)
				}
			}()
			b.LeoReplyInFeedThread(cid, umid, tid, uid, txt, rtt, mention, onLeoPost, parentTyp)
		}()
	}
	return nil
}

func truncateForDM(s string, maxRunes int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= maxRunes {
		return string(r)
	}
	return string(r[:maxRunes]) + "…"
}

// Уведомление в личку + бейдж «Стая»: комментарий к отчёту или ответ на конкретное сообщение треда.
func (b *Bot) afterPackTrainingThreadInserted(packChatID, userMessageID, commenterUserID int64, commenterName, commentText string, threadReplyID, replyToParentThreadID int64, parentType string) {
	if b == nil || b.db == nil {
		return
	}
	cn := strings.TrimSpace(commenterName)
	if cn == "" {
		cn = "Участник стаи"
	}
	var notifyUserID int64
	defer func() {
		skip := map[int64]struct{}{}
		if notifyUserID > 0 {
			skip[notifyUserID] = struct{}{}
		}
		b.notifyPackMemberMentionsInFeedComment(packChatID, commenterUserID, cn, commentText, threadReplyID, skip)
	}()
	if replyToParentThreadID != 0 {
		parent, ok, err := b.db.GetTrainingFeedThreadRowInPack(replyToParentThreadID, packChatID)
		if err != nil {
			b.logger.Warnf("training thread parent lookup: %v", err)
			return
		}
		if !ok {
			return
		}
		if parent.FromUserID != 0 {
			if parent.FromUserID == commenterUserID {
				return
			}
			notifyUserID = parent.FromUserID
		} else {
			aid, ok2, err2 := b.db.GetUserMessageAuthorUserID(packChatID, userMessageID)
			if err2 != nil {
				b.logger.Warnf("training thread author lookup: %v", err2)
				return
			}
			if !ok2 || aid == 0 || aid == commenterUserID {
				return
			}
			notifyUserID = aid
		}
	} else {
		aid, ok, err := b.db.GetUserMessageAuthorUserID(packChatID, userMessageID)
		if err != nil {
			b.logger.Warnf("training thread author lookup: %v", err)
			return
		}
		if !ok || aid == 0 || aid == commenterUserID {
			return
		}
		notifyUserID = aid
	}
	if notifyUserID == 0 {
		return
	}
	if err := b.db.InsertTrainingThreadUnread(notifyUserID, packChatID, threadReplyID); err != nil {
		b.logger.Warnf("training thread unread insert: %v", err)
	}
	preview := truncateForDM(commentText, 160)
	if strings.TrimSpace(preview) == "" {
		preview = "📷 Фото"
	}
	commenterGender, _, _ := b.GetMiniappUserProfileJSONForAPI(commenterUserID, packChatID)
	commenterGender = strings.TrimSpace(strings.ToLower(commenterGender))
	var body string
	if replyToParentThreadID != 0 {
		verb := ""
		switch commenterGender {
		case "m":
			verb = "ответил"
		case "f":
			verb = "ответила"
		}
		if verb == "" {
			body = "↩️ Ответ от " + cn + " на твой комментарий в стае.\n\n«" + preview + "»\n\nОткрой мини-апп → вкладка «Стая»."
		} else {
			body = "↩️ " + cn + " " + verb + " на твой комментарий в стае.\n\n«" + preview + "»\n\nОткрой мини-апп → вкладка «Стая»."
		}
	} else {
		what := "твою тренировку"
		if parentType == "sick_leave" || parentType == "healthy" {
			what = "твой статус по больничному"
		} else if parentType == userMessageTypeAdminPost {
			what = "пост админа"
		} else if parentType == userMessageTypeAdminPoll {
			what = "опрос админа"
		}
		verb := ""
		switch commenterGender {
		case "m":
			verb = "прокомментировал"
		case "f":
			verb = "прокомментировала"
		}
		if verb == "" {
			body = "💬 Комментарий от " + cn + " к " + what + " в стае.\n\n«" + preview + "»\n\nОткрой мини-апп → вкладка «Стая»."
		} else {
			body = "💬 " + cn + " " + verb + " " + what + " в стае.\n\n«" + preview + "»\n\nОткрой мини-апп → вкладка «Стая»."
		}
	}
	b.sendTrainingThreadCommentDM(notifyUserID, body)
}

// Всегда в Telegram-личку, без очереди мини-аппа (уведомления-алерт).
func (b *Bot) sendTrainingThreadCommentDM(telegramUserID int64, text string) {
	if b == nil || b.api == nil || telegramUserID == 0 || strings.TrimSpace(text) == "" {
		return
	}
	m := tgbotapi.NewMessage(telegramUserID, text)
	if _, err := b.api.Send(m); err != nil {
		b.logger.Warnf("training thread comment DM user=%d: %v", telegramUserID, err)
	}
}

// MiniappTrainingThreadUnreadCount — для бейджа на вкладке «Стая».
func (b *Bot) MiniappTrainingThreadUnreadCount(initD initdata.InitData, viewerUserID int64) (int64, error) {
	n, _, err := b.MiniappTrainingThreadUnreadSummary(initD, viewerUserID)
	return n, err
}

// MiniappTrainingThreadUnreadSummary — счётчик и id отчётов с непрочитанными комментариями.
func (b *Bot) MiniappTrainingThreadUnreadSummary(initD initdata.InitData, viewerUserID int64) (count int64, userMessageIDs []int64, err error) {
	if err := b.AssertMiniAppPackChatAligns(initD); err != nil {
		return 0, nil, err
	}
	chatID := b.config.MonetizedChatID
	if chatID == 0 || b.db == nil {
		return 0, nil, nil
	}
	count, err = b.db.CountTrainingThreadUnread(viewerUserID, chatID)
	if err != nil {
		return 0, nil, err
	}
	userMessageIDs, err = b.db.ListTrainingThreadUnreadUserMessageIDs(viewerUserID, chatID)
	if err != nil {
		return 0, nil, err
	}
	return count, userMessageIDs, nil
}

// markTrainingThreadReplyUnread — бейдж «Стая» + опциональная личка (системный текст, не LLM).
func (b *Bot) markTrainingThreadReplyUnread(packChatID, notifyUserID, threadReplyID int64, dmBody string) {
	if b == nil || b.db == nil || notifyUserID == 0 || threadReplyID == 0 {
		return
	}
	if err := b.db.InsertTrainingThreadUnread(notifyUserID, packChatID, threadReplyID); err != nil {
		b.logger.Warnf("training thread unread insert: %v", err)
	}
	if strings.TrimSpace(dmBody) != "" {
		b.sendTrainingThreadCommentDM(notifyUserID, dmBody)
	}
}

// MiniappTrainingThreadUnreadClear — сброс бейджа; userMessageID>0 — только под одним отчётом.
func (b *Bot) MiniappTrainingThreadUnreadClear(initD initdata.InitData, viewerUserID int64, userMessageID int64) error {
	if err := b.AssertMiniAppPackChatAligns(initD); err != nil {
		return err
	}
	chatID := b.config.MonetizedChatID
	if chatID == 0 || b.db == nil {
		return nil
	}
	if userMessageID > 0 {
		return b.db.ClearTrainingThreadUnreadForUserMessage(viewerUserID, chatID, userMessageID)
	}
	return b.db.ClearTrainingThreadUnread(viewerUserID, chatID)
}

// PackTrainingFeedThreadDelete — удалить свою реплику в треде (ответы Лео не удаляются).
func (b *Bot) PackTrainingFeedThreadDelete(viewerUserID int64, initD initdata.InitData, threadReplyID int64) (parentUserMessageID int64, err error) {
	if err := b.AssertMiniAppPackChatAligns(initD); err != nil {
		return 0, err
	}
	if err := b.assertPackFeedSocialViewer(viewerUserID); err != nil {
		return 0, err
	}
	if threadReplyID == 0 {
		return 0, ErrTrainingFeedThreadDeleteNotFound
	}
	chatID := b.config.MonetizedChatID
	// Админ может удалить ЛЮБОЙ коммент в треде (модерация). Обычный участник — только свой.
	if b.isAdminTelegramUser(viewerUserID) {
		row, ok, rerr := b.db.GetTrainingFeedThreadRowInPack(threadReplyID, chatID)
		if rerr != nil {
			return 0, rerr
		}
		if !ok {
			return 0, ErrTrainingFeedThreadDeleteNotFound
		}
		deleted, derr := b.db.AdminHideTrainingFeedThreadReply(chatID, threadReplyID)
		if derr != nil {
			return 0, derr
		}
		if !deleted {
			return 0, ErrTrainingFeedThreadDeleteNotFound
		}
		return row.UserMessageID, nil
	}
	parentID, deleted, err := b.db.DeleteTrainingFeedThreadReply(chatID, threadReplyID, viewerUserID)
	if err != nil {
		return 0, err
	}
	if !deleted {
		return 0, ErrTrainingFeedThreadDeleteNotFound
	}
	if err := b.db.DeleteTrainingThreadUnreadByReplyID(threadReplyID); err != nil {
		b.logger.Warnf("training thread unread delete: %v", err)
	}
	return parentID, nil
}

// PackTrainingFeedThreadEdit — правка своего комментария в треде (не Лео / не официальный голос).
func (b *Bot) PackTrainingFeedThreadEdit(viewerUserID int64, initD initdata.InitData, threadReplyID int64, text string) (parentUserMessageID int64, err error) {
	if err := b.AssertMiniAppPackChatAligns(initD); err != nil {
		return 0, err
	}
	if err := b.assertPackFeedSocialViewer(viewerUserID); err != nil {
		return 0, err
	}
	if threadReplyID == 0 {
		return 0, ErrTrainingFeedThreadDeleteNotFound
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, ErrTrainingFeedThreadEmpty
	}
	if _, err := b.enforceUGC(text, moderation.SurfaceFeedComment, viewerUserID); err != nil {
		return 0, err
	}
	chatID := b.config.MonetizedChatID
	row, ok, err := b.db.GetTrainingFeedThreadRowInPack(threadReplyID, chatID)
	if err != nil {
		return 0, err
	}
	if !ok || row.FromUserID != viewerUserID || row.FromUserID == 0 {
		return 0, ErrTrainingFeedThreadDeleteNotFound
	}
	postedAs := strings.TrimSpace(strings.ToLower(row.PostedAs))
	if postedAs == "" {
		postedAs = "self"
	}
	if postedAs != "self" {
		return 0, ErrTrainingFeedThreadDeleteNotFound
	}
	parentID, updated, err := b.db.UpdateTrainingFeedThreadReplyByAuthor(chatID, threadReplyID, viewerUserID, text)
	if err != nil {
		return 0, err
	}
	if !updated {
		return 0, ErrTrainingFeedThreadDeleteNotFound
	}
	return parentID, nil
}

// PackFeedPostEdit — правка текста своего поста в ленте (training_done / healthy).
func (b *Bot) PackFeedPostEdit(viewerUserID int64, initD initdata.InitData, userMessageID int64, text string) error {
	if err := b.AssertMiniAppPackChatAligns(initD); err != nil {
		return err
	}
	if err := b.assertPackFeedSocialViewer(viewerUserID); err != nil {
		return err
	}
	if userMessageID == 0 {
		return ErrTrainingFeedParentNotFound
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return ErrTrainingFeedThreadEmpty
	}
	chatID := b.config.MonetizedChatID
	typ, has, err := b.db.GetUserMessageTypeByIDForChat(userMessageID, chatID)
	if err != nil {
		return err
	}
	if !has || (typ != "training_done" && typ != "healthy") {
		return ErrTrainingFeedParentNotFound
	}
	surface := moderation.SurfaceFeedComment
	if typ == "training_done" {
		surface = moderation.SurfaceTrainingNote
		checkText := moderation.TextForTrainingModeration(text)
		if _, err := b.enforceUGC(checkText, surface, viewerUserID); err != nil {
			return err
		}
	} else if _, err := b.enforceUGC(text, surface, viewerUserID); err != nil {
		return err
	}
	updated, err := b.db.UpdateUserMessageTextByAuthor(userMessageID, chatID, viewerUserID, text)
	if err != nil {
		return err
	}
	if !updated {
		return ErrTrainingFeedParentNotFound
	}
	return nil
}

// PackFeedPostSetPhoto — прикрепить/заменить фото своего поста ленты (training_done / healthy).
// Возвращает URL прежнего фото (пусто, если его не было или оно совпадает) — вызывающий слой удалит его из хранилища.
func (b *Bot) PackFeedPostSetPhoto(viewerUserID int64, initD initdata.InitData, userMessageID int64, newPhotoURL string) (oldPhotoURL string, err error) {
	newPhotoURL = strings.TrimSpace(newPhotoURL)
	if newPhotoURL == "" {
		return "", ErrTrainingFeedThreadEmpty
	}
	return b.packFeedPostUpdatePhoto(viewerUserID, initD, userMessageID, newPhotoURL)
}

// PackFeedPostDeletePhoto — удалить фото своего поста ленты. Возвращает URL удалённого фото (для очистки хранилища).
func (b *Bot) PackFeedPostDeletePhoto(viewerUserID int64, initD initdata.InitData, userMessageID int64) (oldPhotoURL string, err error) {
	return b.packFeedPostUpdatePhoto(viewerUserID, initD, userMessageID, "")
}

func (b *Bot) packFeedPostUpdatePhoto(viewerUserID int64, initD initdata.InitData, userMessageID int64, newPhotoURL string) (oldPhotoURL string, err error) {
	if err := b.AssertMiniAppPackChatAligns(initD); err != nil {
		return "", err
	}
	if err := b.assertPackFeedSocialViewer(viewerUserID); err != nil {
		return "", err
	}
	if userMessageID == 0 {
		return "", ErrTrainingFeedParentNotFound
	}
	chatID := b.config.MonetizedChatID
	typ, has, err := b.db.GetUserMessageTypeByIDForChat(userMessageID, chatID)
	if err != nil {
		return "", err
	}
	if !has || (typ != "training_done" && typ != "healthy") {
		return "", ErrTrainingFeedParentNotFound
	}
	oldPhotoURL, err = b.db.GetTrainingPhotoURLByMessageID(chatID, userMessageID)
	if err != nil {
		return "", err
	}
	updated, err := b.db.UpdateUserMessagePhotoURLByAuthor(userMessageID, chatID, viewerUserID, newPhotoURL)
	if err != nil {
		return "", err
	}
	if !updated {
		return "", ErrTrainingFeedParentNotFound
	}
	if strings.TrimSpace(oldPhotoURL) == strings.TrimSpace(newPhotoURL) {
		return "", nil
	}
	return strings.TrimSpace(oldPhotoURL), nil
}

// PackTrainingFeedThreadLikeToggle — лайк комментария в треде training_done.
func (b *Bot) PackTrainingFeedThreadLikeToggle(viewerUserID int64, initD initdata.InitData, threadReplyID int64) (parentUserMessageID int64, err error) {
	if err := b.AssertMiniAppPackChatAligns(initD); err != nil {
		return 0, err
	}
	if err := b.assertPackFeedSocialViewer(viewerUserID); err != nil {
		return 0, err
	}
	if threadReplyID == 0 {
		return 0, ErrTrainingFeedThreadDeleteNotFound
	}
	chatID := b.config.MonetizedChatID
	row, ok, err := b.db.GetTrainingFeedThreadRowInPack(threadReplyID, chatID)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, ErrTrainingFeedThreadDeleteNotFound
	}
	liked, err := b.db.ToggleTrainingFeedThreadLike(chatID, threadReplyID, viewerUserID)
	if err != nil {
		return 0, err
	}
	if liked {
		b.notifyFeedCommentLiked(chatID, viewerUserID, displayNameFromInitData(initD), threadReplyID, row.FromUserID)
	}
	return row.UserMessageID, nil
}

func (b *Bot) threadRowsToPackReplies(rows []database.TrainingFeedThreadRow, viewerUserID, packChatID int64, initDataRaw string) []PackFeedThreadReply {
	out := make([]PackFeedThreadReply, 0, len(rows))
	if len(rows) == 0 {
		return out
	}
	// Атрибуцию (какой админ писал от имени Лео/Админ) видят только админы.
	viewerIsAdmin := b.isAdminTelegramUser(viewerUserID)
	var refIDs []int64
	seen := map[int64]struct{}{}
	for _, t := range rows {
		if t.ReplyToID.Valid && t.ReplyToID.Int64 != 0 {
			rid := t.ReplyToID.Int64
			if _, ok := seen[rid]; !ok {
				seen[rid] = struct{}{}
				refIDs = append(refIDs, rid)
			}
		}
	}
	parentByID := map[int64]database.TrainingFeedThreadRow{}
	if len(refIDs) > 0 && b.db != nil {
		m, err := b.db.ListTrainingFeedThreadRowsByIDs(packChatID, refIDs)
		if err != nil {
			b.logger.Warnf("training thread reply parents: %v", err)
		} else {
			parentByID = m
		}
	}
	likeMap := map[int64]database.ThreadLikeAgg{}
	if len(rows) > 0 && b.db != nil {
		ids := make([]int64, 0, len(rows))
		for _, t := range rows {
			ids = append(ids, t.ID)
		}
		m, err := b.db.ListTrainingFeedThreadLikeAggs(packChatID, ids, viewerUserID)
		if err != nil {
			b.logger.Warnf("training thread likes list: %v", err)
		} else {
			likeMap = m
		}
	}
	for _, t := range rows {
		// Системный Лео (from_user_id = 0) ИЛИ админ, выбравший голос «Лео».
		isLeoVoice := t.FromUserID == 0 || t.PostedAs == "leo"
		isAdminVoice := t.PostedAs == "admin"
		pr := PackFeedThreadReply{
			ID:        t.ID,
			UserID:    t.FromUserID,
			Username:  t.Username,
			Text:      t.MessageText,
			PhotoURL:  b.canonicalMiniappTrainingPhotoURL(t.PhotoURL),
			CreatedAt: t.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			IsYou:     t.FromUserID != 0 && t.FromUserID == viewerUserID,
			IsLeo:     isLeoVoice,
			IsAdmin:   isAdminVoice,
		}
		if t.EditedAt.Valid {
			pr.EditedAt = t.EditedAt.Time.UTC().Format("2006-01-02T15:04:05Z07:00")
		}
		// Атрибуция: только админам и только для официальных голосов с реальным автором.
		if viewerIsAdmin && (isAdminVoice || (isLeoVoice && t.FromUserID != 0)) {
			pr.AdminName = strings.TrimSpace(t.Username)
		}
		if t.ReplyToID.Valid && t.ReplyToID.Int64 != 0 {
			rid := t.ReplyToID.Int64
			pr.ReplyToID = rid
			if p, ok := parentByID[rid]; ok {
				pr.ReplyToIsLeo = p.FromUserID == 0 || p.PostedAs == "leo"
				pr.ReplyToIsAdmin = p.PostedAs == "admin"
				if pr.ReplyToIsLeo {
					pr.ReplyToUsername = "Лео"
				} else if pr.ReplyToIsAdmin {
					pr.ReplyToUsername = "Админ"
				} else {
					pr.ReplyToUsername = strings.TrimSpace(p.Username)
					if pr.ReplyToUsername == "" {
						pr.ReplyToUsername = fmt.Sprintf("Участник %d", p.FromUserID)
					}
				}
				pr.ReplyToText = truncateForDM(p.MessageText, 100)
			}
		}
		if l, ok := likeMap[t.ID]; ok {
			pr.LikeCount = l.Count
			pr.LikeMe = l.Me
			pr.LikeVoters = b.packVoters(l.Voters, initDataRaw)
		}
		out = append(out, pr)
	}
	return out
}

// PackFeedThreadRepliesForViewer — полный тред под одним отчётом (после POST комментария и для согласованности с лентой).
// initDataRaw — для прокси-аватаров лайкнувших комментарий.
func (b *Bot) PackFeedThreadRepliesForViewer(viewerUserID, userMessageID int64, initDataRaw string) ([]PackFeedThreadReply, error) {
	if b == nil || b.db == nil {
		return nil, fmt.Errorf("bot unavailable")
	}
	m, err := b.db.ListTrainingFeedThreadByMessages([]int64{userMessageID})
	if err != nil {
		return nil, err
	}
	chatID := b.config.MonetizedChatID
	return b.threadRowsToPackReplies(m[userMessageID], viewerUserID, chatID, initDataRaw), nil
}

func allowedEmojiListForFeedType(messageType string) []string {
	switch messageType {
	case "training_done", userMessageTypeDailyWisdom:
		return trainingFeedAllowedEmojis
	case userMessageTypePackJoin, userMessageTypePackRejoin:
		return packJoinAllowedEmojis
	case "sick_leave":
		return sickLeaveAllowedEmojis
	case "healthy", userMessageTypeAdminPost, userMessageTypeAdminPoll:
		return healthyAllowedEmojis
	default:
		return nil
	}
}

// enrichPackFeedTrainingSocial — реакции и треды для карточек ленты с соц. активностью.
// initDataRaw — для прокси-аватаров отреагировавших/лайкнувших.
func (b *Bot) enrichPackFeedTrainingSocial(items []PackFeedItem, viewerUserID int64, chatID int64, initDataRaw string) []PackFeedItem {
	socialIDs := make([]int64, 0)
	reactionIDs := make([]int64, 0)
	for _, it := range items {
		if packFeedSupportsThread(it.Type) {
			socialIDs = append(socialIDs, it.ID)
		}
		if packFeedSupportsReactions(it.Type) {
			reactionIDs = append(reactionIDs, it.ID)
		}
	}
	if len(socialIDs) == 0 && len(reactionIDs) == 0 {
		return items
	}
	aggsMap := map[int64][]database.TrainingFeedReactionAgg{}
	meMap := map[int64]string{}
	if len(reactionIDs) > 0 {
		var err error
		aggsMap, meMap, err = b.db.ListTrainingFeedReactionAggs(chatID, reactionIDs, viewerUserID)
		if err != nil {
			b.logger.Warnf("pack feed reaction aggs: %v", err)
			return items
		}
	}
	threadMap := map[int64][]database.TrainingFeedThreadRow{}
	if len(socialIDs) > 0 {
		var err error
		threadMap, err = b.db.ListTrainingFeedThreadByMessages(socialIDs)
		if err != nil {
			b.logger.Warnf("pack feed thread list: %v", err)
			threadMap = map[int64][]database.TrainingFeedThreadRow{}
		}
	}
	for i := range items {
		id := items[i].ID
		if packFeedSupportsReactions(items[i].Type) {
			meEmoji := meMap[id]
			if aggs, ok := aggsMap[id]; ok {
				allowed := allowedEmojiListForFeedType(items[i].Type)
				for _, a := range database.SortReactionAggsForDisplay(aggs, allowed) {
					items[i].Reactions = append(items[i].Reactions, PackFeedReaction{
						Emoji:  a.Emoji,
						Count:  a.Count,
						Me:     meEmoji == a.Emoji,
						Voters: b.packVoters(a.Voters, initDataRaw),
					})
				}
			}
		}
		if !packFeedSupportsThread(items[i].Type) {
			continue
		}
		if thr, ok := threadMap[id]; ok {
			items[i].Thread = b.threadRowsToPackReplies(thr, viewerUserID, chatID, initDataRaw)
		}
	}
	return items
}

// PackFeedAdminHidePost — админ скрывает пост ленты (soft hide, можно вернуть в «Скрытое»).
func (b *Bot) PackFeedAdminHidePost(viewerUserID int64, initD initdata.InitData, userMessageID int64) error {
	if err := b.AssertMiniAppPackChatAligns(initD); err != nil {
		return err
	}
	if b == nil || b.db == nil {
		return fmt.Errorf("bot unavailable")
	}
	if !b.isAdminTelegramUser(viewerUserID) {
		return ErrPackFeedForbidden
	}
	if userMessageID <= 0 {
		return ErrTrainingFeedParentNotFound
	}
	chatID := b.config.MonetizedChatID
	if chatID == 0 {
		return ErrTrainingFeedParentNotFound
	}
	ok, err := b.db.AdminHideFeedUserMessage(chatID, userMessageID, "admin_delete")
	if err != nil {
		return err
	}
	if !ok {
		return ErrTrainingFeedParentNotFound
	}
	return nil
}

// PackFeedAdminDeletePost — устаревшее имя; скрывает пост (не удаляет из БД).
func (b *Bot) PackFeedAdminDeletePost(viewerUserID int64, initD initdata.InitData, userMessageID int64) (deletedPhotoURL string, err error) {
	if err := b.PackFeedAdminHidePost(viewerUserID, initD, userMessageID); err != nil {
		return "", err
	}
	return "", nil
}
