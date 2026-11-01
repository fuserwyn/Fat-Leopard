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

// Ежемесячный счёт за сервер одному плательщику (SERVER_PAYMENT_USER_ID): 1-го числа (МСК),
// сумма фиксированная — SERVER_PAYMENT_RUB или SERVER_PAYMENT_STARS. Оплата картой через ЮKassa
// или звёздами; ссылка ЮKassa быстро протухает, поэтому платёж создаём по нажатию.
//
// SERVER_PAYMENT_SUSPEND_ENABLED=true: счёт не оплачен за сутки — бот в Telegram на паузе и
// автоматические кики не идут. Оплата работает и в паузе и снимает её сразу. Мини-апп и отчёты
// о тренировках в нём не трогаем, иначе участники теряли бы стрики из-за чужого счёта.

const (
	serverPaymentDay  = 1
	serverPaymentHour = 15 // с 15:00 МСК
	serverPaymentTick = 5 * time.Minute

	serverPaymentSuspendGrace = 24 * time.Hour

	serverPaymentPayloadPrefix      = "srv_"
	serverPaymentCardCallbackPrefix = "server_pay_card:"
	serverPaymentTestMonthPrefix    = "test-"
)

// Внешние вызовы вынесены в переменные, чтобы подменять их в тестах.
var (
	serverPaymentCreateCard = yookassa.CreatePayment
	serverPaymentGetPayment = yookassa.GetPayment
)

// serverPaymentDue — пора ли выставлять счёт (время МСК). Позже 1-го тоже пора: если бот лежал 1-го, счёт придёт после подъёма.
func serverPaymentDue(now time.Time) bool {
	if now.Day() > serverPaymentDay {
		return true
	}
	return now.Day() == serverPaymentDay && now.Hour() >= serverPaymentHour
}

func serverPaymentPeriodLabel(month time.Time) string {
	names := []string{"январь", "февраль", "март", "апрель", "май", "июнь",
		"июль", "август", "сентябрь", "октябрь", "ноябрь", "декабрь"}
	return fmt.Sprintf("%s %d", names[month.Month()-1], month.Year())
}

func serverPaymentBillText(period string, stars, rub int) string {
	var amounts []string
	if rub > 0 {
		amounts = append(amounts, fmt.Sprintf("%d ₽", rub))
	}
	if stars > 0 {
		amounts = append(amounts, fmt.Sprintf("%d ⭐", stars))
	}
	return fmt.Sprintf("💳 Счёт за сервер Fat Leopard за %s: %s.", period, strings.Join(amounts, " или "))
}

func serverPaymentBillMessage(userID int64, text, starsLink string, stars int, cardCallback string, rub int) tgbotapi.MessageConfig {
	m := tgbotapi.NewMessage(userID, text)
	var row []tgbotapi.InlineKeyboardButton
	if starsLink != "" {
		row = append(row, tgbotapi.NewInlineKeyboardButtonURL(fmt.Sprintf("Оплатить %d ⭐", stars), starsLink))
	}
	if cardCallback != "" {
		row = append(row, tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("Картой %d ₽", rub), cardCallback))
	}
	if len(row) > 0 {
		m.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(row)
	}
	return m
}

// serverPaymentStarsLink — createInvoiceLink в звёздах; tgbotapi этой версии такого метода не знает.
func (b *Bot) serverPaymentStarsLink(month, period string, stars int) (string, error) {
	prices, err := json.Marshal([]tgbotapi.LabeledPrice{{Label: "Сервер Fat Leopard", Amount: stars}})
	if err != nil {
		return "", err
	}
	resp, err := b.api.MakeRequest("createInvoiceLink", tgbotapi.Params{
		"title":       "Оплата сервера Fat Leopard",
		"description": "Оплата ресурсов сервера за " + period,
		"payload":     serverPaymentPayloadPrefix + month,
		"currency":    "XTR",
		"prices":      string(prices),
	})
	if err != nil {
		return "", err
	}
	var link string
	if err := json.Unmarshal(resp.Result, &link); err != nil {
		return "", err
	}
	return strings.TrimSpace(link), nil
}

func (b *Bot) serverPaymentCardReady() bool {
	return b.config.YookassaShopID != "" && b.config.YookassaSecretKey != "" && b.config.ServerPaymentRub > 0
}

// runServerPaymentBilling выставляет счёт за месяц, если пора и его ещё нет. true — счёт отправлен сейчас.
func (b *Bot) runServerPaymentBilling(now time.Time) bool {
	if b == nil || b.db == nil || b.api == nil || b.config == nil || b.config.ServerPaymentUserID == 0 {
		return false
	}
	now = now.In(moscowLocation())
	if !serverPaymentDue(now) {
		return false
	}
	month := now.Format("2006-01")
	existing, err := b.db.GetServerPaymentBill(month)
	if err != nil {
		b.logger.Errorf("server payment %s: %v", month, err)
		return false
	}
	if existing != nil {
		return false
	}
	return b.issueServerPaymentBill(month, serverPaymentPeriodLabel(now), b.config.ServerPaymentUserID,
		b.config.ServerPaymentStars, b.config.ServerPaymentRub, b.config.ServerPaymentSuspendEnabled, now)
}

// handleTestServerBill — /test_server_bill: владелец проверяет оплату настоящими деньгами на небольшой сумме.
// Месяц с префиксом test- не считается просрочкой и не включает паузу.
func (b *Bot) handleTestServerBill(msg *tgbotapi.Message) {
	if msg.From == nil || msg.From.ID != b.config.OwnerID {
		return
	}
	now := time.Now().In(moscowLocation())
	month := serverPaymentTestMonthPrefix + now.Format("20060102-150405")
	if !b.issueServerPaymentBill(month, "тест", msg.From.ID, 1, 10, false, now) {
		if _, err := b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "Не удалось выставить тестовый счёт, подробности в логах.")); err != nil {
			b.logger.Errorf("server payment %s: %v", month, err)
		}
	}
}

// issueServerPaymentBill создаёт счёт и отправляет его; stars или rub = 0 отключает способ оплаты.
func (b *Bot) issueServerPaymentBill(month, period string, userID int64, stars, rub int, warnSuspend bool, now time.Time) bool {
	var err error
	link := ""
	if stars > 0 {
		if link, err = b.serverPaymentStarsLink(month, period, stars); err != nil || link == "" {
			b.logger.Errorf("server payment %s stars link: %v", month, err)
			stars, link = 0, ""
		}
	}
	cardCallback := ""
	if b.serverPaymentCardReady() && rub > 0 {
		cardCallback = serverPaymentCardCallbackPrefix + month
	} else {
		rub = 0
	}
	if stars == 0 && rub == 0 {
		b.logger.Errorf("server payment %s: нет ни одного способа оплаты", month)
		return false
	}

	inserted, err := b.db.InsertServerPaymentBill(database.ServerPaymentBill{
		Month: month, UserID: userID, Stars: stars, Rub: rub, SentAt: now,
	})
	if err != nil || !inserted {
		if err != nil {
			b.logger.Errorf("server payment %s: %v", month, err)
		}
		return false
	}
	text := serverPaymentBillText(period, stars, rub)
	if warnSuspend {
		text += "\nОплатить нужно в течение суток, иначе бот встанет на паузу."
	}
	if _, err := b.api.Send(serverPaymentBillMessage(userID, text, link, stars, cardCallback, rub)); err != nil {
		b.logger.Errorf("server payment %s send to %d: %v", month, userID, err)
		if err := b.db.DeleteServerPaymentBill(month); err != nil {
			b.logger.Errorf("server payment %s delete: %v", month, err)
		}
		return false
	}
	b.logger.Infof("server payment %s: bill sent to %d", month, userID)
	return true
}

func (b *Bot) startServerPaymentScheduler(ctx context.Context) {
	// Рестарт во время паузы не должен открывать окно, когда бот снова всё обрабатывает.
	b.refreshServerPaymentSuspension(time.Now())
	ticker := time.NewTicker(serverPaymentTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.runServerPaymentBilling(time.Now())
			b.syncServerPaymentCardPayments()
			b.refreshServerPaymentSuspension(time.Now())
		}
	}
}

// isServerPaymentPayload — payload счёта за сервер, а не заявки paywall (pw_<id>).
func isServerPaymentPayload(payload string) bool {
	return strings.HasPrefix(payload, serverPaymentPayloadPrefix)
}

// handleServerPaymentPreCheckout — подтверждение счёта в звёздах: сверяем плательщика и сумму.
func (b *Bot) handleServerPaymentPreCheckout(q *tgbotapi.PreCheckoutQuery) {
	reject := func(reason, text string) {
		b.logger.Warnf("server payment pre_checkout reject: %s payload=%q", reason, q.InvoicePayload)
		_, _ = b.api.Request(tgbotapi.PreCheckoutConfig{PreCheckoutQueryID: q.ID, OK: false, ErrorMessage: text})
	}
	bill, err := b.db.GetServerPaymentBill(strings.TrimPrefix(q.InvoicePayload, serverPaymentPayloadPrefix))
	switch {
	case err != nil || bill == nil:
		reject("not_found", "Счёт не найден.")
	case q.From == nil || q.From.ID != bill.UserID:
		reject("account_mismatch", "Этот счёт выставлен другому аккаунту.")
	case bill.PaidAt.Valid:
		reject("already_paid", "Этот счёт уже оплачен.")
	case q.Currency != "XTR" || q.TotalAmount != bill.Stars:
		reject("amount_mismatch", "Неверная сумма счёта.")
	default:
		_, _ = b.api.Request(tgbotapi.PreCheckoutConfig{PreCheckoutQueryID: q.ID, OK: true})
	}
}

// handleServerPaymentSuccessfulPayment — звёзды пришли: закрываем счёт.
func (b *Bot) handleServerPaymentSuccessfulPayment(msg *tgbotapi.Message) {
	sp := msg.SuccessfulPayment
	month := strings.TrimPrefix(sp.InvoicePayload, serverPaymentPayloadPrefix)
	b.completeServerPaymentBill(month, msg.From.ID, "stars", sp.TelegramPaymentChargeID, fmt.Sprintf("%d ⭐", sp.TotalAmount))
}

func (b *Bot) completeServerPaymentBill(month string, userID int64, via, chargeID, amount string) {
	done, err := b.db.MarkServerPaymentBillPaid(month, via, chargeID, time.Now())
	if err != nil {
		b.logger.Errorf("server payment %s paid: %v", month, err)
		return
	}
	if !done {
		return
	}
	b.logger.Infof("server payment %s paid via %s: %s", month, via, amount)
	if _, err := b.api.Send(tgbotapi.NewMessage(userID, fmt.Sprintf("Счёт за сервер оплачен: %s. Спасибо! 🐆", amount))); err != nil {
		b.logger.Errorf("server payment %s thanks: %v", month, err)
	}
	b.refreshServerPaymentSuspension(time.Now())
}

// serverPaymentSyncCard опрашивает платёж ЮKassa: "succeeded" — счёт закрыт, вернёт статус платежа.
func (b *Bot) serverPaymentSyncCard(bill *database.ServerPaymentBill) (string, error) {
	info, err := serverPaymentGetPayment(b.config.YookassaShopID, b.config.YookassaSecretKey, bill.YookassaPaymentID.String)
	if err != nil {
		return "", err
	}
	status := strings.ToLower(strings.TrimSpace(info.Status))
	if status == "succeeded" && info.Paid {
		b.completeServerPaymentBill(bill.Month, bill.UserID, "yookassa", "", fmt.Sprintf("%d ₽", bill.Rub))
	}
	return status, nil
}

// syncServerPaymentCardPayments закрывает оплаченные картой счета: вебхук ms_payments обслуживает только pw_-платежи.
func (b *Bot) syncServerPaymentCardPayments() {
	if b == nil || b.db == nil || b.config == nil || !b.serverPaymentCardReady() {
		return
	}
	bills, err := b.db.UnpaidServerPaymentBillsWithCard()
	if err != nil {
		b.logger.Warnf("server payment card sync: %v", err)
		return
	}
	for _, bill := range bills {
		if _, err := b.serverPaymentSyncCard(bill); err != nil {
			b.logger.Warnf("server payment card sync %s: %v", bill.Month, err)
		}
	}
}

// handleServerPaymentCardCallback — кнопка «Картой»: присылает ссылку на платёж ЮKassa.
// Живой платёж переиспользуем, чтобы повторное нажатие не привело к двойной оплате.
func (b *Bot) handleServerPaymentCardCallback(callback *tgbotapi.CallbackQuery) {
	alert := func(text string) {
		_, _ = b.api.Request(tgbotapi.NewCallbackWithAlert(callback.ID, text))
	}
	month := strings.TrimPrefix(callback.Data, serverPaymentCardCallbackPrefix)
	if callback.From != nil {
		b.logger.Infof("server payment card %s: pressed by %d", month, callback.From.ID)
	}
	bill, err := b.db.GetServerPaymentBill(month)
	if err != nil {
		b.logger.Errorf("server payment card %s: %v", month, err)
		alert("Не удалось открыть оплату. Попробуй позже.")
		return
	}
	if bill == nil || callback.From == nil || bill.UserID != callback.From.ID || bill.Rub <= 0 {
		alert("Этот счёт недоступен.")
		return
	}
	if bill.PaidAt.Valid {
		alert("Этот счёт уже оплачен.")
		return
	}

	// Запрос в ЮKassa идёт до 25 секунд: отвечаем сразу, а дальнейшие сбои сообщаем уже сообщением.
	_, _ = b.api.Request(tgbotapi.NewCallback(callback.ID, "Готовлю ссылку на оплату…"))
	fail := func() {
		if _, err := b.api.Send(tgbotapi.NewMessage(bill.UserID, "Не удалось открыть оплату. Попробуй позже.")); err != nil {
			b.logger.Errorf("server payment card %s fail notice: %v", month, err)
		}
	}

	confirmURL := ""
	if bill.YookassaPaymentID.Valid {
		status, err := b.serverPaymentSyncCard(bill)
		if err != nil {
			b.logger.Errorf("server payment card %s status: %v", month, err)
			fail()
			return
		}
		switch status {
		case "succeeded":
			return // serverPaymentSyncCard уже закрыл счёт и поблагодарил
		case "pending", "waiting_for_capture":
			confirmURL = bill.CardConfirmURL.String
		}
	}
	if confirmURL == "" {
		returnURL := strings.TrimSpace(b.config.YookassaReturnURL)
		if returnURL == "" {
			returnURL = "https://t.me"
		}
		paymentID, url, err := serverPaymentCreateCard(b.config.YookassaShopID, b.config.YookassaSecretKey, bill.Rub*100, "RUB",
			fmt.Sprintf("Оплата сервера Fat Leopard, счёт %s — %d ₽", month, bill.Rub), returnURL, "",
			map[string]string{"kind": "server_payment", "month": month})
		if err == nil {
			err = b.db.SetServerPaymentBillCardPayment(month, paymentID, url)
		}
		if err != nil {
			b.logger.Errorf("server payment card %s: %v", month, err)
			fail()
			return
		}
		confirmURL = url
	}

	m := tgbotapi.NewMessage(bill.UserID, fmt.Sprintf("Оплата счёта за сервер: %d ₽", bill.Rub))
	m.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonURL("Перейти к оплате", confirmURL),
	))
	if _, err := b.api.Send(m); err != nil {
		b.logger.Errorf("server payment card %s send: %v", month, err)
		fail()
		return
	}
	b.logger.Infof("server payment card %s: payment link sent", month)
}

// dropUpdateWhileSuspended — true, если бот на паузе и апдейт обрабатывать не надо.
// Всё, что нужно для оплаты счёта, проходит.
func (b *Bot) dropUpdateWhileSuspended(update tgbotapi.Update) bool {
	if !b.serverPaymentSuspended.Load() {
		return false
	}
	const notice = "Бот на паузе: не оплачен счёт за сервер. Мини-апп работает."
	switch {
	case update.PreCheckoutQuery != nil:
		return !isServerPaymentPayload(update.PreCheckoutQuery.InvoicePayload) && !strings.HasPrefix(update.PreCheckoutQuery.InvoicePayload, paywallPayloadPrefix)
	case update.CallbackQuery != nil:
		if strings.HasPrefix(update.CallbackQuery.Data, serverPaymentCardCallbackPrefix) {
			return false
		}
		_, _ = b.api.Request(tgbotapi.NewCallbackWithAlert(update.CallbackQuery.ID, notice))
	case update.Message != nil && update.Message.SuccessfulPayment != nil:
		return false
	case update.Message != nil && update.Message.Chat != nil && update.Message.Chat.IsPrivate():
		if _, err := b.api.Send(tgbotapi.NewMessage(update.Message.Chat.ID, notice)); err != nil {
			b.logger.Warnf("server payment suspend notice to %d: %v", update.Message.Chat.ID, err)
		}
	}
	return true
}

// refreshServerPaymentSuspension сверяет паузу со счетами. Ошибка базы состояние не меняет:
// сбой не должен ни отключать бота, ни снимать паузу.
func (b *Bot) refreshServerPaymentSuspension(now time.Time) {
	if b == nil || b.db == nil || b.config == nil || !b.config.ServerPaymentSuspendEnabled {
		return
	}
	bill, err := b.db.OldestOverdueServerPaymentBill(now.Add(-serverPaymentSuspendGrace))
	if err != nil {
		b.logger.Errorf("server payment suspension: %v", err)
		return
	}
	was := b.serverPaymentSuspended.Swap(bill != nil)
	if bill == nil {
		if was {
			b.logger.Infof("bot resumed: server bills paid")
			b.notifyServerPaymentOwner(bill, "✅ Счёт за сервер оплачен — бот снова работает.")
		}
		return
	}
	if !was {
		b.logger.Warnf("bot suspended: server bill %s unpaid by %d", bill.Month, bill.UserID)
	}
	claimed, err := b.db.ClaimServerPaymentSuspendNotice(bill.Month)
	if err != nil {
		b.logger.Errorf("server payment suspension: %v", err)
	}
	if !claimed {
		return
	}
	if _, err := b.api.Send(tgbotapi.NewMessage(bill.UserID,
		"⛔️ Счёт за сервер не оплачен за сутки — бот на паузе. Оплати кнопкой в сообщении со счётом, и он сразу заработает.")); err != nil {
		b.logger.Errorf("server payment suspension notice to %d: %v", bill.UserID, err)
	}
	b.notifyServerPaymentOwner(bill, fmt.Sprintf("⛔️ Бот на паузе: счёт за сервер %s не оплачен за сутки (плательщик %d).", bill.Month, bill.UserID))
}

func (b *Bot) notifyServerPaymentOwner(bill *database.ServerPaymentBill, text string) {
	if b.config.OwnerID == 0 || (bill != nil && b.config.OwnerID == bill.UserID) {
		return
	}
	if _, err := b.api.Send(tgbotapi.NewMessage(b.config.OwnerID, text)); err != nil {
		b.logger.Errorf("server payment notice to owner: %v", err)
	}
}

// removeUserForInactivity — автоматический кик. Пауза за неоплаченный сервер не вина участников:
// кик откладывается, его подберёт watchdog после оплаты. Кик из админки идёт мимо этой проверки.
func (b *Bot) removeUserForInactivity(userID, chatID int64, username string) {
	if b.serverPaymentSuspended.Load() {
		b.logger.Infof("inactivity kick user %d postponed: bot suspended", userID)
		return
	}
	b.removeUser(userID, chatID, username)
}
