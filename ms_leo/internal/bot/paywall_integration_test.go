package bot

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"leo-bot/internal/config"
	"leo-bot/internal/yookassa"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func starsConfig(c *config.Config) {
	c.PaymentStarsEnabled = true
	c.PaymentStarsAmount = 350
}

func hasAccess(t *testing.T, b *Bot, userID int64) bool {
	t.Helper()
	ok, err := b.db.UserHasActivePaywallAccess(userID, itPack)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func isKicked(t *testing.T, db *sql.DB, userID int64) bool {
	t.Helper()
	var deleted bool
	if err := db.QueryRow(`SELECT is_deleted FROM training_state WHERE user_id = $1 AND chat_id = $2`, userID, itPack).Scan(&deleted); err != nil {
		t.Fatal(err)
	}
	return deleted
}

// Кто платит: при бесплатном входе — только выбывший за неактивность.
func TestPaywallWhoMustPay(t *testing.T) {
	b, _, db := newIntegrationBot(t, starsConfig)
	seedMember(t, db, itUser, "active", false)
	seedMember(t, db, itUser+1, "kicked", true)

	if b.paywallPrivateNeedsPayFirst(itUser) {
		t.Error("активный участник не платит")
	}
	if b.paywallPrivateNeedsPayFirst(itUser + 2) {
		t.Error("новичок при бесплатном входе не платит")
	}
	if !b.paywallPrivateNeedsPayFirst(itUser + 1) {
		t.Error("выбывший платит за возврат")
	}
	if b.paywallPrivateNeedsPayFirst(itAdmin) {
		t.Error("админ не платит никогда")
	}

	b.config.PaywallEntryFree = false
	if !b.paywallPrivateNeedsPayFirst(itUser + 2) {
		t.Error("при платном входе новичок платит")
	}
	b.config.PaywallEnabled = false
	if b.paywallPrivateNeedsPayFirst(itUser + 1) {
		t.Error("оплата выключена — никто не платит")
	}
}

// Возврат в стаю за звёзды: проверка счёта, оплата, выдача доступа — и
// повторное уведомление Telegram не выдаёт доступ и приветствие второй раз.
func TestPaywallStarsReturnFlow(t *testing.T) {
	b, tg, db := newIntegrationBot(t, starsConfig)
	seedMember(t, db, itUser, "leopard", true)
	reqID, err := b.db.InsertPaywallAccessRequest(itUser, itPack)
	if err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf("%s%d", paywallPayloadPrefix, reqID)
	from := &tgbotapi.User{ID: itUser, FirstName: "Лео", UserName: "leopard"}

	checkout := func(q tgbotapi.PreCheckoutQuery) (ok bool, msg string) {
		tg.reset()
		b.handlePaywallPreCheckout(&q)
		calls := tg.sent("answerPreCheckoutQuery")
		if len(calls) != 1 {
			t.Fatalf("ответов на pre_checkout: %d", len(calls))
		}
		return calls[0].Form.Get("ok") == "true", calls[0].Form.Get("error_message")
	}
	if ok, _ := checkout(tgbotapi.PreCheckoutQuery{ID: "q1", From: from, Currency: "XTR", TotalAmount: 350, InvoicePayload: payload}); !ok {
		t.Fatal("верный счёт должен проходить")
	}
	if ok, msg := checkout(tgbotapi.PreCheckoutQuery{ID: "q2", From: from, Currency: "XTR", TotalAmount: 1, InvoicePayload: payload}); ok || !strings.Contains(msg, "Неверная сумма") {
		t.Errorf("чужая сумма: ok=%v %q", ok, msg)
	}
	if ok, msg := checkout(tgbotapi.PreCheckoutQuery{ID: "q3", From: &tgbotapi.User{ID: itUser + 7}, Currency: "XTR", TotalAmount: 350, InvoicePayload: payload}); ok || !strings.Contains(msg, "не для этого аккаунта") {
		t.Errorf("чужой аккаунт: ok=%v %q", ok, msg)
	}
	if ok, msg := checkout(tgbotapi.PreCheckoutQuery{ID: "q4", From: from, Currency: "XTR", TotalAmount: 350, InvoicePayload: "мусор"}); ok || !strings.Contains(msg, "Некорректный платёж") {
		t.Errorf("чужой payload: ok=%v %q", ok, msg)
	}
	if ok, msg := checkout(tgbotapi.PreCheckoutQuery{ID: "q5", From: from, Currency: "XTR", TotalAmount: 350, InvoicePayload: paywallPayloadPrefix + "999999"}); ok || !strings.Contains(msg, "Заявка не найдена") {
		t.Errorf("нет заявки: ok=%v %q", ok, msg)
	}
	if ok, _ := checkout(tgbotapi.PreCheckoutQuery{ID: "q6", Currency: "XTR", TotalAmount: 350, InvoicePayload: payload}); ok {
		t.Error("без отправителя счёт не проходит")
	}

	pay := func(amount int) {
		b.handlePaywallSuccessfulPayment(&tgbotapi.Message{From: from, SuccessfulPayment: &tgbotapi.SuccessfulPayment{
			Currency: "XTR", TotalAmount: amount, InvoicePayload: payload, TelegramPaymentChargeID: "charge-1",
		}})
	}
	tg.reset()
	pay(1)
	if hasAccess(t, b, itUser) {
		t.Fatal("оплата не на ту сумму не должна давать доступ")
	}
	pay(350)
	if !hasAccess(t, b, itUser) {
		t.Fatal("после оплаты должен быть доступ")
	}
	if isKicked(t, db, itUser) {
		t.Error("участник должен вернуться в стаю")
	}
	if b.paywallPrivateNeedsPayFirst(itUser) {
		t.Error("оплативший больше не платит")
	}
	welcome := 0
	for _, text := range tg.textsTo(itUser) {
		if strings.Contains(text, "Ура, ты в стае") {
			welcome++
		}
	}
	if welcome != 1 {
		t.Errorf("приветствий после оплаты: %d", welcome)
	}
	var status, currency string
	var amount int
	if err := db.QueryRow(`SELECT status, total_amount_minor, currency FROM paywall_access_requests WHERE id = $1`, reqID).Scan(&status, &amount, &currency); err != nil {
		t.Fatal(err)
	}
	if status != "completed" || amount != 350 || currency != "XTR" {
		t.Errorf("заявка: %s %d %s", status, amount, currency)
	}
	var feed int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_messages WHERE user_id = $1`, itUser).Scan(&feed); err != nil || feed != 1 {
		t.Errorf("запись о возвращении в ленте: %d %v", feed, err)
	}

	// Telegram прислал то же уведомление ещё раз.
	tg.reset()
	pay(350)
	for _, text := range tg.textsTo(itUser) {
		if strings.Contains(text, "Ура, ты в стае") {
			t.Error("повторное уведомление не должно слать приветствие второй раз")
		}
	}
	// По закрытой заявке счёт больше не принимается.
	if ok, msg := checkout(tgbotapi.PreCheckoutQuery{ID: "q7", From: from, Currency: "XTR", TotalAmount: 350, InvoicePayload: payload}); ok || !strings.Contains(msg, "неактуален") {
		t.Errorf("закрытая заявка: ok=%v %q", ok, msg)
	}
}

func yookassaConfig(c *config.Config) {
	c.YookassaShopID = "shop"
	c.YookassaSecretKey = "secret"
	c.YookassaAmountMinor = 21000
	c.YookassaCurrency = "RUB"
	c.YookassaReturnURL = "https://t.me/leo_it_bot"
}

// fakeYookassa отвечает на GET /payments/{id} тем, что вернёт reply.
func fakeYookassa(t *testing.T, reply func(paymentID string) string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(reply(r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:])))
	}))
	restore := yookassa.SetAPIBase(srv.URL)
	t.Cleanup(func() {
		restore()
		srv.Close()
	})
}

// Вебхук ЮKassa не дошёл: оплату подтягиваем опросом API, и доступ выдаётся
// только когда платёж действительно прошёл и относится к этой заявке.
func TestPaywallYookassaSyncGrantsAccessOnlyForMatchingPaidPayment(t *testing.T) {
	b, tg, db := newIntegrationBot(t, yookassaConfig)
	seedMember(t, db, itUser, "leopard", true)
	reqID, err := b.db.InsertPaywallAccessRequest(itUser, itPack)
	if err != nil {
		t.Fatal(err)
	}

	// Без id платежа опрашивать нечего.
	if b.paywallTrySyncYookassaPayment(itUser) {
		t.Fatal("заявка без id платежа не может быть оплачена")
	}
	if err := b.db.SetPaywallYookassaPaymentID(reqID, "yk-1"); err != nil {
		t.Fatal(err)
	}

	answer := ""
	fakeYookassa(t, func(string) string { return answer })
	paid := func(user int64, payload string) string {
		return fmt.Sprintf(`{"id":"yk-1","status":"succeeded","paid":true,"amount":{"value":"210.00","currency":"RUB"},"metadata":{"user_telegram_id":"%d","invoice_payload":"%s"}}`, user, payload)
	}
	payload := fmt.Sprintf("%s%d", paywallPayloadPrefix, reqID)

	for name, body := range map[string]string{
		"ещё не оплачен":   `{"id":"yk-1","status":"pending","paid":false}`,
		"отменён":          `{"id":"yk-1","status":"canceled","paid":false}`,
		"чужой плательщик": paid(itUser+5, payload),
		"чужая заявка":     paid(itUser, paywallPayloadPrefix+"777"),
	} {
		answer = body
		if b.paywallTrySyncYookassaPayment(itUser) || hasAccess(t, b, itUser) {
			t.Fatalf("%s: доступ выдан зря", name)
		}
	}

	answer = paid(itUser, payload)
	tg.reset()
	if !b.paywallTrySyncYookassaPayment(itUser) {
		t.Fatal("оплаченный платёж должен закрыть заявку")
	}
	if !hasAccess(t, b, itUser) || isKicked(t, db, itUser) {
		t.Fatal("после оплаты — доступ и возврат в стаю")
	}
	var amount int
	var currency, charge string
	if err := db.QueryRow(`SELECT total_amount_minor, currency, COALESCE(yookassa_payment_id, '') FROM paywall_access_requests WHERE id = $1`, reqID).Scan(&amount, &currency, &charge); err != nil {
		t.Fatal(err)
	}
	if amount != 21000 || currency != "RUB" || charge != "yk-1" {
		t.Errorf("заявка: %d %s %q", amount, currency, charge)
	}
	if len(tg.textsTo(itUser)) == 0 {
		t.Error("оплатившему должно прийти сообщение")
	}

	// Повторный опрос ничего не меняет.
	tg.reset()
	if b.paywallTrySyncYookassaPayment(itUser) {
		t.Error("закрытая заявка не закрывается повторно")
	}
}

// Фоновая сверка дожимает оплату без участия пользователя.
func TestPaywallYookassaReconcilerClosesPaidRequests(t *testing.T) {
	b, _, db := newIntegrationBot(t, yookassaConfig)
	seedMember(t, db, itUser, "paid", true)
	seedMember(t, db, itUser+1, "waiting", true)
	paidReq, _ := b.db.InsertPaywallAccessRequest(itUser, itPack)
	waitReq, _ := b.db.InsertPaywallAccessRequest(itUser+1, itPack)
	_ = b.db.SetPaywallYookassaPaymentID(paidReq, "yk-paid")
	_ = b.db.SetPaywallYookassaPaymentID(waitReq, "yk-wait")
	// Сверщик берёт заявки старше порога — состарим обе.
	if _, err := db.Exec(`UPDATE paywall_access_requests SET created_at = created_at - INTERVAL '2 hours'`); err != nil {
		t.Fatal(err)
	}
	fakeYookassa(t, func(id string) string {
		if id == "yk-paid" {
			return fmt.Sprintf(`{"id":"yk-paid","status":"succeeded","paid":true,"amount":{"value":"210.00","currency":"RUB"},"metadata":{"user_telegram_id":"%d","invoice_payload":"%s%d"}}`, itUser, paywallPayloadPrefix, paidReq)
		}
		return `{"id":"yk-wait","status":"pending","paid":false}`
	})

	b.reconcilePaywallYookassaPayments()

	if !hasAccess(t, b, itUser) {
		t.Error("оплаченная заявка должна закрыться сверкой")
	}
	if hasAccess(t, b, itUser+1) {
		t.Error("неоплаченная заявка остаётся ждать")
	}
}
