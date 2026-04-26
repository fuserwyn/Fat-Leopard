package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"leo-bot/internal/database"
	"leo-bot/internal/yookassa"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const (
	outboxWorkerBatchSize = 20
	outboxWorkerPollEvery = 2 * time.Second
)

type refundRequestedPayload struct {
	RequestID int64  `json:"request_id"`
	UserID    int64  `json:"user_id"`
	Reason    string `json:"reason"`
}

type nonRetryableOutboxError struct {
	msg string
}

func (e nonRetryableOutboxError) Error() string { return e.msg }

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
			maxAttempts := outboxMaxAttemptsForType(event.EventType)
			if event.Attempts >= maxAttempts || isNonRetryableOutboxErr(err) {
				_ = b.db.MarkOutboxEventDead(event.ID, err.Error())
				if event.EventType == "paywall_access_restore_requested" {
					b.enqueueRefundRequestedForRestoreFailure(event, err)
				}
				b.notifyOps("OUTBOX DEAD event_id=%d type=%s attempts=%d err=%v", event.ID, event.EventType, event.Attempts, err)
				continue
			}
			next := time.Now().Add(outboxBackoffDuration(event.EventType, event.Attempts))
			if rerr := b.db.MarkOutboxEventRetry(event.ID, next, err.Error()); rerr != nil {
				b.logger.Errorf("outbox mark retry event_id=%d: %v", event.ID, rerr)
			}
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
			return nonRetryableOutboxError{msg: fmt.Sprintf("invalid payload user=%d chat=%d", payload.UserID, payload.ChatID)}
		}
		// Смоук: мгновенный dead без Telegram/БД deliver → enqueueRefundRequestedForRestoreFailure (payload с ненулевыми id).
		// aggregate_key должен начинаться с smoke:fail_restore — уведомление владельцу подавляет префикс smoke:.
		if strings.HasPrefix(strings.TrimSpace(event.AggregateKey), "smoke:fail_restore") {
			if payload.RequestID == 0 {
				return nonRetryableOutboxError{msg: "smoke:fail_restore requires non-zero request_id in payload"}
			}
			return nonRetryableOutboxError{msg: "smoke: simulated paywall deliver failure (aggregate smoke:fail_restore*)"}
		}
		if err := b.paywallDeliverAccessAfterPayment(payload.UserID); err != nil {
			return err
		}
		return nil
	case "refund_requested":
		var payload refundRequestedPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return fmt.Errorf("decode refund payload: %w", err)
		}
		if payload.RequestID == 0 || payload.UserID == 0 {
			return nonRetryableOutboxError{msg: fmt.Sprintf("invalid refund payload request=%d user=%d", payload.RequestID, payload.UserID)}
		}
		return b.processRefundRequested(payload)
	default:
		return fmt.Errorf("unknown outbox event type: %s", event.EventType)
	}
}

func outboxBackoffDuration(eventType string, attempt int) time.Duration {
	if attempt <= 1 {
		if eventType == "paywall_access_restore_requested" {
			return 5 * time.Second
		}
		return 10 * time.Second
	}
	d := 10 * time.Second
	if eventType == "paywall_access_restore_requested" {
		d = 5 * time.Second
	}
	for i := 1; i < attempt; i++ {
		d *= 3
		if d > 30*time.Minute {
			return 30 * time.Minute
		}
	}
	return d
}

func outboxMaxAttemptsForType(eventType string) int {
	switch eventType {
	case "paywall_access_restore_requested":
		return 4
	case "refund_requested":
		return 3
	default:
		return 8
	}
}

func isNonRetryableOutboxErr(err error) bool {
	if err == nil {
		return false
	}
	_, ok := err.(nonRetryableOutboxError)
	return ok
}

func (b *Bot) enqueueRefundRequestedForRestoreFailure(event database.OutboxEvent, cause error) {
	var restorePayload database.PaywallRestoreOutboxPayload
	if err := json.Unmarshal(event.Payload, &restorePayload); err != nil {
		b.notifyOps("OUTBOX refund enqueue failed: decode restore payload event_id=%d err=%v", event.ID, err)
		return
	}
	if restorePayload.RequestID == 0 || restorePayload.UserID == 0 {
		return
	}
	refundPayload := refundRequestedPayload{
		RequestID: restorePayload.RequestID,
		UserID:    restorePayload.UserID,
		Reason:    strings.TrimSpace(cause.Error()),
	}
	key := fmt.Sprintf("refund_request:%d", restorePayload.RequestID)
	if err := b.db.EnqueueOutboxEvent("refund_requested", key, refundPayload); err != nil {
		b.notifyOps("OUTBOX refund enqueue failed request=%d user=%d err=%v", restorePayload.RequestID, restorePayload.UserID, err)
	}
}

func (b *Bot) processRefundRequested(payload refundRequestedPayload) error {
	req, err := b.db.GetPaywallAccessRequestByID(payload.RequestID)
	if err != nil {
		return err
	}
	if req == nil {
		return nonRetryableOutboxError{msg: fmt.Sprintf("refund request not found id=%d", payload.RequestID)}
	}
	if req.UserID != payload.UserID {
		return nonRetryableOutboxError{msg: fmt.Sprintf("refund user mismatch req=%d payload_user=%d db_user=%d", payload.RequestID, payload.UserID, req.UserID)}
	}
	if strings.ToUpper(strings.TrimSpace(req.Currency.String)) == "XTR" {
		if !req.TelegramPaymentChargeID.Valid || strings.TrimSpace(req.TelegramPaymentChargeID.String) == "" {
			return nonRetryableOutboxError{msg: fmt.Sprintf("refund stars unsupported: empty telegram charge id req=%d", req.ID)}
		}
		if err := b.refundStarPayment(req.UserID, req.TelegramPaymentChargeID.String); err != nil {
			return err
		}
		b.paywallNotifyUser(req.UserID, "✅ Оплата возвращена. Попробуй ещё раз чуть позже.")
		return nil
	}
	if req.YookassaPaymentID.Valid && strings.TrimSpace(req.YookassaPaymentID.String) != "" {
		amount := int(req.TotalAmountMinor.Int64)
		if amount <= 0 {
			amount = b.config.YookassaAmountMinor
		}
		currency := strings.TrimSpace(req.Currency.String)
		if currency == "" {
			currency = b.config.YookassaCurrency
		}
		if currency == "" {
			currency = "RUB"
		}
		if err := b.refundYookassaPayment(req.YookassaPaymentID.String, amount, currency); err != nil {
			return err
		}
		b.paywallNotifyUser(req.UserID, "✅ Оплата возвращена. Попробуй ещё раз чуть позже.")
		return nil
	}
	return nonRetryableOutboxError{msg: fmt.Sprintf("refund unsupported: request=%d currency=%s requires manual provider refund", req.ID, req.Currency.String)}
}

func (b *Bot) refundStarPayment(userID int64, chargeID string) error {
	params := tgbotapi.Params{}
	params.AddNonZero64("user_id", userID)
	params.AddNonEmpty("telegram_payment_charge_id", strings.TrimSpace(chargeID))
	resp, err := b.api.MakeRequest("refundStarPayment", params)
	if err != nil {
		return fmt.Errorf("refundStarPayment request: %w", err)
	}
	if !resp.Ok {
		return fmt.Errorf("refundStarPayment api error code=%d desc=%s", resp.ErrorCode, resp.Description)
	}
	return nil
}

func (b *Bot) refundYookassaPayment(paymentID string, amountMinor int, currency string) error {
	if strings.TrimSpace(b.config.YookassaShopID) == "" || strings.TrimSpace(b.config.YookassaSecretKey) == "" {
		return nonRetryableOutboxError{msg: "refund yookassa unsupported: credentials empty"}
	}
	return yookassa.RefundPayment(
		b.config.YookassaShopID,
		b.config.YookassaSecretKey,
		strings.TrimSpace(paymentID),
		amountMinor,
		currency,
	)
}

func (b *Bot) notifyOps(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	b.logger.Errorf(msg)
	if b.config == nil || b.config.OwnerID == 0 {
		return
	}
	_, _ = b.api.Send(tgbotapi.NewMessage(b.config.OwnerID, "⚠️ "+msg))
}
