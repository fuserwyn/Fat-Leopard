package bot

import (
	"fmt"
	"strings"
	"sync"
	"unicode"

	"leo-bot/internal/ai"
)

var leoTranslateCache sync.Map // text hash/key → english

func leoTextNeedsEnglishTranslate(text string) bool {
	for _, r := range text {
		if unicode.In(r, unicode.Cyrillic) {
			return true
		}
	}
	return false
}

// TranslateLeoTextToEnglish — перевод текста Лео на английский (кэш + wisdom DB + LLM).
func (b *Bot) TranslateLeoTextToEnglish(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("empty text")
	}
	if !leoTextNeedsEnglishTranslate(text) {
		return text, nil
	}
	if cached, ok := leoTranslateCache.Load(text); ok {
		if s, ok := cached.(string); ok && strings.TrimSpace(s) != "" {
			return s, nil
		}
	}
	if b != nil && b.db != nil {
		en, _, ok, err := b.db.GetDailyWisdomMatchingStored(text)
		if err == nil && ok && strings.TrimSpace(en) != "" && en != text {
			leoTranslateCache.Store(text, en)
			return en, nil
		}
	}
	if b == nil || b.aiClient == nil {
		return "", fmt.Errorf("ai unavailable")
	}
	messages := []ai.ChatMessage{
		{
			Role: "system",
			Content: strings.TrimSpace(`You translate Leo the leopard coach messages into natural English.
Keep the tone: friendly predator, short, first person masculine.
Preserve emoji and hashtags (#sick_leave, #healthy) unchanged.
Output ONLY the English translation, no quotes or preface.`),
		},
		{Role: "user", Content: text},
	}
	out, err := b.aiClient.Chat(messages, "")
	if err != nil {
		return "", err
	}
	out = ai.SanitizeTextForUser(out)
	out = strings.Trim(out, `"'«»“”„`)
	if out == "" {
		return "", fmt.Errorf("empty translation")
	}
	leoTranslateCache.Store(text, out)
	return out, nil
}
