package bot

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"leo-bot/internal/yookassa"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const paywallPayloadPrefix = "pw_"

const paywallCallbackResendInvoice = "paywall_resend_invoice" // совместимость со старыми сообщениями
const paywallCallbackPayStars = "paywall_pay_stars"
const paywallCallbackPayYookassa = "paywall_pay_yookassa"
const paywallCallbackPayProvider = "paywall_pay_provider"

const paywallInviteCacheTTL = 25 * time.Minute

// Несколько попыток: вебхук ЮKassa может закрыть заявку в БД чуть позже события «вступил в группу».
const paywallAccessRecheckAttempts = 5
const paywallAccessRecheckDelay = 800 * time.Millisecond

func (b *Bot) userHasActivePaywallAccessResilient(userID, chatID int64) (bool, error) {
	var lastErr error
	for i := 0; i < paywallAccessRecheckAttempts; i++ {
		if i > 0 {
			time.Sleep(paywallAccessRecheckDelay)
		}
		ok, err := b.db.UserHasActivePaywallAccess(userID, chatID)
		if err != nil {
			lastErr = err
			continue
		}
		lastErr = nil
		if ok {
			return true, nil
		}
	}
	if lastErr != nil {
		return false, lastErr
	}
	return false, nil
}

// paywallInvoiceErrLog — строка для логов (полная диагностика).
func paywallInvoiceErrLog(err error) string {
	if err == nil {
		return ""
	}
	var tgErr *tgbotapi.Error
	if errors.As(err, &tgErr) {
		return fmt.Sprintf("telegram error_code=%d: %s", tgErr.Code, tgErr.Message)
	}
	return err.Error()
}

// paywallInvoiceShortHintForUser — коротко, без переменных окружения (детали только в логах).
func paywallInvoiceShortHintForUser(err error) string {
	if err == nil {
		return ""
	}
	var tgErr *tgbotapi.Error
	if !errors.As(err, &tgErr) {
		return "Попробуй ещё раз чуть позже или другой способ оплаты."
	}
	m := strings.ToLower(tgErr.Message)
	switch {
	case strings.Contains(m, "payment_provider_invalid"):
		return "Счёт в Telegram сейчас недоступен. Попробуй другой способ кнопкой ниже."
	case strings.Contains(m, "currency_invalid"), strings.Contains(m, "currency_total_amount_invalid"):
		return "Платёж не прошёл проверку. Нажми /start и запроси счёт снова."
	case strings.Contains(m, "invoice_payload_invalid"), strings.Contains(m, "invoice_invalid"):
		return "Счёт отклонён Telegram. Обнови приложение или попробуй оплату картой (вторая кнопка)."
	case tgErr.Code == 403 || strings.Contains(m, "blocked"):
		return "Разблокируй бота: ⋮ в чате → Разблокировать."
	case strings.Contains(m, "chat not found") || strings.Contains(m, "user is deactivated"):
		return "Напиши боту любое сообщение в личке и снова нажми кнопку."
	default:
		return "Не вышло отправить счёт. Попробуй оплату картой (вторая кнопка) или /start позже."
	}
}

// paywallYookassaShortHintForUser — понятная подсказка пользователю по типовым сбоям создания ссылки ЮKassa.
func paywallYookassaShortHintForUser(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(msg, "credentials empty"):
		return "Оплата ЮKassa временно недоступна (не заданы ключи). Напиши администратору."
	case strings.Contains(msg, "amount must be positive"):
		return "Оплата ЮKassa временно недоступна (некорректная сумма). Напиши администратору."
	case strings.Contains(msg, "return_url must be http"):
		return "Оплата ЮKassa временно недоступна (некорректный URL возврата). Напиши администратору."
	case strings.Contains(msg, "http 401"), strings.Contains(msg, "http 403"):
		return "ЮKassa отклонила запрос (проверь ключи магазина). Попробуй позже."
	case strings.Contains(msg, "http 400"):
		return "ЮKassa вернула ошибку параметров платежа. Попробуй позже."
	case strings.Contains(msg, "timeout"), strings.Contains(msg, "deadline exceeded"):
		return "ЮKassa долго отвечает. Попробуй ещё раз через минуту."
	default:
		return "Ссылка на оплату не создалась. Попробуй позже."
	}
}

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

// paywallFreshGroupInviteURL — новая ссылка после оплаты (без использования кэша: иначе member_limit=1 уже «съедена» или ссылка устарела).
func (b *Bot) paywallFreshGroupInviteURL() string {
	if !b.paywallActive() || b.config.MonetizedChatID == 0 {
		return ""
	}
	b.paywallInviteMu.Lock()
	defer b.paywallInviteMu.Unlock()

	b.paywallInviteFromAPI = false
	b.paywallInviteURL = ""
	b.paywallInviteCached = time.Time{}

	primary := b.config.PaywallInviteCreatesJoinRequest
	for _, createsJR := range []bool{primary, !primary} {
		u, err := b.paywallCreateInviteLink(createsJR)
		if err == nil && u != "" {
			b.paywallInviteURL = u
			b.paywallInviteCached = time.Now()
			b.paywallInviteFromAPI = true
			return u
		}
		b.logger.Warnf("paywall fresh createChatInviteLink (creates_join_request=%v): %v", createsJR, err)
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
	if b.config.PaywallUsesStars() {
		priceRub += fmt.Sprintf(" Звёзды Telegram: %d ⭐.", b.config.PaywallStarsInvoiceAmount())
	}
	if b.config.PaywallYookassaReady() {
		yk := b.config.YookassaAmountMinor
		yc := b.config.YookassaCurrency
		if yc == "RUB" && yk > 0 {
			rub := yk / 100
			kop := yk % 100
			if kop == 0 {
				priceRub += fmt.Sprintf(" Карта (ЮKassa): %d ₽.", rub)
			} else {
				priceRub += fmt.Sprintf(" Карта (ЮKassa): %d,%02d ₽.", rub, kop)
			}
		} else if yk > 0 && yc != "" {
			priceRub += fmt.Sprintf(" Карта (ЮKassa): %d мин. ед. %s.", yk, yc)
		}
	}
	if b.config.PaywallUsesTelegramProviderInvoice() && b.config.PaymentCurrency == "RUB" && b.config.PaymentAmountMinorUnits > 0 {
		rub := b.config.PaymentAmountMinorUnits / 100
		kop := b.config.PaymentAmountMinorUnits % 100
		if kop == 0 {
			priceRub += fmt.Sprintf(" Карта (Telegram): %d ₽.", rub)
		} else {
			priceRub += fmt.Sprintf(" Карта (Telegram): %d,%02d ₽.", rub, kop)
		}
	} else if b.config.PaywallUsesTelegramProviderInvoice() && b.config.PaymentAmountMinorUnits > 0 && b.config.PaymentCurrency != "" && b.config.PaymentCurrency != "XTR" {
		priceRub += fmt.Sprintf(" Карта (Telegram): %d мин. ед. %s.", b.config.PaymentAmountMinorUnits, b.config.PaymentCurrency)
	}

	if !b.config.PaywallPaymentReady() {
		return `💳 Платный вход в закрытую группу

⚠️ Оплата у бота ещё не настроена. Напиши администратору.

Когда заработает — здесь появятся кнопки выбора способа оплаты.` + priceRub + `

Доступ после оплаты — 30 дней.`
	}

	return `💳 Платный вход в закрытую группу

Нажми кнопку нужного способа — пришлю счёт или ссылку на оплату. Достаточно **одного** успешного платежа.

После оплаты пришлю кнопку входа в группу. Полную справку бота — тоже после оплаты. Доступ 30 дней.` + priceRub + `

Пока без оплаты длинную справку не показываю.

_Кнопки работают только в этом чате с ботом — пересланное сообщение их не показывает._

👇 Выбери способ оплаты:`
}

// paywallUnpaidInlineKeyboard — отдельные кнопки под каждый способ оплаты.
func (b *Bot) paywallUnpaidInlineKeyboard() *tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton
	if b.config.PaywallUsesStars() {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⭐ Счёт на звёзды", paywallCallbackPayStars),
		))
	}
	if b.config.PaywallYookassaReady() {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("💳 Карта — ссылка ЮKassa", paywallCallbackPayYookassa),
		))
	}
	if b.config.PaywallUsesTelegramProviderInvoice() {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("💳 Счёт в Telegram (карта)", paywallCallbackPayProvider),
		))
	}
	if len(rows) == 0 {
		return nil
	}
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

// ensurePaywallInvoiceSent создаёт pending-заявку и подтягивает оплату ЮKassa при /start; счета не шлёт — пользователь жмёт кнопки.
func (b *Bot) ensurePaywallInvoiceSent(userID int64) {
	if !b.paywallActive() || userID == 0 {
		return
	}
	if !b.config.PaywallPaymentReady() {
		return
	}
	if ok, err := b.db.UserHasActivePaywallAccess(userID, b.config.MonetizedChatID); err != nil {
		b.logger.Errorf("paywall ensure invoice access check: %v", err)
	} else if ok {
		return
	}
	if b.config.PaywallYookassaReady() {
		if b.paywallTrySyncYookassaPayment(userID) {
			return
		}
	}
	pending, err := b.db.GetLatestPendingPaywallAccessRequest(userID, b.config.MonetizedChatID)
	if err != nil {
		b.logger.Errorf("paywall ensure invoice get pending: %v", err)
		b.paywallNotifyUser(userID, "⚠️ Временная ошибка. Попробуй /start чуть позже.")
		return
	}
	if pending != nil {
		return
	}
	if _, err := b.db.InsertPaywallAccessRequest(userID, b.config.MonetizedChatID); err != nil {
		b.logger.Errorf("paywall ensure invoice insert: %v", err)
		b.paywallNotifyUser(userID, "⚠️ Не удалось начать оплату. Попробуй /start снова.")
	}
}

// paywallGetOrCreatePendingReqID — для callback: последняя pending-заявка или новая.
func (b *Bot) paywallGetOrCreatePendingReqID(userID int64) (int64, error) {
	rec, err := b.db.GetLatestPendingPaywallAccessRequest(userID, b.config.MonetizedChatID)
	if err != nil {
		return 0, err
	}
	if rec != nil {
		return rec.ID, nil
	}
	return b.db.InsertPaywallAccessRequest(userID, b.config.MonetizedChatID)
}

// paywallSendPaymentOffers — всё сразу (старые кнопки «выслать снова»); ошибки пользователю короткие, детали в логах.
func (b *Bot) paywallSendPaymentOffers(userID, reqID int64) {
	if b.config.PaywallUsesStars() {
		if err := b.SendPaywallStarsInvoice(userID, reqID); err != nil {
			b.logger.Errorf("paywall stars invoice: %s", paywallInvoiceErrLog(err))
			b.paywallNotifyUser(userID, "⚠️ "+paywallInvoiceShortHintForUser(err))
		}
	}
	if b.config.PaywallUsesTelegramProviderInvoice() {
		if err := b.SendPaywallProviderInvoice(userID, reqID); err != nil {
			b.logger.Errorf("paywall provider invoice: %s", paywallInvoiceErrLog(err))
			b.paywallNotifyUser(userID, "⚠️ "+paywallInvoiceShortHintForUser(err))
		}
	}
	if b.config.PaywallYookassaReady() {
		if err := b.SendYookassaPaymentLink(userID, reqID); err != nil {
			b.logger.Errorf("paywall yookassa link: %v", err)
			b.paywallNotifyUser(userID, "⚠️ "+paywallYookassaShortHintForUser(err))
		}
	}
}

// SendPaywallStarsInvoice — XTR, provider_token пустой; payload pw_<reqID>.
func paywallInvoiceClipTitle(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "Доступ"
	}
	r := []rune(s)
	if len(r) > 32 {
		return string(r[:32])
	}
	return s
}

func paywallInvoiceClipDescription(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "Оплата доступа"
	}
	r := []rune(s)
	if len(r) > 255 {
		return string(r[:255])
	}
	return s
}

func (b *Bot) SendPaywallStarsInvoice(userID, reqID int64) error {
	if !b.config.PaywallUsesStars() {
		return fmt.Errorf("stars payment not configured")
	}
	amt := b.config.PaywallStarsInvoiceAmount()
	if amt <= 0 {
		return fmt.Errorf("stars amount invalid")
	}
	payload := fmt.Sprintf("%s%d", paywallPayloadPrefix, reqID)
	prices := []tgbotapi.LabeledPrice{{Label: "Доступ", Amount: amt}}
	inv := tgbotapi.NewInvoice(
		userID,
		paywallInvoiceClipTitle(b.config.PaymentInvoiceTitle),
		paywallInvoiceClipDescription(b.config.PaymentInvoiceDesc),
		payload,
		"",
		"",
		"XTR",
		prices,
	)
	// Workaround for Telegram API validation: library may encode nil as null.
	// Telegram expects an array for suggested_tip_amounts when field is present.
	inv.SuggestedTipAmounts = []int{}
	_, err := b.api.Send(inv)
	return err
}

// SendPaywallProviderInvoice — RUB/др. через PAYMENT_PROVIDER_TOKEN; payload pw_<reqID>.
func (b *Bot) SendPaywallProviderInvoice(userID, reqID int64) error {
	if !b.config.PaywallUsesTelegramProviderInvoice() {
		return fmt.Errorf("telegram provider invoice not configured")
	}
	tok := strings.TrimSpace(b.config.PaymentProviderToken)
	payload := fmt.Sprintf("%s%d", paywallPayloadPrefix, reqID)
	prices := []tgbotapi.LabeledPrice{{Label: "Доступ", Amount: b.config.PaymentAmountMinorUnits}}
	inv := tgbotapi.NewInvoice(
		userID,
		paywallInvoiceClipTitle(b.config.PaymentInvoiceTitle),
		paywallInvoiceClipDescription(b.config.PaymentInvoiceDesc),
		payload,
		tok,
		"",
		b.config.PaymentCurrency,
		prices,
	)
	inv.SuggestedTipAmounts = []int{}
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
	paymentID, confirmURL, err := yookassa.CreatePayment(
		b.config.YookassaShopID,
		b.config.YookassaSecretKey,
		b.config.YookassaAmountMinor,
		b.config.YookassaCurrency,
		b.config.PaymentInvoiceDesc,
		returnURL,
		b.config.YookassaNotificationURL,
		meta,
	)
	if err != nil {
		return err
	}
	if err := b.db.SetPaywallYookassaPaymentID(reqID, paymentID); err != nil {
		b.logger.Warnf("paywall SetPaywallYookassaPaymentID: %v", err)
	}
	text := `💳 Оплата доступа картой (ЮKassa).

Если оплатил(а), а доступ не открылся в течение пары минут — нажми /start: бот проверит платёж в ЮKassa напрямую (если вебхук не сработал).

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
		_, _ = b.api.Request(tgbotapi.NewCallbackWithAlert(callback.ID, "Оплата сейчас недоступна."))
		return
	}
	if b.config.PaywallYookassaReady() {
		if b.paywallTrySyncYookassaPayment(callback.From.ID) {
			_, _ = b.api.Request(tgbotapi.NewCallback(callback.ID, "Оплата уже учтена. Нажми /start."))
			return
		}
	}
	reqID, err := b.paywallGetOrCreatePendingReqID(callback.From.ID)
	if err != nil {
		b.logger.Errorf("paywall resend pending: %v", err)
		_, _ = b.api.Request(tgbotapi.NewCallbackWithAlert(callback.ID, "Ошибка. Попробуй /start."))
		return
	}
	b.paywallSendPaymentOffers(callback.From.ID, reqID)
	_, _ = b.api.Request(tgbotapi.NewCallback(callback.ID, "Проверь новые сообщения в чате."))
}

func (b *Bot) handlePaywallPayStarsCallback(callback *tgbotapi.CallbackQuery) {
	if callback.From == nil {
		_, _ = b.api.Request(tgbotapi.NewCallback(callback.ID, ""))
		return
	}
	uid := callback.From.ID
	if !b.paywallActive() || !b.config.PaywallPaymentReady() || !b.config.PaywallUsesStars() {
		_, _ = b.api.Request(tgbotapi.NewCallbackWithAlert(callback.ID, "Счёт на звёзды сейчас недоступен."))
		return
	}
	if b.config.PaywallYookassaReady() && b.paywallTrySyncYookassaPayment(uid) {
		_, _ = b.api.Request(tgbotapi.NewCallback(callback.ID, "Оплата уже учтена. Нажми /start."))
		return
	}
	reqID, err := b.paywallGetOrCreatePendingReqID(uid)
	if err != nil {
		b.logger.Errorf("paywall stars cb pending: %v", err)
		_, _ = b.api.Request(tgbotapi.NewCallbackWithAlert(callback.ID, "Ошибка. Попробуй /start."))
		return
	}
	if err := b.SendPaywallStarsInvoice(uid, reqID); err != nil {
		b.logger.Errorf("paywall stars invoice: %s", paywallInvoiceErrLog(err))
		h := paywallInvoiceShortHintForUser(err)
		if len(h) > 180 {
			h = h[:177] + "…"
		}
		_, _ = b.api.Request(tgbotapi.NewCallbackWithAlert(callback.ID, h))
		return
	}
	_, _ = b.api.Request(tgbotapi.NewCallback(callback.ID, "Счёт на звёзды отправлен — открой его выше и нажми «Оплатить»."))
}

func (b *Bot) handlePaywallPayYookassaCallback(callback *tgbotapi.CallbackQuery) {
	if callback.From == nil {
		_, _ = b.api.Request(tgbotapi.NewCallback(callback.ID, ""))
		return
	}
	uid := callback.From.ID
	if !b.paywallActive() || !b.config.PaywallPaymentReady() || !b.config.PaywallYookassaReady() {
		_, _ = b.api.Request(tgbotapi.NewCallbackWithAlert(callback.ID, "Оплата картой сейчас недоступна."))
		return
	}
	if b.paywallTrySyncYookassaPayment(uid) {
		_, _ = b.api.Request(tgbotapi.NewCallback(callback.ID, "Оплата уже учтена. Нажми /start."))
		return
	}
	reqID, err := b.paywallGetOrCreatePendingReqID(uid)
	if err != nil {
		b.logger.Errorf("paywall yk cb pending: %v", err)
		_, _ = b.api.Request(tgbotapi.NewCallbackWithAlert(callback.ID, "Ошибка. Попробуй /start."))
		return
	}
	if err := b.SendYookassaPaymentLink(uid, reqID); err != nil {
		b.logger.Errorf("paywall yookassa link: %v", err)
		h := paywallYookassaShortHintForUser(err)
		if len(h) > 180 {
			h = h[:177] + "…"
		}
		_, _ = b.api.Request(tgbotapi.NewCallbackWithAlert(callback.ID, h))
		return
	}
	_, _ = b.api.Request(tgbotapi.NewCallback(callback.ID, "Ссылка на оплату отправлена — открой сообщение ниже."))
}

func (b *Bot) handlePaywallPayProviderCallback(callback *tgbotapi.CallbackQuery) {
	if callback.From == nil {
		_, _ = b.api.Request(tgbotapi.NewCallback(callback.ID, ""))
		return
	}
	uid := callback.From.ID
	if !b.paywallActive() || !b.config.PaywallPaymentReady() || !b.config.PaywallUsesTelegramProviderInvoice() {
		_, _ = b.api.Request(tgbotapi.NewCallbackWithAlert(callback.ID, "Счёт провайдера сейчас недоступен."))
		return
	}
	if b.config.PaywallYookassaReady() && b.paywallTrySyncYookassaPayment(uid) {
		_, _ = b.api.Request(tgbotapi.NewCallback(callback.ID, "Оплата уже учтена. Нажми /start."))
		return
	}
	reqID, err := b.paywallGetOrCreatePendingReqID(uid)
	if err != nil {
		b.logger.Errorf("paywall provider cb pending: %v", err)
		_, _ = b.api.Request(tgbotapi.NewCallbackWithAlert(callback.ID, "Ошибка. Попробуй /start."))
		return
	}
	if err := b.SendPaywallProviderInvoice(uid, reqID); err != nil {
		b.logger.Errorf("paywall provider invoice: %s", paywallInvoiceErrLog(err))
		h := paywallInvoiceShortHintForUser(err)
		if len(h) > 180 {
			h = h[:177] + "…"
		}
		_, _ = b.api.Request(tgbotapi.NewCallbackWithAlert(callback.ID, h))
		return
	}
	_, _ = b.api.Request(tgbotapi.NewCallback(callback.ID, "Счёт отправлен — открой его выше и нажми «Оплатить»."))
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

// paywallTrySyncYookassaPayment — если вебхук не дошёл, подтягиваем успешную оплату через GET /v3/payments/{id}.
func (b *Bot) paywallTrySyncYookassaPayment(userID int64) bool {
	if !b.paywallActive() || userID == 0 {
		return false
	}
	if !b.config.PaywallYookassaReady() {
		return false
	}
	pending, err := b.db.GetLatestPendingPaywallAccessRequest(userID, b.config.MonetizedChatID)
	if err != nil {
		b.logger.Errorf("paywall yookassa sync get pending: %v", err)
		return false
	}
	if pending == nil {
		return false
	}
	if !pending.YookassaPaymentID.Valid || strings.TrimSpace(pending.YookassaPaymentID.String) == "" {
		b.logger.Warnf(
			"paywall yookassa sync: заявка id=%d user=%d в pending, но yookassa_payment_id пуст — опрос API ЮKassa невозможен. "+
				"Нужны миграция 16, деплой бота с сохранением payment id и новая ссылка на оплату (или рабочий вебхук ms_payments).",
			pending.ID, userID,
		)
		return false
	}
	info, err := yookassa.GetPayment(b.config.YookassaShopID, b.config.YookassaSecretKey, pending.YookassaPaymentID.String)
	if err != nil {
		b.logger.Warnf("paywall yookassa sync GetPayment: %v", err)
		return false
	}
	st := strings.ToLower(strings.TrimSpace(info.Status))
	if st != "succeeded" || !info.Paid {
		return false
	}
	meta := info.Metadata
	userStr := strings.TrimSpace(meta["user_telegram_id"])
	if userStr == "" {
		userStr = strings.TrimSpace(meta["user_telegramId"])
	}
	payloadStr := strings.TrimSpace(meta["invoice_payload"])
	if payloadStr == "" {
		payloadStr = strings.TrimSpace(meta["invoicePayload"])
	}
	if userStr != fmt.Sprintf("%d", userID) {
		b.logger.Warnf("paywall yookassa sync user mismatch meta=%q db_user=%d", userStr, userID)
		return false
	}
	reqFromMeta, ok := parsePaywallPayload(payloadStr)
	if !ok || reqFromMeta != pending.ID {
		b.logger.Warnf("paywall yookassa sync payload mismatch meta=%q want id=%d", payloadStr, pending.ID)
		return false
	}
	amountMinor := info.AmountMinor
	cur := info.Currency
	if amountMinor <= 0 || cur == "" {
		amountMinor = b.config.YookassaAmountMinor
		cur = b.config.YookassaCurrency
	}
	if cur == "" {
		cur = "RUB"
	}
	okDb, err := b.db.CompletePaywallAccessRequest(pending.ID, userID, b.config.MonetizedChatID, info.ID, amountMinor, cur)
	if err != nil {
		b.logger.Errorf("paywall yookassa sync complete: %v", err)
		return false
	}
	if !okDb {
		paid, err := b.db.UserHasActivePaywallAccess(userID, b.config.MonetizedChatID)
		if err == nil && paid {
			b.logger.Infof("paywall yookassa sync: заявка %d уже completed (вебхук)", pending.ID)
		}
		return false
	}
	b.logger.Infof("paywall yookassa sync: заявка %d закрыта по API ЮKassa (вебхук мог не сработать)", pending.ID)
	b.paywallDeliverAccessAfterPayment(userID, userID)
	return true
}

func (b *Bot) handlePaywallPreCheckout(q *tgbotapi.PreCheckoutQuery) {
	if q.From == nil {
		_, _ = b.api.Request(tgbotapi.PreCheckoutConfig{PreCheckoutQueryID: q.ID, OK: false, ErrorMessage: "Оплата недоступна."})
		return
	}
	telegramInvoice := b.config.PaywallUsesTelegramInvoice()
	if !b.paywallActive() || !telegramInvoice {
		_, _ = b.api.Request(tgbotapi.PreCheckoutConfig{PreCheckoutQueryID: q.ID, OK: false, ErrorMessage: "Оплата недоступна."})
		return
	}
	switch q.Currency {
	case "XTR":
		if !b.config.PaywallUsesStars() || q.TotalAmount != b.config.PaywallStarsInvoiceAmount() {
			b.logger.Warnf("paywall pre_checkout stars mismatch: got %s %d want XTR %d", q.Currency, q.TotalAmount, b.config.PaywallStarsInvoiceAmount())
			_, _ = b.api.Request(tgbotapi.PreCheckoutConfig{PreCheckoutQueryID: q.ID, OK: false, ErrorMessage: "Неверная сумма (звёзды). Обнови заявку /start."})
			return
		}
	default:
		if !b.config.PaywallUsesTelegramProviderInvoice() {
			_, _ = b.api.Request(tgbotapi.PreCheckoutConfig{PreCheckoutQueryID: q.ID, OK: false, ErrorMessage: "Оплата недоступна."})
			return
		}
		if strings.TrimSpace(b.config.PaymentProviderToken) == "" {
			_, _ = b.api.Request(tgbotapi.PreCheckoutConfig{PreCheckoutQueryID: q.ID, OK: false, ErrorMessage: "Оплата недоступна."})
			return
		}
		if q.Currency != b.config.PaymentCurrency || q.TotalAmount != b.config.PaymentAmountMinorUnits {
			b.logger.Warnf("paywall pre_checkout amount mismatch: got %s %d want %s %d", q.Currency, q.TotalAmount, b.config.PaymentCurrency, b.config.PaymentAmountMinorUnits)
			_, _ = b.api.Request(tgbotapi.PreCheckoutConfig{PreCheckoutQueryID: q.ID, OK: false, ErrorMessage: "Неверная сумма. Обнови заявку и попробуй снова."})
			return
		}
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

// paywallDeliverAccessAfterPayment — приглашение в группу и приветствие после зачёта оплаты (Telegram Payments / ЮKassa / sync API).
func (b *Bot) paywallDeliverAccessAfterPayment(chatID, userID int64) {
	inviteURL := b.paywallFreshGroupInviteURL()

	_, err := b.api.Request(tgbotapi.ApproveChatJoinRequestConfig{
		ChatConfig: tgbotapi.ChatConfig{ChatID: b.config.MonetizedChatID},
		UserID:     userID,
	})
	if err != nil {
		b.logger.Errorf("paywall approve join request failed: %v", err)
		follow := "✅ Оплата принята, доступ открыт на 30 дней.\n\nЕсли ты ещё не в группе — нажми кнопку ниже и отправь заявку на вступление."
		pm := tgbotapi.NewMessage(chatID, follow)
		if inviteURL != "" {
			pm.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{
				InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonURL("📩 Войти в группу", inviteURL),
					),
				},
			}
		} else {
			pm.Text += "\n\nНе удалось создать ссылку автоматически — попроси ссылку у администратора."
		}
		b.api.Send(pm)
		welcome := welcomeStartText()
		wmsg := tgbotapi.NewMessage(chatID, welcome)
		b.api.Send(wmsg)
		return
	}
	done := tgbotapi.NewMessage(chatID, "✅ Оплата принята, доступ к группе открыт на 30 дней. Если ты ещё не в группе — нажми кнопку ниже.")
	if inviteURL != "" {
		done.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{
			InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonURL("📩 Войти в группу", inviteURL),
				),
			},
		}
	}
	if _, err := b.api.Send(done); err != nil {
		b.logger.Errorf("paywall send done msg: %v", err)
	}
	welcome := welcomeStartText()
	wmsg := tgbotapi.NewMessage(chatID, welcome)
	if _, err := b.api.Send(wmsg); err != nil {
		b.logger.Errorf("paywall send welcome after payment: %v", err)
	}
}

func (b *Bot) handlePaywallSuccessfulPayment(msg *tgbotapi.Message) {
	if !b.paywallActive() || msg.From == nil || msg.SuccessfulPayment == nil {
		return
	}
	sp := msg.SuccessfulPayment
	switch sp.Currency {
	case "XTR":
		if !b.config.PaywallUsesStars() || sp.TotalAmount != b.config.PaywallStarsInvoiceAmount() {
			b.logger.Errorf(
				"paywall successful_payment stars mismatch: got %d, want %d — PAYMENT_STARS_AMOUNT / XTR",
				sp.TotalAmount, b.config.PaywallStarsInvoiceAmount(),
			)
			return
		}
	default:
		if !b.config.PaywallUsesTelegramProviderInvoice() || sp.Currency != b.config.PaymentCurrency || sp.TotalAmount != b.config.PaymentAmountMinorUnits {
			b.logger.Errorf(
				"paywall successful_payment mismatch: got %s %d, config wants %s %d — провайдер / PAYMENT_AMOUNT_*",
				sp.Currency, sp.TotalAmount, b.config.PaymentCurrency, b.config.PaymentAmountMinorUnits,
			)
			return
		}
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

	b.paywallDeliverAccessAfterPayment(msg.Chat.ID, msg.From.ID)
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
	paid, err := b.userHasActivePaywallAccessResilient(userID, chatID)
	if err != nil {
		b.logger.Errorf("paywall direct join paid check: %v", err)
		return false
	}
	if paid {
		return false
	}
	b.logger.Warnf(
		"paywall: kick direct join user=%d chat=%d — нет активной записи completed+access_expires; последние заявки: %s",
		userID, chatID, b.db.PaywallAccessDebugSnapshot(userID, chatID),
	)
	return true
}

func (b *Bot) paywallKickFromMonetizedChatAndExplain(userID int64) {
	chatID := b.config.MonetizedChatID
	// Вышибаем из группы через ограниченный ban (требование API), иначе клиент Telegram долго
	// показывает «забанен админом». Сразу после — unban: пользователь не в чате, но не числится
	// в чёрном списке и может снова зайти по ссылке после появления строки доступа в БД.
	until := time.Now().Add(40 * time.Second).Unix()
	if _, err := b.api.Request(tgbotapi.BanChatMemberConfig{
		ChatMemberConfig: tgbotapi.ChatMemberConfig{ChatID: chatID, UserID: userID},
		UntilDate:        until,
		RevokeMessages:   false,
	}); err != nil {
		b.logger.Errorf("paywall remove unpaid direct join user=%d: %v", userID, err)
		return
	}
	if _, err := b.api.Request(tgbotapi.UnbanChatMemberConfig{
		ChatMemberConfig: tgbotapi.ChatMemberConfig{ChatID: chatID, UserID: userID},
		OnlyIfBanned:     false,
	}); err != nil {
		b.logger.Warnf("paywall unban after kick user=%d: %v", userID, err)
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
	ok, err := b.userHasActivePaywallAccessResilient(msg.From.ID, b.config.MonetizedChatID)
	if err != nil {
		b.logger.Errorf("paywall message gate check: %v", err)
		return false
	}
	if ok {
		return false
	}
	b.logger.Warnf(
		"paywall: kick on message user=%d chat=%d — нет активной записи completed+access_expires; заявки: %s",
		msg.From.ID, msg.Chat.ID, b.db.PaywallAccessDebugSnapshot(msg.From.ID, b.config.MonetizedChatID),
	)
	b.paywallKickFromMonetizedChatAndExplain(msg.From.ID)
	return true
}

func (b *Bot) paywallDeclineJoinRequest(userID int64) {
	if userID == 0 || b.config.MonetizedChatID == 0 {
		return
	}
	if _, err := b.api.Request(tgbotapi.DeclineChatJoinRequest{
		ChatConfig: tgbotapi.ChatConfig{ChatID: b.config.MonetizedChatID},
		UserID:     userID,
	}); err != nil {
		b.logger.Warnf("paywall decline join request user=%d: %v", userID, err)
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

	if b.config.OwnerID != 0 && userID == b.config.OwnerID {
		if _, err := b.api.Request(tgbotapi.ApproveChatJoinRequestConfig{
			ChatConfig: tgbotapi.ChatConfig{ChatID: b.config.MonetizedChatID},
			UserID:     userID,
		}); err != nil {
			b.logger.Errorf("paywall approve owner join request: %v", err)
		}
		return
	}

	paid, err := b.userHasActivePaywallAccessResilient(userID, b.config.MonetizedChatID)
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
		b.logger.Error("PAYWALL_ENABLED but payment not configured: PAYMENT_STARS_ENABLED, XTR, PAYMENT_PROVIDER_TOKEN, or YooKassa+RUB")
		b.paywallDeclineJoinRequest(userID)
		b.paywallNotifyUser(userID, "⚠️ Вход в группу только после оплаты через бота, но оплата у бота не настроена. Напиши администратору.")
		return
	}

	if _, err := b.paywallGetOrCreatePendingReqID(userID); err != nil {
		b.logger.Errorf("paywall join request pending: %v", err)
		b.paywallDeclineJoinRequest(userID)
		return
	}
	b.paywallNotifyUser(userID, "Вход в группу после оплаты. Открой этого бота в личке, нажми /start и выбери способ: звёзды или карта (ЮKassa).")
	// Без записи об активной оплате в БД в группу не пускаем — отклоняем «висящую» заявку;
	// после оплаты бот пришлёт ссылку / одобрит вступление.
	b.paywallDeclineJoinRequest(userID)
}
