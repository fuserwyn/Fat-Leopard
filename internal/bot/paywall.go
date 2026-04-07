package bot

import (
	"fmt"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const paywallPayloadPrefix = "pw_"

// paywallActive — платный вход включён и задана целевая группа.
func (b *Bot) paywallActive() bool {
	return b.config.PaywallEnabled && b.config.MonetizedChatID != 0
}

func (b *Bot) paywallPrivateStartBanner() string {
	if !b.paywallActive() {
		return ""
	}
	return `💳 Платный чат
Подай заявку на вступление в группу по пригласительной ссылке. Бот пришлёт счёт в этот чат; после успешной оплаты заявка будет одобрена автоматически.

Нужны: бот — администратор группы с правом «одобрять заявки», в группе включены заявки на вступление, в @BotFather подключены платежи и задан PAYMENT_PROVIDER_TOKEN.`
}

func parsePaywallPayload(payload string) (requestID int64, ok bool) {
	payload = strings.TrimSpace(payload)
	if !strings.HasPrefix(payload, paywallPayloadPrefix) {
		return 0, false
	}
	id, err := strconv.ParseInt(payload[len(paywallPayloadPrefix):], 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func (b *Bot) handlePaywallPreCheckout(q *tgbotapi.PreCheckoutQuery) {
	if q.From == nil {
		_, _ = b.api.Request(tgbotapi.PreCheckoutConfig{PreCheckoutQueryID: q.ID, OK: false, ErrorMessage: "Оплата недоступна."})
		return
	}
	if !b.paywallActive() || b.config.PaymentProviderToken == "" {
		_, _ = b.api.Request(tgbotapi.PreCheckoutConfig{PreCheckoutQueryID: q.ID, OK: false, ErrorMessage: "Оплата недоступна."})
		return
	}
	if q.Currency != b.config.PaymentCurrency || q.TotalAmount != b.config.PaymentAmountMinorUnits {
		b.logger.Warnf("paywall pre_checkout amount mismatch: got %s %d want %s %d", q.Currency, q.TotalAmount, b.config.PaymentCurrency, b.config.PaymentAmountMinorUnits)
		_, _ = b.api.Request(tgbotapi.PreCheckoutConfig{PreCheckoutQueryID: q.ID, OK: false, ErrorMessage: "Неверная сумма. Обнови заявку и попробуй снова."})
		return
	}
	reqID, ok := parsePaywallPayload(q.InvoicePayload)
	if !ok {
		_, _ = b.api.Request(tgbotapi.PreCheckoutConfig{PreCheckoutQueryID: q.ID, OK: false, ErrorMessage: "Некорректный платёж."})
		return
	}
	rec, err := b.db.GetPaywallAccessRequestByID(reqID)
	if err != nil || rec == nil {
		b.logger.Errorf("paywall pre_checkout load request: %v", err)
		_, _ = b.api.Request(tgbotapi.PreCheckoutConfig{PreCheckoutQueryID: q.ID, OK: false, ErrorMessage: "Заявка не найдена. Подай заявку снова."})
		return
	}
	if rec.Status != "pending" {
		_, _ = b.api.Request(tgbotapi.PreCheckoutConfig{PreCheckoutQueryID: q.ID, OK: false, ErrorMessage: "Этот счёт уже неактуален."})
		return
	}
	if rec.UserID != q.From.ID || rec.MonetizedChatID != b.config.MonetizedChatID {
		_, _ = b.api.Request(tgbotapi.PreCheckoutConfig{PreCheckoutQueryID: q.ID, OK: false, ErrorMessage: "Платёж не для этого аккаунта."})
		return
	}
	_, _ = b.api.Request(tgbotapi.PreCheckoutConfig{PreCheckoutQueryID: q.ID, OK: true})
}

func (b *Bot) handlePaywallSuccessfulPayment(msg *tgbotapi.Message) {
	if !b.paywallActive() || msg.From == nil || msg.SuccessfulPayment == nil {
		return
	}
	sp := msg.SuccessfulPayment
	if sp.Currency != b.config.PaymentCurrency || sp.TotalAmount != b.config.PaymentAmountMinorUnits {
		b.logger.Errorf("paywall successful_payment mismatch: %s %d", sp.Currency, sp.TotalAmount)
		return
	}
	reqID, ok := parsePaywallPayload(sp.InvoicePayload)
	if !ok {
		return
	}
	rec, err := b.db.GetPaywallAccessRequestByID(reqID)
	if err != nil || rec == nil {
		b.logger.Errorf("paywall payment load request: %v", err)
		return
	}
	if rec.UserID != msg.From.ID || rec.MonetizedChatID != b.config.MonetizedChatID {
		b.logger.Warnf("paywall payment user/chat mismatch")
		return
	}
	okDb, err := b.db.CompletePaywallAccessRequest(reqID, msg.From.ID, b.config.MonetizedChatID, sp.TelegramPaymentChargeID, sp.TotalAmount, sp.Currency)
	if err != nil {
		b.logger.Errorf("paywall complete request: %v", err)
		return
	}
	if !okDb {
		b.logger.Infof("paywall request %d already completed or not pending", reqID)
	}

	_, err = b.api.Request(tgbotapi.ApproveChatJoinRequestConfig{
		ChatConfig: tgbotapi.ChatConfig{ChatID: b.config.MonetizedChatID},
		UserID:     msg.From.ID,
	})
	if err != nil {
		b.logger.Errorf("paywall approve join request failed: %v", err)
		pm := tgbotapi.NewMessage(msg.Chat.ID, "✅ Оплата прошла. Авто-одобрение заявки не вышло — напиши администратору, приложи этот чек.")
		b.api.Send(pm)
		return
	}
	done := tgbotapi.NewMessage(msg.Chat.ID, "✅ Оплата принята, заявка в группу одобрена. Добро пожаловать!")
	b.api.Send(done)
}

func (b *Bot) handlePaywallChatJoinRequest(j *tgbotapi.ChatJoinRequest) {
	if !b.paywallActive() {
		return
	}
	if j.Chat.ID != b.config.MonetizedChatID {
		return
	}
	if b.config.PaymentProviderToken == "" {
		b.logger.Error("PAYWALL_ENABLED but PAYMENT_PROVIDER_TOKEN is empty")
		return
	}
	userID := j.From.ID
	reqID, err := b.db.InsertPaywallAccessRequest(userID, j.Chat.ID)
	if err != nil {
		b.logger.Errorf("paywall insert request: %v", err)
		return
	}
	payload := fmt.Sprintf("%s%d", paywallPayloadPrefix, reqID)
	prices := []tgbotapi.LabeledPrice{{Label: "Доступ", Amount: b.config.PaymentAmountMinorUnits}}
	inv := tgbotapi.NewInvoice(
		userID,
		b.config.PaymentInvoiceTitle,
		b.config.PaymentInvoiceDesc,
		payload,
		b.config.PaymentProviderToken,
		"",
		b.config.PaymentCurrency,
		prices,
	)
	if _, err := b.api.Send(inv); err != nil {
		b.logger.Errorf("paywall send invoice: %v", err)
	}
}
