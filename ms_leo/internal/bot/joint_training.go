package bot

import (
	"regexp"
	"strings"

	"leo-bot/internal/database"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	initdata "github.com/telegram-mini-apps/init-data-golang"
)

// Совместная тренировка: в отчёте о тренировке отмечают напарника через @ник
// Telegram или @имя участника стаи. Участнику стаи приходит уведомление, что его
// отметили на совместной тренировке. Если отмеченного в стае нет, ему уходит
// инвайт с личной ссылкой отметившего (?start=ref-<id>) — друг засчитывается
// приглашением. Бот не может написать первым тому, кто ни разу не открывал бота:
// тогда ссылку для пересылки получает сам отметивший.

// jointTrainingInviteCooldownDays — не чаще одного инвайта на пару «кто → @ник».
const jointTrainingInviteCooldownDays = 7

// reTelegramUsername — допустимый ник Telegram (без «@», в нижнем регистре).
var reTelegramUsername = regexp.MustCompile(`^[a-z][a-z0-9_]{3,31}$`)

func isTelegramUsernameToken(tok string) bool {
	return reTelegramUsername.MatchString(tok)
}

func jointTrainingVerb(gender string) string {
	switch strings.TrimSpace(strings.ToLower(gender)) {
	case "m":
		return "отметил"
	case "f":
		return "отметила"
	default:
		return "отметил(а)"
	}
}

// jointTrainingMemberText — уведомление участнику стаи, которого отметили.
func jointTrainingMemberText(authorName, gender, reportText string) string {
	preview := truncateForDM(reportText, 160)
	return "🤝 " + authorName + " " + jointTrainingVerb(gender) + " тебя на совместной тренировке.\n\n«" +
		preview + "»\n\nОткрой мини-апп → вкладка «Стая»."
}

// jointTrainingInviteText — инвайт тому, кого отметили, но кого нет в стае.
func jointTrainingInviteText(authorName, gender, reportText string) string {
	preview := truncateForDM(reportText, 160)
	return "🐆 " + authorName + " " + jointTrainingVerb(gender) + " тебя на совместной тренировке в стае Fat Leopard.\n\n«" +
		preview + "»\n\nПрисоединяйся к стае — тренироваться вместе веселее!"
}

// jointTrainingForwardText — отметившему: бот не знает человека, ссылку нужно переслать самому.
// username может быть уже с «@» (label) или без: в тексте всегда один @.
func jointTrainingForwardText(username, link string) string {
	return "🤝 " + jointTrainingMentionLabel(username) + " пока нет в стае, а Лео не может написать первым. " +
		"Перешли другу приглашение на совместные тренировки:\n" + link
}

// notifyJointTrainingMentions разбирает @-отметки в отчёте о тренировке.
func (b *Bot) notifyJointTrainingMentions(packChatID, authorUserID int64, authorName, text string) {
	if b == nil || b.db == nil || packChatID == 0 || authorUserID == 0 || strings.TrimSpace(text) == "" {
		return
	}
	botName := b.botUsername()
	tokens := extractPackMentionTokens(text, botName)
	if len(tokens) == 0 {
		return
	}
	byToken, err := b.db.LookupActivePackMembersByMentionTokens(packChatID, tokens)
	if err != nil {
		b.logger.Warnf("joint training mention lookup: %v", err)
		return
	}
	cn := strings.TrimSpace(authorName)
	if cn == "" {
		cn = "Участник стаи"
	}
	gender, _, _ := b.GetMiniappUserProfileJSONForAPI(authorUserID, packChatID)

	notified := map[int64]struct{}{}
	var outside []string
	for _, tok := range tokens {
		uid, ok := byToken[tok]
		if !ok || uid == 0 {
			outside = append(outside, tok)
			continue
		}
		if uid == authorUserID {
			continue
		}
		if _, dup := notified[uid]; dup {
			continue
		}
		notified[uid] = struct{}{}
		b.sendTrainingThreadCommentDM(uid, jointTrainingMemberText(cn, gender, text))
	}

	link := referralLink(botName, authorUserID)
	if link == "" {
		return
	}
	for _, tok := range outside {
		b.inviteToJointTraining(packChatID, authorUserID, cn, gender, tok, text, link)
	}
}

// inviteToJointTraining — инвайт в стаю отмеченному @username, которого в стае нет.
func (b *Bot) inviteToJointTraining(packChatID, authorUserID int64, authorName, gender, mentionToken, text, link string) {
	mentionToken = strings.TrimSpace(mentionToken)
	if mentionToken == "" {
		return
	}
	var (
		inviteeID int64
		err       error
	)
	if isTelegramUsernameToken(mentionToken) {
		inviteeID, err = b.db.FindBotUserOutsidePackByUsername(packChatID, mentionToken)
	} else {
		inviteeID, err = b.db.FindBotUserOutsidePackByDisplayName(packChatID, mentionToken)
	}
	if err != nil {
		b.logger.Warnf("joint training invitee lookup @%s: %v", mentionToken, err)
		return
	}
	if inviteeID == authorUserID {
		return
	}
	ok, err := b.db.ClaimJointTrainingInvite(authorUserID, mentionToken, inviteeID, jointTrainingInviteCooldownDays)
	if err != nil {
		b.logger.Warnf("joint training invite claim @%s: %v", mentionToken, err)
		return
	}
	if !ok {
		return
	}
	b.db.TrackEvent(database.AnalyticsEvent{
		Name: database.EventJointTrainingInvite, UserID: authorUserID, TelegramID: authorUserID,
		Payload: map[string]any{"invitee_user_id": inviteeID, "delivered": inviteeID != 0},
	})
	label := jointTrainingMentionLabel(mentionToken)
	if inviteeID == 0 {
		b.notifyUserTextByID(authorUserID, authorUserID, jointTrainingForwardText(label, link), "", 0)
		return
	}
	if b.api == nil {
		return
	}
	m := tgbotapi.NewMessage(inviteeID, jointTrainingInviteText(authorName, gender, text))
	m.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonURL("Вступить в стаю", link)),
	)
	if _, err := b.api.Send(m); err != nil {
		b.logger.Warnf("joint training invite DM user=%d: %v", inviteeID, err)
		b.notifyUserTextByID(authorUserID, authorUserID, jointTrainingForwardText(label, link), "", 0)
		return
	}
	b.notifyUserTextByID(authorUserID, authorUserID,
		"🤝 Отправил "+label+" приглашение в стаю на совместные тренировки.", "", 0)
}

func jointTrainingMentionLabel(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return "@"
	}
	if strings.HasPrefix(token, "@") {
		return token
	}
	return "@" + token
}

// JointTrainingMention — подсказка в мини-аппе при наборе «@» в описании тренировки.
// Mention — то, что вставляется после «@»: его потом узнаёт разбор отметок.
type JointTrainingMention struct {
	UserID  int64  `json:"user_id"`
	Name    string `json:"name"`
	Mention string `json:"mention"`
}

// reMentionableName — имя в стае, которое целиком ловится как @-отметка.
var reMentionableName = regexp.MustCompile(`^[^\s@,.!?;:()\[\]{}«»"']{1,40}$`)

// jointTrainingMentionToken — чем отметить участника: ник Telegram, а без ника —
// имя в стае, если в нём нет пробелов и знаков. "" — отметить нельзя.
func jointTrainingMentionToken(username, displayName string) string {
	if u := strings.TrimLeft(strings.TrimSpace(username), "@"); u != "" {
		return u
	}
	if n := strings.TrimSpace(displayName); reMentionableName.MatchString(n) {
		return n
	}
	return ""
}

// SearchJointTrainingMentionsForViewer — участники стаи для @-отметки напарника.
func (b *Bot) SearchJointTrainingMentionsForViewer(viewerUserID int64, initD initdata.InitData, query string) ([]JointTrainingMention, error) {
	if err := b.AssertMiniAppPackChatAligns(initD); err != nil {
		return nil, err
	}
	if err := b.assertPackFeedSocialViewer(viewerUserID); err != nil {
		return nil, err
	}
	rows, err := b.db.SearchPackMembersForMention(b.config.MonetizedChatID, viewerUserID, query, 8)
	if err != nil {
		return nil, err
	}
	out := make([]JointTrainingMention, 0, len(rows))
	for _, r := range rows {
		tok := jointTrainingMentionToken(r.Username, r.DisplayName)
		if tok == "" {
			continue
		}
		out = append(out, JointTrainingMention{
			UserID:  r.UserID,
			Name:    packMemberDisplayName(r.DisplayName, r.Username),
			Mention: tok,
		})
	}
	return out, nil
}
