package bot

import (
	"fmt"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const paywallPayloadPrefix = "pw_"

const paywallCallbackResendInvoice = "paywall_resend_invoice"

// paywallActive — платный вход включён и задана целевая группа.
func (b *Bot) paywallActive() bool {
	return b.config.PaywallEnabled && b.config.MonetizedChatID != 0
}

// paywallPrivateUnpaidUserText — только оплата и шаги (без полной справки бота).
func (b *Bot) paywallPrivateUnpaidUserText() string {
	priceRub := ""
	if b.config.PaymentCurrency == "RUB" && b.config.PaymentAmountMinorUnits > 0 {
		rub := b.config.PaymentAmountMinorUnits / 100
		kop := b.config.PaymentAmountMinorUnits % 100
		if kop == 0 {
			priceRub = fmt.Sprintf(" Сумма: %d ₽.", rub)
		} else {
			priceRub = fmt.Sprintf(" Сумма: %d,%02d ₽.", rub, kop)
		}
	} else if b.config.PaymentAmountMinorUnits > 0 && b.config.PaymentCurrency != "" {
		priceRub = fmt.Sprintf(" В счёте будет указано: %d мин. ед. валюты %s.", b.config.PaymentAmountMinorUnits, b.config.PaymentCurrency)
	}
	return `💳 Платный вход в закрытую группу

Сразу после этого сообщения придёт счёт с кнопкой «Оплатить» в Telegram.

Порядок такой:
1. Оплати счёт здесь (можно до заявки в группу).
2. Затем нажми кнопку ниже «Подать заявку» и подтверди вступление — если оплата уже прошла, заявку одобрю автоматически.

Полное приветствие с командами бота пришлю после успешной оплаты.` + priceRub + `

Пока оплаты не было, длинную справку в личке не показываю — она пригодится в группе.

👇 Кнопки: ссылка в группу и повторная отправка счёта (если сообщение со счётом потерялось).`
}

// paywallUnpaidInlineKeyboard — ссылка на группу + повтор счёта (invoice сам по себе содержит кнопку «Оплатить»).
func (b *Bot) paywallUnpaidInlineKeyboard() *tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton
	if u := strings.TrimSpace(b.config.MonetizedChatInviteURL); u != "" {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL("📥 Подать заявку в группу", u),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("💳 Выслать счёт снова", paywallCallbackResendInvoice),
	))
	return &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// ensurePaywallInvoiceSent создаёт pending-заявку при необходимости и шлёт invoice (кнопка «Оплатить»).
func (b *Bot) ensurePaywallInvoiceSent(userID int64) {
	if !b.paywallActive() || userID == 0 || b.config.PaymentProviderToken == "" {
		return
	}
	pending, err := b.db.GetLatestPendingPaywallAccessRequest(userID, b.config.MonetizedChatID)
	if err != nil {
		b.logger.Errorf("paywall ensure invoice get pending: %v", err)
		return
	}
	var reqID int64
	if pending != nil {
		reqID = pending.ID
	} else {
		reqID, err = b.db.InsertPaywallAccessRequest(userID, b.config.MonetizedChatID)
		if err != nil {
			b.logger.Errorf("paywall ensure invoice insert: %v", err)
			return
		}
	}
	if err := b.SendPaywallInvoice(userID, reqID); err != nil {
		b.logger.Errorf("paywall ensure invoice send: %v", err)
	}
}

// SendPaywallInvoice отправляет invoice в ЛС; payload pw_<reqID> должен совпадать с записью в БД.
func (b *Bot) SendPaywallInvoice(userID, reqID int64) error {
	if b.config.PaymentProviderToken == "" {
		return fmt.Errorf("payment provider token empty")
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
	_, err := b.api.Send(inv)
	return err
}

func (b *Bot) handlePaywallResendInvoiceCallback(callback *tgbotapi.CallbackQuery) {
	if callback.From == nil {
		_, _ = b.api.Request(tgbotapi.NewCallback(callback.ID, ""))
		return
	}
	if !b.paywallActive() || b.config.PaymentProviderToken == "" {
		_, _ = b.api.Request(tgbotapi.NewCallbackWithAlert(callback.ID, "Оплата сейчас недоступна. Напиши администратору."))
		return
	}

	rec, err := b.db.GetLatestPendingPaywallAccessRequest(callback.From.ID, b.config.MonetizedChatID)
	if err != nil {
		b.logger.Errorf("paywall get pending: %v", err)
		_, _ = b.api.Request(tgbotapi.NewCallbackWithAlert(callback.ID, "Ошибка. Попробуй позже или напиши администратору."))
		return
	}
	reqID := int64(0)
	if rec != nil {
		reqID = rec.ID
	} else {
		reqID, err = b.db.InsertPaywallAccessRequest(callback.From.ID, b.config.MonetizedChatID)
		if err != nil {
			b.logger.Errorf("paywall resend insert: %v", err)
			_, _ = b.api.Request(tgbotapi.NewCallbackWithAlert(callback.ID, "Не удалось выставить счёт. Попробуй /start."))
			return
		}
	}

	if err := b.SendPaywallInvoice(callback.From.ID, reqID); err != nil {
		b.logger.Errorf("paywall resend invoice: %v", err)
		_, _ = b.api.Request(tgbotapi.NewCallbackWithAlert(callback.ID, "Не удалось отправить счёт. Проверь, что бот не в чёрном списке."))
		return
	}

	_, _ = b.api.Request(tgbotapi.NewCallback(callback.ID, "Счёт отправлен — в чате со мной открой сообщение со счётом и нажми «Оплатить»."))
}

func (b *Bot) paywallPrivatePaidFooter() string {
	if !b.paywallActive() {
		return ""
	}
	return `

💳 Доступ к платной группе оплачен. Если вышел(а) из группы и нужен снова вход — подай заявку по ссылке, пришлю новый счёт.`
}

// userIsActiveMemberOfMonetizedChat — пользователь уже в целевой группе (не нужно гонять в paywall по /start в личке).
func (b *Bot) userIsActiveMemberOfMonetizedChat(userID int64) bool {
	if !b.paywallActive() || userID == 0 {
		return false
	}
	member, err := b.api.GetChatMember(tgbotapi.GetChatMemberConfig{
		ChatConfigWithUser: tgbotapi.ChatConfigWithUser{
			ChatID: b.config.MonetizedChatID,
			UserID: userID,
		},
	})
	if err != nil {
		b.logger.Warnf("paywall getChatMember user=%d chat=%d: %v", userID, b.config.MonetizedChatID, err)
		return false
	}
	if member.HasLeft() || member.WasKicked() {
		return false
	}
	switch member.Status {
	case "creator", "administrator", "member":
		return true
	case "restricted":
		return member.IsMember
	default:
		return false
	}
}

// paywallPrivateNeedsPayFirst — личка, paywall включён, не владелец, нет завершённой оплаты по MONETIZED_CHAT_ID.
func (b *Bot) paywallPrivateNeedsPayFirst(userID int64) bool {
	if !b.paywallActive() || userID == 0 {
		return false
	}
	if b.config.OwnerID != 0 && userID == b.config.OwnerID {
		return false
	}
	if b.userIsActiveMemberOfMonetizedChat(userID) {
		return false
	}
	ok, err := b.db.UserHasCompletedPaywallAccess(userID, b.config.MonetizedChatID)
	if err != nil {
		b.logger.Errorf("paywall access check: %v", err)
		return true
	}
	return !ok
}

func (b *Bot) monetizedChatWelcomeType() string {
	t, err := b.db.GetChatType(b.config.MonetizedChatID)
	if err != nil {
		return "training"
	}
	return t
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
		_, _ = b.api.Request(tgbotapi.PreCheckoutConfig{PreCheckoutQueryID: q.ID, OK: false, ErrorMessage: "Заявка не найдена. Нажми /start снова."})
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
		follow := "✅ Оплата принята.\n\nЕсли ты ещё не подал(а) заявку в группу — открой пригласительную ссылку и нажми «Запросить вступление». Заявка одобрится автоматически."
		if u := strings.TrimSpace(b.config.MonetizedChatInviteURL); u != "" {
			follow += "\n\nСсылка: " + u
		} else {
			follow += "\n\nПопроси ссылку у администратора."
		}
		pm := tgbotapi.NewMessage(msg.Chat.ID, follow)
		b.api.Send(pm)
		welcome := welcomeStartText(b.monetizedChatWelcomeType())
		wmsg := tgbotapi.NewMessage(msg.Chat.ID, welcome)
		b.api.Send(wmsg)
		return
	}
	done := tgbotapi.NewMessage(msg.Chat.ID, "✅ Оплата принята, заявка в группу одобрена. Добро пожаловать!")
	if _, err := b.api.Send(done); err != nil {
		b.logger.Errorf("paywall send done msg: %v", err)
	}
	welcome := welcomeStartText(b.monetizedChatWelcomeType())
	wmsg := tgbotapi.NewMessage(msg.Chat.ID, welcome)
	if _, err := b.api.Send(wmsg); err != nil {
		b.logger.Errorf("paywall send welcome after payment: %v", err)
	}
}

func (b *Bot) handlePaywallChatJoinRequest(j *tgbotapi.ChatJoinRequest) {
	if !b.paywallActive() {
		return
	}
	if j.Chat.ID != b.config.MonetizedChatID {
		return
	}
	userID := j.From.ID
	if userID == 0 {
		return
	}

	paid, err := b.db.UserHasCompletedPaywallAccess(userID, b.config.MonetizedChatID)
	if err != nil {
		b.logger.Errorf("paywall join request paid check: %v", err)
		return
	}
	if paid {
		_, err := b.api.Request(tgbotapi.ApproveChatJoinRequestConfig{
			ChatConfig: tgbotapi.ChatConfig{ChatID: b.config.MonetizedChatID},
			UserID:     userID,
		})
		if err != nil {
			b.logger.Errorf("paywall approve (already paid): %v", err)
		}
		return
	}

	if b.config.PaymentProviderToken == "" {
		b.logger.Error("PAYWALL_ENABLED but PAYMENT_PROVIDER_TOKEN is empty")
		return
	}

	pending, err := b.db.GetLatestPendingPaywallAccessRequest(userID, b.config.MonetizedChatID)
	if err != nil {
		b.logger.Errorf("paywall join request pending: %v", err)
		return
	}
	var reqID int64
	if pending != nil {
		reqID = pending.ID
	} else {
		reqID, err = b.db.InsertPaywallAccessRequest(userID, j.Chat.ID)
		if err != nil {
			b.logger.Errorf("paywall insert request: %v", err)
			return
		}
	}
	if err := b.SendPaywallInvoice(userID, reqID); err != nil {
		b.logger.Errorf("paywall send invoice: %v", err)
	}
}
