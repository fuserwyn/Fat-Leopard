package bot

import (
	"leo-bot/internal/i18n"
	"regexp"
	"strings"

	"leo-bot/internal/database"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	initdata "github.com/telegram-mini-apps/init-data-golang"
)

// Совместная workout: в отчёте о тренировке отмечают напарника через @ник
// Telegram или @имя участника стаи. Участнику стаи приходит уведомление, что его
// taggedи на совместной тренировке. Если отмеченного в стае нет, ему уходит
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

func jointTrainingVerb(lang i18n.Lang, gender string) string {
	if socialRU(lang) {
		return ruPastVerb(gender, "отметил")
	}
	return "tagged"
}

// jointTrainingMemberText — уведомление участнику стаи, которого отметили.
func jointTrainingMemberText(lang i18n.Lang, authorName, gender, reportText string) string {
	preview := truncateForDM(reportText, 160)
	if socialRU(lang) {
		return "🤝 " + authorName + " " + jointTrainingVerb(lang, gender) + " тебя на совместной тренировке.\n\n«" +
			preview + "»\n\n" + openPackFooter(lang)
	}
	return "🤝 " + authorName + " " + jointTrainingVerb(lang, gender) + " you on a joint workout.\n\n«" +
		preview + "»\n\n" + openPackFooter(lang)
}

// jointTrainingInviteText — инвайт тому, кого отметили, но кого нет в стае.
func jointTrainingInviteText(lang i18n.Lang, authorName, gender, reportText string) string {
	preview := truncateForDM(reportText, 160)
	if socialRU(lang) {
		return "🐆 " + authorName + " " + jointTrainingVerb(lang, gender) + " тебя на совместной тренировке в стае Fat Leopard.\n\n«" +
			preview + "»\n\nВступай в стаю — тренироваться вместе веселее!"
	}
	return "🐆 " + authorName + " " + jointTrainingVerb(lang, gender) + " you on a joint workout in the Fat Leopard pack.\n\n«" +
		preview + "»\n\nJoin the pack — training together is more fun!"
}

func jointTrainingJoinButton(lang i18n.Lang) string {
	if socialRU(lang) {
		return "Вступить в стаю"
	}
	return "Join the pack"
}

// jointTrainingForwardText — отметившему: бот не знает человека, ссылку нужно переслать самому.
// username может быть уже с «@» (label) или без: в тексте всегда один @.
func jointTrainingForwardText(lang i18n.Lang, username, link string) string {
	if socialRU(lang) {
		return "🤝 " + jointTrainingMentionLabel(username) + " ещё нет в стае, и Лео не может написать первым. " +
			"Перешли это приглашение на совместные тренировки:\n" + link
	}
	return "🤝 " + jointTrainingMentionLabel(username) + " isn't in the pack yet, and Leo can't message them first. " +
		"Forward this invite for joint workouts:\n" + link
}

func jointTrainingSentText(lang i18n.Lang, label string) string {
	if socialRU(lang) {
		return "🤝 Отправил " + label + " приглашение в стаю на совместные тренировки."
	}
	return "🤝 Sent " + label + " a pack invite for joint workouts."
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
		cn = "Pack member"
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
		lang := b.EffectiveUILang(uid)
		b.sendTrainingThreadCommentDM(uid, jointTrainingMemberText(lang, socialNameOr(cn, lang), gender, text))
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
	authorLang := b.EffectiveUILang(authorUserID)
	if inviteeID == 0 {
		b.notifyUserTextByID(authorUserID, authorUserID, jointTrainingForwardText(authorLang, label, link), "", 0)
		return
	}
	if b.api == nil {
		return
	}
	inviteeLang := b.EffectiveUILang(inviteeID)
	m := tgbotapi.NewMessage(inviteeID, jointTrainingInviteText(inviteeLang, socialNameOr(authorName, inviteeLang), gender, text))
	m.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonURL(jointTrainingJoinButton(inviteeLang), link)),
	)
	if _, err := b.api.Send(m); err != nil {
		b.logger.Warnf("joint training invite DM user=%d: %v", inviteeID, err)
		b.notifyUserTextByID(authorUserID, authorUserID, jointTrainingForwardText(authorLang, label, link), "", 0)
		return
	}
	b.notifyUserTextByID(authorUserID, authorUserID, jointTrainingSentText(authorLang, label), "", 0)
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

// JointTrainingMention — подсказка в мини-аппе при наборе «@» в описании workouts.
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
