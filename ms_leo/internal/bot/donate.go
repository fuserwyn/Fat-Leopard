package bot

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"leo-bot/internal/database"
	"leo-bot/internal/yookassa"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Донат — добровольная поддержка проекта из профиля мини-аппа. Раньше единственной
// оплатой был платный вход в личке бота; теперь вход бесплатный (PAYWALL_ENTRY_FREE),
// а платить можно по желанию. Донат не выдаёт доступ, не отменяет кик за неактивность
// и не связан с paywall_access_requests — это отдельная таблица donations.
//
// Звёзды: createInvoiceLink (XTR) + WebApp.openInvoice — оплата не выходит из мини-аппа.
// Карта РФ: тот же ЮKassa, что у платного возврата, но confirmation URL открывается
// через WebApp.openLink, а статус мини-апп доопрашивает сам (вебхук ms_payments
// обслуживает только pw_-платежи, донаты он пропускает).
const donatePayloadPrefix = "dn_"

// donateYookassaSyncMaxAge — насколько назад доопрашиваем незакрытые донаты по ссылке ЮKassa.
const donateYookassaSyncMaxAge = 24 * time.Hour

func donatePayload(donationID int64) string {
	return fmt.Sprintf("%s%d", donatePayloadPrefix, donationID)
}

// parseDonatePayload — «dn_42» → 42. ok=false для чужих payload (например paywall pw_<id>).
func parseDonatePayload(payload string) (donationID int64, ok bool) {
	payload = strings.TrimSpace(payload)
	if !strings.HasPrefix(payload, donatePayloadPrefix) {
		return 0, false
	}
	id, err := strconv.ParseInt(payload[len(donatePayloadPrefix):], 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// IsDonatePayload — публичная проверка для роутинга платёжных апдейтов.
func IsDonatePayload(payload string) bool {
	_, ok := parseDonatePayload(payload)
	return ok
}

// DonateOptions — номиналы и доступные способы для экрана профиля.
type DonateOptions struct {
	StarsTiers     []int `json:"stars_tiers"`
	CardTiersRub   []int `json:"card_tiers_rub"`
	StarsAvailable bool  `json:"stars_available"`
	CardAvailable  bool  `json:"card_available"`
	CompletedCount int   `json:"completed_count"`
}

// Обёртки конфига для HTTP-слоя мини-аппа (miniappapi видит только *bot.Bot).

// DonateStarsReady — донат звёздами настроен.
func (b *Bot) DonateStarsReady() bool { return b.config.DonateStarsReady() }

// DonateCardReady — донат картой РФ настроен (ключи ЮKassa + номиналы).
func (b *Bot) DonateCardReady() bool { return b.config.DonateCardReady() }

// DonateStarsTierAllowed — сумма звёзд из списка номиналов.
func (b *Bot) DonateStarsTierAllowed(stars int) bool { return b.config.DonateStarsTierAllowed(stars) }

// DonateCardTierAllowed — сумма в рублях из списка номиналов.
func (b *Bot) DonateCardTierAllowed(rub int) bool { return b.config.DonateCardTierAllowed(rub) }

// DonateOptionsForUser — что показать в секции «Задонатить».
func (b *Bot) DonateOptionsForUser(userID int64) DonateOptions {
	out := DonateOptions{
		StarsAvailable: b.config.DonateStarsReady(),
		CardAvailable:  b.config.DonateCardReady(),
		StarsTiers:     []int{},
		CardTiersRub:   []int{},
	}
	if out.StarsAvailable {
		out.StarsTiers = b.config.DonateStarsTiers
	}
	if out.CardAvailable {
		out.CardTiersRub = b.config.DonateCardTiersRub
	}
	if userID != 0 {
		if n, err := b.db.UserDonationsSummary(userID); err != nil {
			b.logger.Warnf("donate summary user=%d: %v", userID, err)
		} else {
			out.CompletedCount = n
		}
	}
	return out
}

// CreateDonateStarsInvoiceLink — ссылка на счёт в звёздах для WebApp.openInvoice.
// tgbotapi v5 этой версии не знает createInvoiceLink, поэтому вызываем метод напрямую
// (как setChatMenuButton в miniapp_menu_button.go).
func (b *Bot) CreateDonateStarsInvoiceLink(userID int64, stars int) (link string, donationID int64, err error) {
	if userID == 0 {
		return "", 0, fmt.Errorf("donate stars: empty user")
	}
	if !b.config.DonateStarsReady() {
		return "", 0, fmt.Errorf("donate stars not configured")
	}
	if !b.config.DonateStarsTierAllowed(stars) {
		return "", 0, fmt.Errorf("donate stars: amount %d not in tiers", stars)
	}

	donationID, err = b.db.InsertDonation(userID, "stars", stars, "XTR")
	if err != nil {
		return "", 0, err
	}

	link, err = b.createStarsInvoiceLink(donationID, stars, "Поддержка Fat Leopard", "Поддержать Fat Leopard", donateStarsInvoiceDescription(stars))
	if err != nil {
		return "", 0, err
	}

	b.db.TrackEvent(database.AnalyticsEvent{
		Name:       database.EventDonateInitiated,
		UserID:     userID,
		TelegramID: userID,
		Payload:    map[string]any{"provider": "stars", "amount": stars, "currency": "XTR"},
	})
	return link, donationID, nil
}

// createStarsInvoiceLink — createInvoiceLink в звёздах на готовую запись donations:
// оплату закрывают те же обработчики, что и у доната (payload dn_<id>).
func (b *Bot) createStarsInvoiceLink(donationID int64, stars int, priceLabel, title, description string) (string, error) {
	prices, err := json.Marshal([]tgbotapi.LabeledPrice{{Label: priceLabel, Amount: stars}})
	if err != nil {
		return "", fmt.Errorf("donate stars prices: %w", err)
	}
	params := tgbotapi.Params{
		"title":       paywallInvoiceClipTitle(title),
		"description": paywallInvoiceClipDescription(description),
		"payload":     donatePayload(donationID),
		"currency":    "XTR",
		"prices":      string(prices),
	}
	resp, err := b.api.MakeRequest("createInvoiceLink", params)
	if err != nil {
		return "", fmt.Errorf("createInvoiceLink: %w", err)
	}
	var link string
	if err := json.Unmarshal(resp.Result, &link); err != nil {
		return "", fmt.Errorf("createInvoiceLink result: %w", err)
	}
	link = strings.TrimSpace(link)
	if link == "" {
		return "", fmt.Errorf("createInvoiceLink: empty link")
	}
	return link, nil
}

func donateStarsInvoiceDescription(stars int) string {
	return fmt.Sprintf(
		"Добровольная поддержка проекта: %d %s. Доступ к стае это не меняет — он бесплатный, и от кика за неактивность донат не спасает. Спасибо, что помогаешь Лео жить!",
		stars, starsWordRU(stars),
	)
}

// CreateDonateCardPayment — платёж ЮKassa на выбранный номинал; возвращает ссылку на оплату.
func (b *Bot) CreateDonateCardPayment(userID int64, rub int) (confirmURL string, donationID int64, err error) {
	if userID == 0 {
		return "", 0, fmt.Errorf("donate card: empty user")
	}
	if !b.config.DonateCardReady() {
		return "", 0, fmt.Errorf("donate card not configured")
	}
	if !b.config.DonateCardTierAllowed(rub) {
		return "", 0, fmt.Errorf("donate card: amount %d not in tiers", rub)
	}

	amountMinor := rub * 100
	donationID, err = b.db.InsertDonation(userID, "yookassa", amountMinor, "RUB")
	if err != nil {
		return "", 0, err
	}

	returnURL := strings.TrimSpace(b.config.YookassaReturnURL)
	if returnURL == "" {
		returnURL = "https://t.me"
	}
	meta := map[string]string{
		"user_telegram_id": strconv.FormatInt(userID, 10),
		"invoice_payload":  donatePayload(donationID),
		"kind":             "donation",
	}
	paymentID, confirmURL, err := yookassa.CreatePayment(
		b.config.YookassaShopID,
		b.config.YookassaSecretKey,
		amountMinor,
		"RUB",
		fmt.Sprintf("Поддержка проекта Fat Leopard — %d ₽", rub),
		returnURL,
		b.config.YookassaNotificationURL,
		meta,
	)
	if err != nil {
		return "", 0, err
	}
	if err := b.db.SetDonationYookassaPaymentID(donationID, paymentID); err != nil {
		b.logger.Warnf("donate SetDonationYookassaPaymentID id=%d: %v", donationID, err)
	}

	b.db.TrackEvent(database.AnalyticsEvent{
		Name:       database.EventDonateInitiated,
		UserID:     userID,
		TelegramID: userID,
		Payload:    map[string]any{"provider": "yukassa", "amount": rub, "currency": "RUB"},
	})
	return confirmURL, donationID, nil
}

// DonationStatus — статус доната для мини-аппа: pending | completed.
// Для ЮKassa сначала доопрашиваем API — вебхук ms_payments донаты не закрывает.
func (b *Bot) DonationStatus(userID, donationID int64) (status string, err error) {
	rec, err := b.db.GetDonationByID(donationID)
	if err != nil {
		return "", err
	}
	if rec == nil || rec.UserID != userID {
		return "", fmt.Errorf("donation %d not found for user %d", donationID, userID)
	}
	if rec.Status == "completed" {
		return "completed", nil
	}
	if rec.Provider == "yookassa" && b.donateTrySyncYookassaDonation(rec) {
		return "completed", nil
	}
	return "pending", nil
}

// DonateSyncPendingForUser — подтягивает оплаченные донаты по ссылке ЮKassa (вызов при /start
// и из мини-аппа), чтобы «спасибо» дошло даже если пользователь не вернулся в приложение.
func (b *Bot) DonateSyncPendingForUser(userID int64) {
	if userID == 0 || !b.config.DonateCardReady() {
		return
	}
	pending, err := b.db.PendingYookassaDonations(userID, donateYookassaSyncMaxAge, 5)
	if err != nil {
		b.logger.Warnf("donate sync list user=%d: %v", userID, err)
		return
	}
	for _, rec := range pending {
		b.donateTrySyncYookassaDonation(rec)
	}
}

// donateTrySyncYookassaDonation — GET /v3/payments/{id}: succeeded → закрываем донат и благодарим.
func (b *Bot) donateTrySyncYookassaDonation(rec *database.Donation) bool {
	if rec == nil || !rec.YookassaPaymentID.Valid {
		return false
	}
	paymentID := strings.TrimSpace(rec.YookassaPaymentID.String)
	if paymentID == "" {
		return false
	}
	info, err := yookassa.GetPayment(b.config.YookassaShopID, b.config.YookassaSecretKey, paymentID)
	if err != nil {
		b.logger.Warnf("donate yookassa GetPayment id=%d: %v", rec.ID, err)
		return false
	}
	if strings.ToLower(strings.TrimSpace(info.Status)) != "succeeded" || !info.Paid {
		return false
	}
	// Сумма из ответа ЮKassa — источник истины; пустую игнорируем и оставляем свою.
	amountMinor := info.AmountMinor
	currency := info.Currency
	if amountMinor <= 0 {
		amountMinor = int(rec.AmountMinor)
		currency = rec.Currency
	}
	ok, err := b.db.CompleteDonation(rec.ID, rec.UserID, "", paymentID, amountMinor, currency)
	if err != nil {
		b.logger.Errorf("donate complete id=%d: %v", rec.ID, err)
		return false
	}
	if !ok {
		return true // уже закрыт другим вызовом — «спасибо» тоже уже ушло
	}
	b.donateAfterCompleted(rec.ID, rec.UserID, amountMinor, currency, "yukassa")
	return true
}

// handleDonatePreCheckout — подтверждение счёта в звёздах: сверяем заявку и сумму.
func (b *Bot) handleDonatePreCheckout(q *tgbotapi.PreCheckoutQuery) {
	reject := func(reason, userMsg string) {
		var tgID int64
		if q.From != nil {
			tgID = q.From.ID
		}
		b.logger.Warnf("donate pre_checkout reject user=%d reason=%s payload=%q", tgID, reason, q.InvoicePayload)
		_, _ = b.api.Request(tgbotapi.PreCheckoutConfig{PreCheckoutQueryID: q.ID, OK: false, ErrorMessage: userMsg})
	}
	if q.From == nil {
		reject("no_sender", "Донат недоступен.")
		return
	}
	donationID, ok := parseDonatePayload(q.InvoicePayload)
	if !ok {
		reject("invalid_payload", "Некорректный платёж.")
		return
	}
	rec, err := b.db.GetDonationByID(donationID)
	if err != nil || rec == nil {
		reject("not_found", "Заявка не найдена. Попробуй ещё раз из профиля.")
		return
	}
	if rec.UserID != q.From.ID {
		reject("account_mismatch", "Платёж не для этого аккаунта.")
		return
	}
	if rec.Status != "pending" {
		reject("already_completed", "Этот счёт уже оплачен.")
		return
	}
	if q.Currency != "XTR" || int64(q.TotalAmount) != rec.AmountMinor {
		reject("amount_mismatch", "Неверная сумма. Открой профиль и создай донат заново.")
		return
	}
	_, _ = b.api.Request(tgbotapi.PreCheckoutConfig{PreCheckoutQueryID: q.ID, OK: true})
}

// handleDonateSuccessfulPayment — звёзды пришли: закрываем донат и благодарим.
func (b *Bot) handleDonateSuccessfulPayment(msg *tgbotapi.Message) {
	if msg.From == nil || msg.SuccessfulPayment == nil {
		return
	}
	sp := msg.SuccessfulPayment
	donationID, ok := parseDonatePayload(sp.InvoicePayload)
	if !ok {
		return
	}
	rec, err := b.db.GetDonationByID(donationID)
	if err != nil || rec == nil {
		b.logger.Errorf("donate successful_payment load id=%d user=%d: %v", donationID, msg.From.ID, err)
		return
	}
	if rec.UserID != msg.From.ID {
		b.logger.Warnf("donate successful_payment user mismatch id=%d", donationID)
		return
	}
	currency := strings.ToUpper(strings.TrimSpace(sp.Currency))
	done, err := b.db.CompleteDonation(donationID, msg.From.ID, sp.TelegramPaymentChargeID, "", sp.TotalAmount, currency)
	if err != nil {
		b.logger.Errorf("donate complete id=%d: %v", donationID, err)
		return
	}
	if !done {
		// Ретрай Telegram по уже закрытой заявке: второе «спасибо» не шлём.
		b.logger.Infof("donate duplicate successful_payment id=%d user=%d", donationID, msg.From.ID)
		return
	}
	provider := "stars"
	if currency != "XTR" {
		provider = "card"
	}
	b.donateAfterCompleted(donationID, msg.From.ID, sp.TotalAmount, currency, provider)
}

// donateAfterCompleted — общий хвост успешного доната: аналитика + «спасибо» в личку.
func (b *Bot) donateAfterCompleted(donationID, userID int64, amountMinor int, currency, provider string) {
	b.db.TrackEvent(database.AnalyticsEvent{
		Name:           database.EventDonateCompleted,
		UserID:         userID,
		TelegramID:     userID,
		Payload:        map[string]any{"provider": provider, "amount_minor": amountMinor, "currency": currency},
		IdempotencyKey: fmt.Sprintf("donate_completed:%d", donationID),
	})
	b.logger.Infof("donate completed id=%d user=%d %d %s via %s", donationID, userID, amountMinor, currency, provider)

	rec, err := b.db.GetDonationByID(donationID)
	if err == nil && rec != nil && rec.ThanksSentAt.Valid {
		return
	}
	if _, err := b.api.Send(tgbotapi.NewMessage(userID, donateThanksText(amountMinor, currency))); err != nil {
		b.logger.Errorf("donate thanks DM user=%d: %v", userID, err)
		return
	}
	if err := b.db.MarkDonationThanksSent(donationID); err != nil {
		b.logger.Warnf("donate mark thanks id=%d: %v", donationID, err)
	}
}

func donateThanksText(amountMinor int, currency string) string {
	amount := donateAmountHuman(amountMinor, currency)
	if amount != "" {
		amount = " на " + amount
	}
	return fmt.Sprintf(`Рык! Спасибо за поддержку%s 🐆

Ты не купил себе поблажку — вход в стаю бесплатный, и вилку с ножом я не убираю. Ты помог проекту жить: серверам, Лео и новым фичам.

Продолжаем двигаться!`, amount)
}

// donateAmountHuman — «150 ⭐» / «300 ₽» / «10 USD» для текста благодарности.
func donateAmountHuman(amountMinor int, currency string) string {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if amountMinor <= 0 {
		return ""
	}
	switch currency {
	case "XTR":
		return fmt.Sprintf("%d ⭐", amountMinor)
	case "RUB":
		rub := amountMinor / 100
		if kop := amountMinor % 100; kop != 0 {
			return fmt.Sprintf("%d,%02d ₽", rub, kop)
		}
		return fmt.Sprintf("%d ₽", rub)
	case "":
		return ""
	default:
		return fmt.Sprintf("%d %s", amountMinor/100, currency)
	}
}
