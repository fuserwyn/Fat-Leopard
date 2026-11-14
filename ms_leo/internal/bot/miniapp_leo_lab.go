package bot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"leo-bot/internal/ai"
	"leo-bot/internal/rag"

	initdata "github.com/telegram-mini-apps/init-data-golang"
)

// «Тест Лео» в админке: поговорить с тем же обученным Лео, попробовать другой
// системный промпт и подкинуть ему знание, которое он будет использовать
// дальше. Полноценного дообучения модели тут нет и быть не может — «обучение»
// у нас это две вещи: системный промпт (характер и правила) и память RAG
// (факты про стаю). Обе доступны отсюда.

// MiniappLeoLabAsk — ответ Лео на вопрос. Пустой prompt — боевой характер.
func (b *Bot) MiniappLeoLabAsk(
	viewerUserID int64, initD initdata.InitData, systemPrompt, question string,
) (answer string, usedDefault bool, err error) {
	if _, err := b.requireMiniappAdmin(viewerUserID, initD); err != nil {
		return "", false, err
	}
	q := strings.TrimSpace(question)
	if q == "" {
		return "", false, fmt.Errorf("спроси что-нибудь")
	}
	if b.aiClient == nil {
		return "", false, fmt.Errorf("Лео недоступен: не настроен OpenRouter")
	}
	sys := strings.TrimSpace(systemPrompt)
	if sys == "" {
		sys = strings.TrimSpace(b.livePrompts().AnswerUserQuestion)
		usedDefault = true
	}
	if len([]rune(sys)) > 6000 {
		sys = string([]rune(sys)[:6000])
	}
	if len([]rune(q)) > 2000 {
		q = string([]rune(q)[:2000])
	}
	answer, err = b.aiClient.Chat([]ai.ChatMessage{
		{Role: "system", Content: sys},
		{Role: "user", Content: q},
	}, "")
	if err != nil {
		return "", usedDefault, fmt.Errorf("Лео не ответил: %w", err)
	}
	return strings.TrimSpace(answer), usedDefault, nil
}

// MiniappLeoLabPrompt — боевой системный промпт Лео, чтобы было от чего плясать.
func (b *Bot) MiniappLeoLabPrompt(viewerUserID int64, initD initdata.InitData) (string, error) {
	if _, err := b.requireMiniappAdmin(viewerUserID, initD); err != nil {
		return "", err
	}
	return strings.TrimSpace(b.livePrompts().AnswerUserQuestion), nil
}

// MiniappLeoLabTeach — положить факт в память Лео (RAG общего чата стаи).
// Он подтянется в контекст там же, где Лео вспоминает переписку стаи.
func (b *Bot) MiniappLeoLabTeach(viewerUserID int64, initD initdata.InitData, text string) error {
	if _, err := b.requireMiniappAdmin(viewerUserID, initD); err != nil {
		return err
	}
	fact := strings.TrimSpace(text)
	if fact == "" {
		return fmt.Errorf("напиши, что Лео должен запомнить")
	}
	if len([]rune(fact)) > 2000 {
		fact = string([]rune(fact)[:2000])
	}
	if b.ragStore == nil || !b.ragStore.Enabled() {
		return fmt.Errorf("память выключена: не настроен Qdrant")
	}
	packChatID := b.config.MonetizedChatID
	if packChatID == 0 {
		return fmt.Errorf("не настроен MonetizedChatID")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// source_id = 0: у факта нет строки в Postgres, он живёт только в памяти.
	return b.ragStore.Index(ctx, rag.MessageDoc{
		SessionID:  rag.PackGroupSessionID(packChatID),
		Channel:    rag.ChannelPackGroup,
		UserID:     viewerUserID,
		PackChatID: packChatID,
		Role:       "leo",
		Text:       fact,
		CreatedAt:  time.Now().UTC(),
	})
}

// MiniappLeoLabMemory — что сейчас в памяти: всего точек и сколько старых.
func (b *Bot) MiniappLeoLabMemory(
	viewerUserID int64, initD initdata.InitData, days int,
) (rag.MemoryStats, error) {
	var out rag.MemoryStats
	if _, err := b.requireMiniappAdmin(viewerUserID, initD); err != nil {
		return out, err
	}
	if b.ragStore == nil || !b.ragStore.Enabled() {
		return out, nil
	}
	if days <= 0 {
		days = 180
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return b.ragStore.Stats(ctx, time.Now().AddDate(0, 0, -days))
}
