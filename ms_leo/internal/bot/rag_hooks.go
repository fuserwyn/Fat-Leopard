package bot

import (
	"context"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"leo-bot/internal/rag"
)

// aiContextChannel — личка с Лео и общий чат стаи не смешивают RAG-контекст.
func (b *Bot) aiContextChannel(msg *tgbotapi.Message, skipTelegram, compactContext bool) rag.Channel {
	if msg == nil || msg.Chat == nil {
		return rag.ChannelPersonalLeo
	}
	if skipTelegram && compactContext {
		switch msg.Chat.Type {
		case "group", "supergroup":
			return rag.ChannelPackGroup
		}
	}
	return rag.ChannelPersonalLeo
}

func (b *Bot) ragSessionID(channel rag.Channel, userID, packChatID int64) string {
	switch channel {
	case rag.ChannelPackGroup:
		return rag.PackGroupSessionID(packChatID)
	default:
		return rag.PersonalSessionID(userID, packChatID)
	}
}

func (b *Bot) indexRAGMessage(doc rag.MessageDoc) {
	if b == nil || b.ragStore == nil || !b.ragStore.Enabled() {
		return
	}
	go func(d rag.MessageDoc) {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		if err := b.ragStore.Index(ctx, d); err != nil && b.logger != nil {
			b.logger.Warnf("rag index session=%s: %v", d.SessionID, err)
		}
	}(doc)
}

func (b *Bot) indexPersonalChatRAG(userID int64, role, text string, sourceID int64) {
	if b == nil || b.config == nil || b.config.MonetizedChatID == 0 {
		return
	}
	b.indexRAGMessage(rag.MessageDoc{
		SessionID:  rag.PersonalSessionID(userID, b.config.MonetizedChatID),
		Channel:    rag.ChannelPersonalLeo,
		UserID:     userID,
		PackChatID: b.config.MonetizedChatID,
		Role:       role,
		Text:       text,
		CreatedAt:  time.Now().UTC(),
		SourceID:   sourceID,
	})
}

func (b *Bot) indexPackGroupChatRAG(packChatID, userID int64, role, text string, sourceID int64) {
	if packChatID == 0 {
		return
	}
	b.indexRAGMessage(rag.MessageDoc{
		SessionID:  rag.PackGroupSessionID(packChatID),
		Channel:    rag.ChannelPackGroup,
		UserID:     userID,
		PackChatID: packChatID,
		Role:       role,
		Text:       text,
		CreatedAt:  time.Now().UTC(),
		SourceID:   sourceID,
	})
}

func (b *Bot) appendRAGContext(ctx context.Context, channel rag.Channel, userID, packChatID int64, question string, contextText *strings.Builder) {
	if b == nil || b.ragStore == nil || !b.ragStore.Enabled() {
		return
	}
	sessionID := b.ragSessionID(channel, userID, packChatID)
	chunks, err := b.ragStore.Retrieve(ctx, sessionID, question, 10)
	if err != nil {
		if b.logger != nil {
			b.logger.Warnf("rag retrieve session=%s: %v", sessionID, err)
		}
		return
	}
	title := "RAG: личная переписка с Лео"
	if channel == rag.ChannelPackGroup {
		title = "RAG: общий чат стаи"
	}
	contextText.WriteString(rag.FormatChunksForPrompt(title, chunks))
}

func (b *Bot) appendPackGroupSQLContext(packChatID int64, contextText *strings.Builder, limit int) {
	if b == nil || b.db == nil || packChatID == 0 {
		return
	}
	if limit <= 0 {
		limit = 12
	}
	msgs, err := b.db.ListMiniappPackGroupChat(packChatID, limit, nil)
	if err != nil || len(msgs) == 0 {
		return
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m == nil {
			continue
		}
		who := strings.TrimSpace(m.Username)
		if m.IsLeo || who == "Лео" || strings.EqualFold(who, "Leo") {
			who = "Leo"
		}
		if who == "" {
			who = "Member"
		}
		if !strings.HasPrefix(who, "@") && who != "Leo" && who != "Лео" {
			who = "@" + who
		}
		txt := strings.TrimSpace(m.Text)
		if len(txt) > 400 {
			txt = txt[:400] + "…"
		}
		// Фото-вложение помечаем явно, чтобы Лео знал о картинке даже без текста.
		if strings.TrimSpace(m.PhotoURL) != "" {
			if txt == "" {
				txt = "[photo]"
			} else {
				txt += " [photo]"
			}
		}
		if txt == "" {
			continue
		}
		// Связь реплая: на чьё сообщение отвечают.
		replyMark := ""
		if m.ReplyToID != 0 {
			rwho := strings.TrimSpace(m.ReplyToUsername)
			if m.ReplyToIsLeo || rwho == "Лео" || strings.EqualFold(rwho, "Leo") {
				rwho = "Leo"
			}
			if rwho != "" {
				if rwho != "Leo" && rwho != "Лео" && !strings.HasPrefix(rwho, "@") {
					rwho = "@" + rwho
				}
				replyMark = " (replying to " + rwho + ")"
			}
		}
		ts := strings.TrimSpace(m.CreatedAt)
		if ts == "" {
			ts = "—"
		}
		contextText.WriteString("• [" + ts + "] " + who + replyMark + ": " + txt + "\n")
	}
}

// forgetRAGSource — сообщение удалили или скрыли: убрать его из памяти Лео,
// иначе он продолжит цитировать то, чего в ленте уже нет.
//
// Пока не вызывается: чистку памяти включаем отдельным решением, чтобы ничего
// не стиралось молча. Точка входа готова — останется повесить на удаление и
// скрытие сообщений.
//nolint:unused
func (b *Bot) forgetRAGSource(sourceID int64) {
	if b == nil || b.ragStore == nil || !b.ragStore.Enabled() || sourceID == 0 {
		return
	}
	go func(id int64) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := b.ragStore.DeleteBySource(ctx, id); err != nil && b.logger != nil {
			b.logger.Warnf("rag forget source=%d: %v", id, err)
		}
	}(sourceID)
}

// forgetRAGUser — человек ушёл из стаи: его реплики Лео помнить не должен.
// Тоже пока не вызывается — см. комментарий выше.
//nolint:unused
func (b *Bot) forgetRAGUser(userID int64) {
	if b == nil || b.ragStore == nil || !b.ragStore.Enabled() || userID == 0 {
		return
	}
	go func(id int64) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := b.ragStore.DeleteByUser(ctx, id); err != nil && b.logger != nil {
			b.logger.Warnf("rag forget user=%d: %v", id, err)
		}
	}(userID)
}
