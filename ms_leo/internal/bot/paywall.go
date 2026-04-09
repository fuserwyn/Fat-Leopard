package bot

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"leo-bot/internal/yookassa"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const paywallPayloadPrefix = "pw_"

const paywallCallbackResendInvoice = "paywall_resend_invoice"

const paywallInviteCacheTTL = 25 * time.Minute

// paywallCreateInviteLink вызывает Telegram API; бот должен быть админом с правом приглашений.
func (b *Bot) paywallCreateInviteLink(createsJoinRequest bool) (string, error) {
	cfg := tgbotapi.CreateChatInviteLinkConfig{
		ChatConfig:         tgbotapi.ChatConfig{ChatID: b.config.MonetizedChatID},
		CreatesJoinRequest: createsJoinRequest,
	}
	resp, err := b.api.Request(cfg)
	if err != nil {
		return "", err
	}
	var link tgbotapi.ChatInviteLink
	if err := json.Unmarshal(resp.Result, &link); err != nil {
		return "", err
	}
	if link.InviteLink == "" {
		return "", fmt.Errorf("empty invite_link in API response")
	}
	return link.InviteLink, nil
}

// paywallGroupInviteURL — актуальная ссылка в группу: свежая через API (кэш) или MONETIZED_CHAT_INVITE_URL.
// Статические t.me/+... часто протухают; API даёт новую дополнительную ссылку.
func (b *Bot) paywallGroupInviteURL() string {
	if !b.paywallActive() || b.config.MonetizedChatID == 0 {
		return ""
	}
	b.paywallInviteMu.Lock()
	defer b.paywallInviteMu.Unlock()

	if b.paywallInviteFromAPI && b.paywallInviteURL != "" && time.Since(b.paywallInviteCached) < paywallInviteCacheTTL {
		return b.paywallInviteURL
	}
	b.paywallInviteFromAPI = false
	b.paywallInviteURL = ""

	primary := b.config.PaywallInviteCreatesJoinRequest
	for _, createsJR := range []bool{primary, !primary} {
		u, err := b.paywallCreateInviteLink(createsJR)
		if err == nil && u != "" {
			b.paywallInviteURL = u
			b.paywallInviteCached = time.Now()
			b.paywallInviteFromAPI = true
			return u
		}
		b.logger.Warnf("paywall createChatInviteLink (creates_join_request=%v): %v", createsJR, err)
	}
	if u := strings.TrimSpace(b.config.MonetizedChatInviteURL); u != "" {
		return u
	}
	return ""
}

// paywallActive — платный вход включён и задана целевая группа.
func (b *Bot) paywallActive() bool {
	return b.config.PaywallEnabled && b.config.MonetizedChatID != 0
}

// paywallPrivateUnpaidUserText — только оплата и шаги (без полной справки бота).
// Счёт/ссылка должны уйти отдельным сообщением *до* этого текста (см. handleStart / help).
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

	if !b.config.PaywallPaymentReady() {
		return `💳 Платный вход в закрытую группу

⚠️ **Оплата не подключена** (нет ни PAYMENT_PROVIDER_TOKEN, ни пары YOOKASSA_SHOP_ID + YOOKASSA_SECRET_KEY). Напиши администратору — без этого счёт и ссылка не появятся.

Когда настроят:
• в Telegram придёт счёт с кнопкой «Оплатить», **или**
• придёт ссылка на оплату ЮKassa.

Порядок после настройки:
1. Оплати.
2. Нажми «Подать заявку в группу» ниже — после оплаты заявку обычно одобряю автоматически.` + priceRub + `

Доступ после оплаты — 30 дней. Полную справку бота пришлю, когда оплата пройдёт.

👇 Кнопки: заявка в группу и повтор попытки отправки счёта (когда оплата будет включена).`
	}

	payLine := `📩 **Предыдущее сообщение** в этом чате — счёт в Telegram с кнопкой «Оплатить». Не видишь — нажми «Выслать счёт снова» ниже.

Порядок:
1. Оплати счёт (можно до заявки в группу).`
	if !b.config.PaywallUsesTelegramInvoice() {
		payLine = `📩 **Предыдущее сообщение** — ссылка на оплату картой (ЮKassa). Не видишь — нажми кнопку повторной отправки ниже.

Порядок:
1. Перейди по ссылке и оплати (можно до заявки в группу).`
	}

	return `💳 Платный вход в закрытую группу

` + payLine + `
2. Затем нажми кнопку ниже «Подать заявку в группу» и подтверди вступление — если оплата уже прошла, заявку одобрю автоматически.

Полное приветствие с командами бота пришлю после успешной оплаты. Доступ действует 30 дней.` + priceRub + `

Пока оплаты не было, длинную справку в личке не показываю — она пригодится в группе.

👇 Кнопки: ссылка в группу и повторная отправка счёта / ссылки.`
}

// paywallUnpaidInlineKeyboard — ссылка на группу + повтор счёта (invoice сам по себе содержит кнопку «Оплатить»).
func (b *Bot) paywallUnpaidInlineKeyboard() *tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton
	if u := b.paywallGroupInviteURL(); u != "" {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL("📥 Подать заявку в группу", u),
		))
	}
	resendLabel := "💳 Выслать счёт снова"
	if !b.config.PaywallUsesTelegramInvoice() {
		resendLabel = "💳 Ссылка на оплату снова"
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData(resendLabel, paywallCallbackResendInvoice),
	))
	return &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func (b *Bot) paywallNotifyUser(userID int64, text string) {
	if userID == 0 {
		return
	}
	if _, err := b.api.Send(tgbotapi.NewMessage(userID, text)); err != nil {
		b.logger.Errorf("paywall user notify: %v", err)
	}
}

// ensurePaywallInvoiceSent создаёт pending-заявку при необходимости и шлёт счёт Telegram или ссылку ЮKassa.
// Вызывай до текста «Предыдущее сообщение — счёт», чтобы порядок в чате совпадал с подсказкой.
func (b *Bot) ensurePaywallInvoiceSent(userID int64) {
	if !b.paywallActive() || userID == 0 {
		return
	}
	if !b.config.PaywallPaymentReady() {
		// Текст с инструкцией придёт следующим сообщением (paywallPrivateUnpaidUserText).
		return
	}
	pending, err := b.db.GetLatestPendingPaywallAccessRequest(userID, b.config.MonetizedChatID)
	if err != nil {
		b.logger.Errorf("paywall ensure invoice get pending: %v", err)
		b.paywallNotifyUser(userID, "⚠️ Ошибка доступа к базе. Попробуй /start снова чуть позже или напиши администратору.")
		return
	}
	var reqID int64
	if pending != nil {
		reqID = pending.ID
	} else {
		reqID, err = b.db.InsertPaywallAccessRequest(userID, b.config.MonetizedChatID)
		if err != nil {
			b.logger.Errorf("paywall ensure invoice insert: %v", err)
			b.paywallNotifyUser(userID, "⚠️ Не удалось создать заявку на оплату. Попробуй /start снова.")
			return
		}
	}
	if b.config.PaywallUsesTelegramInvoice() {
		if err := b.SendPaywallInvoice(userID, reqID); err != nil {
			b.logger.Errorf("paywall ensure invoice send: %v", err)
			b.paywallNotifyUser(userID,
				"⚠️ Не удалось отправить счёт. Проверь, что бот не в чёрном списке, и нажми «Выслать счёт снова» под предыдущим сообщением или в инструкции ниже.")
		}
		return
	}
	if err := b.SendYookassaPaymentLink(userID, reqID); err != nil {
		b.logger.Errorf("paywall ensure yookassa link: %v", err)
		b.paywallNotifyUser(userID,
			"⚠️ Не удалось создать ссылку ЮKassa. Проверь настройки у администратора или нажми кнопку повторной отправки ниже.")
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

// SendYookassaPaymentLink создаёт платёж в ЮKassa и отправляет пользователю кнопку со ссылкой на оплату.
func (b *Bot) SendYookassaPaymentLink(userID, reqID int64) error {
	if b.config.YookassaShopID == "" || b.config.YookassaSecretKey == "" {
		return fmt.Errorf("yookassa credentials empty")
	}
	returnURL := strings.TrimSpace(b.config.YookassaReturnURL)
	if returnURL == "" {
		returnURL = "https://t.me"
	}
	meta := map[string]string{
		"user_telegram_id": fmt.Sprintf("%d", userID),
		"invoice_payload":  fmt.Sprintf("%s%d", paywallPayloadPrefix, reqID),
	}
	_, confirmURL, err := yookassa.CreatePayment(
		b.config.YookassaShopID,
		b.config.YookassaSecretKey,
		b.config.PaymentAmountMinorUnits,
		b.config.PaymentCurrency,
		b.config.PaymentInvoiceDesc,
		returnURL,
		meta,
	)
	if err != nil {
		return err
	}
	text := `💳 Оплата доступа картой (ЮKassa).

Нажми кнопку «Оплатить», заверши оплату на сайте. После успешного списания доступ откроется автоматически (до 1–2 минут) — затем подай заявку в группу или открой приглашение ещё раз.`
	msg := tgbotapi.NewMessage(userID, text)
	msg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{
		InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonURL("Оплатить", confirmURL)),
		},
	}
	_, err = b.api.Send(msg)
	return err
}

func (b *Bot) handlePaywallResendInvoiceCallback(callback *tgbotapi.CallbackQuery) {
	if callback.From == nil {
		_, _ = b.api.Request(tgbotapi.NewCallback(callback.ID, ""))
		return
	}
	if !b.paywallActive() || !b.config.PaywallPaymentReady() {
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

	if b.config.PaywallUsesTelegramInvoice() {
		if err := b.SendPaywallInvoice(callback.From.ID, reqID); err != nil {
			b.logger.Errorf("paywall resend invoice: %v", err)
			_, _ = b.api.Request(tgbotapi.NewCallbackWithAlert(callback.ID, "Не удалось отправить счёт. Проверь, что бот не в чёрном списке."))
			return
		}
		_, _ = b.api.Request(tgbotapi.NewCallback(callback.ID, "Счёт отправлен — открой сообщение со счётом и нажми «Оплатить»."))
		return
	}
	if err := b.SendYookassaPaymentLink(callback.From.ID, reqID); err != nil {
		b.logger.Errorf("paywall resend yookassa: %v", err)
		_, _ = b.api.Request(tgbotapi.NewCallbackWithAlert(callback.ID, "Не удалось создать оплату. Попробуй позже или напиши администратору."))
		return
	}
	_, _ = b.api.Request(tgbotapi.NewCallback(callback.ID, "Ссылка на оплату отправлена — открой новое сообщение и нажми «Оплатить»."))
}

func (b *Bot) paywallPrivatePaidFooter() string {
	if !b.paywallActive() {
		return ""
	}
	return `

💳 Доступ к платной группе оплачен. Если вышел(а) из группы и нужен снова вход — подай заявку по ссылке, пришлю новый счёт.`
}

// paywallPrivateNeedsPayFirst — личка, paywall включён, не владелец, нет активной (не истёкшей) оплаты.
func (b *Bot) paywallPrivateNeedsPayFirst(userID int64) bool {
	if !b.paywallActive() || userID == 0 {
		return false
	}
	if b.config.OwnerID != 0 && userID == b.config.OwnerID {
		return false
	}
	ok, err := b.db.UserHasActivePaywallAccess(userID, b.config.MonetizedChatID)
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
		follow := "✅ Оплата принята, доступ открыт на 30 дней.\n\nЕсли ты ещё не подал(а) заявку в группу — открой пригласительную ссылку и нажми «Запросить вступление». Заявка одобрится автоматически."
		if u := b.paywallGroupInviteURL(); u != "" {
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
	done := tgbotapi.NewMessage(msg.Chat.ID, "✅ Оплата принята, доступ к группе открыт на 30 дней, заявка одобрена. Добро пожаловать!")
	if _, err := b.api.Send(done); err != nil {
		b.logger.Errorf("paywall send done msg: %v", err)
	}
	welcome := welcomeStartText(b.monetizedChatWelcomeType())
	wmsg := tgbotapi.NewMessage(msg.Chat.ID, welcome)
	if _, err := b.api.Send(wmsg); err != nil {
		b.logger.Errorf("paywall send welcome after payment: %v", err)
	}
}

// paywallShouldKickDirectJoinWithoutPayment — человек уже в группе (добавили вручную / публичный вход без заявки), а оплаты в БД нет.
func (b *Bot) paywallShouldKickDirectJoinWithoutPayment(chatID, userID int64) bool {
	if !b.paywallActive() || chatID != b.config.MonetizedChatID || userID == 0 {
		return false
	}
	if b.config.OwnerID != 0 && userID == b.config.OwnerID {
		return false
	}
	member, err := b.api.GetChatMember(tgbotapi.GetChatMemberConfig{
		ChatConfigWithUser: tgbotapi.ChatConfigWithUser{ChatID: chatID, UserID: userID},
	})
	if err == nil && (member.IsCreator() || member.IsAdministrator()) {
		return false
	}
	paid, err := b.db.UserHasActivePaywallAccess(userID, chatID)
	if err != nil {
		b.logger.Errorf("paywall direct join paid check: %v", err)
		return false
	}
	if paid {
		return false
	}
	return true
}

func (b *Bot) paywallKickFromMonetizedChatAndExplain(userID int64) {
	chatID := b.config.MonetizedChatID
	// Ровно 30 секунд — минимум, чтобы не считалось «бан навсегда» по правилам Bot API.
	until := time.Now().Add(40 * time.Second).Unix()
	if _, err := b.api.Request(tgbotapi.BanChatMemberConfig{
		ChatMemberConfig: tgbotapi.ChatMemberConfig{ChatID: chatID, UserID: userID},
		UntilDate:        until,
		RevokeMessages:   false,
	}); err != nil {
		b.logger.Errorf("paywall remove unpaid direct join user=%d: %v", userID, err)
		return
	}
	txt := `Вход в эту группу только после оплаты через бота.

Нажми /start в личке с ботом — пришлю счёт. После оплаты зайди по ссылке на группу снова (или подай заявку, если включены заявки).`
	if u := b.paywallGroupInviteURL(); u != "" {
		txt += "\n\nСсылка на группу: " + u
	}
	pm := tgbotapi.NewMessage(userID, txt)
	if _, err := b.api.Send(pm); err != nil {
		b.logger.Warnf("paywall DM after kick user=%d: %v", userID, err)
	}
}

// enforcePaywallForMonetizedChatMessage — если пользователь пишет в платном чате без активной оплаты,
// удаляем его из чата и отправляем инструкцию в ЛС.
// Возвращает true, если дальнейшую обработку сообщения нужно прекратить.
func (b *Bot) enforcePaywallForMonetizedChatMessage(msg *tgbotapi.Message) bool {
	if msg == nil || msg.From == nil || msg.From.IsBot {
		return false
	}
	if !b.paywallActive() || msg.Chat.ID != b.config.MonetizedChatID {
		return false
	}
	if b.config.OwnerID != 0 && msg.From.ID == b.config.OwnerID {
		return false
	}
	ok, err := b.db.UserHasActivePaywallAccess(msg.From.ID, b.config.MonetizedChatID)
	if err != nil {
		b.logger.Errorf("paywall message gate check: %v", err)
		return false
	}
	if ok {
		return false
	}
	b.paywallKickFromMonetizedChatAndExplain(msg.From.ID)
	return true
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

	paid, err := b.db.UserHasActivePaywallAccess(userID, b.config.MonetizedChatID)
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

	if !b.config.PaywallPaymentReady() {
		b.logger.Error("PAYWALL_ENABLED but neither PAYMENT_PROVIDER_TOKEN nor YooKassa credentials are set")
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
	if b.config.PaywallUsesTelegramInvoice() {
		if err := b.SendPaywallInvoice(userID, reqID); err != nil {
			b.logger.Errorf("paywall send invoice: %v", err)
		}
		return
	}
	if err := b.SendYookassaPaymentLink(userID, reqID); err != nil {
		b.logger.Errorf("paywall send yookassa link: %v", err)
	}
}
