package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"leo-bot/internal/database"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const (
	outboxWorkerBatchSize  = 20
	outboxWorkerPollEvery  = 2 * time.Second
	outboxWorkerMaxAttempts = 8
)

func (b *Bot) startOutboxWorker(ctx context.Context) {
	ticker := time.NewTicker(outboxWorkerPollEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.processOutboxBatch()
		}
	}
}

func (b *Bot) processOutboxBatch() {
	events, err := b.db.ClaimOutboxEvents(outboxWorkerBatchSize)
	if err != nil {
		b.logger.Errorf("outbox claim events: %v", err)
		return
	}
	for _, event := range events {
		if err := b.processOutboxEvent(event); err != nil {
			if event.Attempts >= outboxWorkerMaxAttempts {
				_ = b.db.MarkOutboxEventDead(event.ID, err.Error())
				b.notifyOps("OUTBOX DEAD event_id=%d type=%s attempts=%d err=%v", event.ID, event.EventType, event.Attempts, err)
				continue
			}
			next := time.Now().Add(outboxBackoffDuration(event.Attempts))
			_ = b.db.MarkOutboxEventRetry(event.ID, next, err.Error())
			continue
		}
		_ = b.db.MarkOutboxEventDone(event.ID)
	}
}

func (b *Bot) processOutboxEvent(event database.OutboxEvent) error {
	switch event.EventType {
	case "paywall_access_restore_requested":
		var payload database.PaywallRestoreOutboxPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return fmt.Errorf("decode payload: %w", err)
		}
		if payload.UserID == 0 || payload.ChatID == 0 {
			return fmt.Errorf("invalid payload user=%d chat=%d", payload.UserID, payload.ChatID)
		}
		if err := b.paywallDeliverAccessAfterPayment(payload.UserID); err != nil {
			return err
		}
		return nil
	default:
		return fmt.Errorf("unknown outbox event type: %s", event.EventType)
	}
}

func outboxBackoffDuration(attempt int) time.Duration {
	if attempt <= 1 {
		return 10 * time.Second
	}
	// 10s, 30s, 90s, 270s, capped.
	d := 10 * time.Second
	for i := 1; i < attempt; i++ {
		d *= 3
		if d > 30*time.Minute {
			return 30 * time.Minute
		}
	}
	return d
}

func (b *Bot) notifyOps(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	b.logger.Errorf(msg)
	if b.config == nil || b.config.OwnerID == 0 {
		return
	}
	_, _ = b.api.Send(tgbotapi.NewMessage(b.config.OwnerID, "⚠️ "+msg))
}
