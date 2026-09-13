package bot

import (
	"regexp"
	"strings"
)

// @username или @Имя (display_name) — без пробелов и знаков препинания.
var rePackMemberMentions = regexp.MustCompile(`(?i)@([^\s@,.!?;:()\[\]{}«»"']{1,40})`)

type packMentionSurface int

const (
	packMentionFeedPost packMentionSurface = iota
	packMentionFeedComment
	packMentionPackGroupMessage
)

func extractPackMentionTokens(text, botUsername string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	reserved := map[string]struct{}{
		"leo": {},
	}
	if bu := strings.TrimSpace(strings.ToLower(strings.TrimPrefix(botUsername, "@"))); bu != "" {
		reserved[bu] = struct{}{}
	}
	seen := map[string]struct{}{}
	var out []string
	for _, m := range rePackMemberMentions.FindAllStringSubmatch(text, -1) {
		if len(m) < 2 {
			continue
		}
		tok := strings.TrimSpace(strings.ToLower(m[1]))
		if tok == "" {
			continue
		}
		if _, skip := reserved[tok]; skip {
			continue
		}
		if _, dup := seen[tok]; dup {
			continue
		}
		seen[tok] = struct{}{}
		out = append(out, tok)
	}
	return out
}

func (b *Bot) resolvePackMentionUserIDs(packChatID int64, text, botUsername string) []int64 {
	if b == nil || b.db == nil || packChatID == 0 {
		return nil
	}
	tokens := extractPackMentionTokens(text, botUsername)
	if len(tokens) == 0 {
		return nil
	}
	byToken, err := b.db.LookupActivePackMembersByMentionTokens(packChatID, tokens)
	if err != nil {
		b.logger.Warnf("pack mention lookup: %v", err)
		return nil
	}
	seen := map[int64]struct{}{}
	var ids []int64
	for _, uid := range byToken {
		if uid == 0 {
			continue
		}
		if _, dup := seen[uid]; dup {
			continue
		}
		seen[uid] = struct{}{}
		ids = append(ids, uid)
	}
	return ids
}

func packMentionVerb(gender string) string {
	switch strings.TrimSpace(strings.ToLower(gender)) {
	case "m":
		return "упомянул"
	case "f":
		return "упомянула"
	default:
		return ""
	}
}

func packMentionPlace(surface packMentionSurface) string {
	switch surface {
	case packMentionFeedPost:
		return "в посте"
	case packMentionFeedComment:
		return "в комментарии"
	default:
		return "в чате"
	}
}

func (b *Bot) notifyPackMemberMentions(
	packChatID, authorUserID int64,
	authorName, text string,
	surface packMentionSurface,
	refID int64,
	skipUserIDs map[int64]struct{},
) {
	if b == nil || b.db == nil || packChatID == 0 || strings.TrimSpace(text) == "" {
		return
	}
	botName := ""
	if b.api != nil && b.api.Self.ID != 0 {
		botName = b.api.Self.UserName
	}
	mentioned := b.resolvePackMentionUserIDs(packChatID, text, botName)
	if len(mentioned) == 0 {
		return
	}
	cn := strings.TrimSpace(authorName)
	if cn == "" {
		cn = "Участник стаи"
	}
	preview := truncateForDM(text, 160)
	if strings.TrimSpace(preview) == "" {
		preview = "📷 Фото"
	}
	place := packMentionPlace(surface)
	authorGender, _, _ := b.GetMiniappUserProfileJSONForAPI(authorUserID, packChatID)
	verb := packMentionVerb(authorGender)

	for _, uid := range mentioned {
		if uid == 0 || uid == authorUserID {
			continue
		}
		if skipUserIDs != nil {
			if _, skip := skipUserIDs[uid]; skip {
				continue
			}
		}
		switch surface {
		case packMentionFeedComment:
			if refID > 0 {
				if err := b.db.InsertTrainingThreadUnread(uid, packChatID, refID); err != nil {
					b.logger.Warnf("mention training thread unread insert: %v", err)
				}
			}
		case packMentionPackGroupMessage:
			if refID > 0 {
				if err := b.db.InsertPackGroupUnread(uid, packChatID, refID); err != nil {
					b.logger.Warnf("mention pack group unread insert: %v", err)
				}
			}
		}
		var body string
		if verb == "" {
			body = "📣 " + cn + " отметил(а) тебя " + place + " в стае.\n\n«" + preview + "»\n\nОткрой мини-апп → вкладка «Стая»."
		} else {
			body = "📣 " + cn + " " + verb + " тебя " + place + " в стае.\n\n«" + preview + "»\n\nОткрой мини-апп → вкладка «Стая»."
		}
		if surface == packMentionPackGroupMessage {
			body = strings.Replace(body, "вкладка «Стая».", "«Стая» → «Чат».", 1)
		}
		b.sendTrainingThreadCommentDM(uid, body)
	}
}

func (b *Bot) notifyPackMemberMentionsInFeedPost(packChatID, authorUserID int64, authorName, text string, userMessageID int64) {
	b.notifyPackMemberMentions(packChatID, authorUserID, authorName, text, packMentionFeedPost, userMessageID, nil)
}

func (b *Bot) notifyPackMemberMentionsInFeedComment(
	packChatID, authorUserID int64,
	authorName, text string,
	threadReplyID int64,
	skipUserIDs map[int64]struct{},
) {
	b.notifyPackMemberMentions(packChatID, authorUserID, authorName, text, packMentionFeedComment, threadReplyID, skipUserIDs)
}

func (b *Bot) notifyPackMemberMentionsInPackGroup(
	packChatID, authorUserID int64,
	authorName, text string,
	messageID int64,
	skipUserIDs map[int64]struct{},
) {
	b.notifyPackMemberMentions(packChatID, authorUserID, authorName, text, packMentionPackGroupMessage, messageID, skipUserIDs)
}
