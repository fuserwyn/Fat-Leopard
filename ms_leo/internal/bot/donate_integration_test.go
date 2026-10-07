package bot

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"leo-bot/internal/config"
	"leo-bot/internal/yookassa"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func donateConfig(c *config.Config) {
	yookassaConfig(c)
	c.DonateStarsTiers = []int{50, 150}
	c.DonateCardTiersRub = []int{100, 300}
}

func donationRow(t *testing.T, db *sql.DB, id int64) (status string, amount int, currency string) {
	t.Helper()
	if err := db.QueryRow(`SELECT status, amount_minor, currency FROM donations WHERE id = $1`, id).Scan(&status, &amount, &currency); err != nil {
		t.Fatal(err)
	}
	return
}

func thanksCount(tg *fakeTelegram, userID int64) int {
	n := 0
	for _, text := range tg.textsTo(userID) {
		if strings.Contains(text, "Спасибо за поддержку") {
			n++
		}
	}
	return n
}

// Донат звёздами: сумма только из разрешённых номиналов, счёт проверяется,
// «спасибо» приходит один раз, доступ к стае донат не меняет.
func TestDonateStarsFlow(t *testing.T) {
	b, tg, db := newIntegrationBot(t, donateConfig)
	seedMember(t, db, itUser, "leopard", true)

	if !b.DonateStarsReady() || !b.DonateStarsTierAllowed(150) || b.DonateStarsTierAllowed(151) {
		t.Fatal("номиналы звёзд")
	}
	if _, _, err := b.CreateDonateStarsInvoiceLink(itUser, 151); err == nil {
		t.Fatal("сумма не из номиналов должна отклоняться")
	}
	if _, _, err := b.CreateDonateStarsInvoiceLink(0, 150); err == nil {
		t.Fatal("без пользователя донат не создаётся")
	}

	link, donationID, err := b.CreateDonateStarsInvoiceLink(itUser, 150)
	if err != nil || link != "https://t.me/$invoice-test" || donationID == 0 {
		t.Fatalf("ссылка на счёт: %q %d %v", link, donationID, err)
	}
	invoice := tg.sent("createInvoiceLink")
	if len(invoice) != 1 || invoice[0].Form.Get("currency") != "XTR" || invoice[0].Form.Get("payload") != donatePayload(donationID) || !strings.Contains(invoice[0].Form.Get("prices"), `"amount":150`) {
		t.Fatalf("запрос счёта: %+v", invoice)
	}
	if status, amount, currency := donationRow(t, db, donationID); status != "pending" || amount != 150 || currency != "XTR" {
		t.Errorf("заявка: %s %d %s", status, amount, currency)
	}

	from := &tgbotapi.User{ID: itUser}
	payload := donatePayload(donationID)
	checkout := func(q tgbotapi.PreCheckoutQuery) (bool, string) {
		tg.reset()
		b.handleDonatePreCheckout(&q)
		calls := tg.sent("answerPreCheckoutQuery")
		if len(calls) != 1 {
			t.Fatalf("ответов на pre_checkout: %d", len(calls))
		}
		return calls[0].Form.Get("ok") == "true", calls[0].Form.Get("error_message")
	}
	if ok, _ := checkout(tgbotapi.PreCheckoutQuery{ID: "d1", From: from, Currency: "XTR", TotalAmount: 150, InvoicePayload: payload}); !ok {
		t.Fatal("верный счёт должен проходить")
	}
	for name, q := range map[string]tgbotapi.PreCheckoutQuery{
		"Неверная сумма":        {ID: "d2", From: from, Currency: "XTR", TotalAmount: 50, InvoicePayload: payload},
		"не для этого аккаунта": {ID: "d3", From: &tgbotapi.User{ID: itUser + 3}, Currency: "XTR", TotalAmount: 150, InvoicePayload: payload},
		"Некорректный платёж":   {ID: "d4", From: from, Currency: "XTR", TotalAmount: 150, InvoicePayload: "мусор"},
		"Заявка не найдена":     {ID: "d5", From: from, Currency: "XTR", TotalAmount: 150, InvoicePayload: donatePayload(999999)},
		"Донат недоступен":      {ID: "d6", Currency: "XTR", TotalAmount: 150, InvoicePayload: payload},
	} {
		if ok, msg := checkout(q); ok || !strings.Contains(msg, name) {
			t.Errorf("%s: ok=%v %q", name, ok, msg)
		}
	}

	pay := func() {
		b.handleDonateSuccessfulPayment(&tgbotapi.Message{From: from, SuccessfulPayment: &tgbotapi.SuccessfulPayment{
			Currency: "XTR", TotalAmount: 150, InvoicePayload: payload, TelegramPaymentChargeID: "don-1",
		}})
	}
	tg.reset()
	pay()
	if status, _, _ := donationRow(t, db, donationID); status != "completed" {
		t.Fatalf("донат должен закрыться: %s", status)
	}
	if n := thanksCount(tg, itUser); n != 1 {
		t.Errorf("благодарностей: %d", n)
	}
	pay() // Telegram повторил уведомление
	if n := thanksCount(tg, itUser); n != 1 {
		t.Errorf("после повтора благодарностей: %d", n)
	}
	if ok, msg := checkout(tgbotapi.PreCheckoutQuery{ID: "d7", From: from, Currency: "XTR", TotalAmount: 150, InvoicePayload: payload}); ok || !strings.Contains(msg, "уже оплачен") {
		t.Errorf("оплаченный счёт: ok=%v %q", ok, msg)
	}

	// Донат — не оплата возврата: выбывший остаётся выбывшим.
	if hasAccess(t, b, itUser) || !isKicked(t, db, itUser) || !b.paywallPrivateNeedsPayFirst(itUser) {
		t.Error("донат не должен возвращать в стаю")
	}
	if opts := b.DonateOptionsForUser(itUser); opts.CompletedCount != 1 || !opts.StarsAvailable || !opts.CardAvailable || len(opts.CardTiersRub) != 2 {
		t.Errorf("что показать в профиле: %+v", opts)
	}
}

// Донат картой: платёж создаётся в ЮKassa, статус подтягивается опросом.
func TestDonateCardFlow(t *testing.T) {
	b, tg, db := newIntegrationBot(t, donateConfig)
	seedMember(t, db, itUser, "leopard", false)

	var created struct {
		Amount   struct{ Value, Currency string }
		Metadata map[string]string
	}
	status := "pending"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &created)
			_, _ = w.Write([]byte(`{"id":"don-yk-1","status":"pending","confirmation":{"type":"redirect","confirmation_url":"https://kassa.test/don"}}`))
			return
		}
		_, _ = w.Write([]byte(fmt.Sprintf(`{"id":"don-yk-1","status":"%s","paid":%v,"amount":{"value":"300.00","currency":"RUB"}}`, status, status == "succeeded")))
	}))
	defer srv.Close()
	defer yookassa.SetAPIBase(srv.URL)()

	if !b.DonateCardReady() || !b.DonateCardTierAllowed(300) || b.DonateCardTierAllowed(301) {
		t.Fatal("номиналы в рублях")
	}
	if _, _, err := b.CreateDonateCardPayment(itUser, 301); err == nil {
		t.Fatal("сумма не из номиналов должна отклоняться")
	}
	link, donationID, err := b.CreateDonateCardPayment(itUser, 300)
	if err != nil || link != "https://kassa.test/don" {
		t.Fatalf("ссылка на оплату: %q %v", link, err)
	}
	if created.Amount.Value != "300.00" || created.Metadata["kind"] != "donation" || created.Metadata["invoice_payload"] != donatePayload(donationID) || created.Metadata["user_telegram_id"] != itoa(itUser) {
		t.Errorf("платёж в кассе: %+v", created)
	}

	if _, err := b.DonationStatus(itUser+9, donationID); err == nil {
		t.Error("чужой донат не виден")
	}
	if st, err := b.DonationStatus(itUser, donationID); err != nil || st != "pending" {
		t.Errorf("до оплаты: %q %v", st, err)
	}
	if thanksCount(tg, itUser) != 0 {
		t.Error("до оплаты благодарить рано")
	}

	status = "succeeded"
	if st, err := b.DonationStatus(itUser, donationID); err != nil || st != "completed" {
		t.Fatalf("после оплаты: %q %v", st, err)
	}
	if st, amount, currency := donationRow(t, db, donationID); st != "completed" || amount != 30000 || currency != "RUB" {
		t.Errorf("заявка: %s %d %s", st, amount, currency)
	}
	b.DonateSyncPendingForUser(itUser)
	if st, _ := b.DonationStatus(itUser, donationID); st != "completed" {
		t.Error("повторный опрос статус не меняет")
	}
	if n := thanksCount(tg, itUser); n != 1 {
		t.Errorf("благодарностей: %d", n)
	}
}
