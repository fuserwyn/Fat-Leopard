package bot

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"leo-bot/internal/config"
	"leo-bot/internal/yookassa"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func bothMethodsConfig(c *config.Config) {
	starsConfig(c)
	yookassaConfig(c)
}

// fakeYookassaCreate принимает создание платежа и запоминает его тело.
func fakeYookassaCreate(t *testing.T) *struct {
	Amount   struct{ Value, Currency string }
	Metadata map[string]string
} {
	t.Helper()
	created := &struct {
		Amount   struct{ Value, Currency string }
		Metadata map[string]string
	}{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, created)
			_, _ = w.Write([]byte(`{"id":"yk-new","status":"pending","confirmation":{"type":"redirect","confirmation_url":"https://kassa.test/pay-new"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"yk-new","status":"pending","paid":false}`))
	}))
	restore := yookassa.SetAPIBase(srv.URL)
	t.Cleanup(func() {
		restore()
		srv.Close()
	})
	return created
}

// Экран «выбыл из стаи»: текст и кнопки с ценой для каждого способа оплаты.
func TestPaywallUnpaidScreenShowsMethodsWithPrices(t *testing.T) {
	b, tg, db := newIntegrationBot(t, bothMethodsConfig)
	seedMember(t, db, itUser, "leopard", true)

	if err := b.sendPaywallUnpaidPrivateScreen(itUser); err != nil {
		t.Fatal(err)
	}
	msgs := tg.sent("sendMessage")
	if len(msgs) != 2 || !strings.Contains(msgs[0].Form.Get("text"), "Ты выбыл из стаи") || msgs[1].Form.Get("text") != "Способы оплаты:" {
		t.Fatalf("сообщения экрана оплаты: %+v", msgs)
	}
	kb := msgs[1].Form.Get("reply_markup")
	for _, want := range []string{"210 ₽", "350 ⭐", paywallCallbackPayYookassa, paywallCallbackPayStars} {
		if !strings.Contains(kb, want) {
			t.Errorf("в кнопках нет %q: %s", want, kb)
		}
	}

	// Админ сменил цену — на кнопке новая.
	if err := b.db.SetPackPaywallAmountMinor(itPack, 49900, itAdmin); err != nil {
		t.Fatal(err)
	}
	tg.reset()
	_ = b.sendPaywallUnpaidPrivateScreen(itUser)
	if kb := tg.sent("sendMessage")[1].Form.Get("reply_markup"); !strings.Contains(kb, "499 ₽") {
		t.Errorf("цена админа не попала на кнопку: %s", kb)
	}

	// Платный вход — другой текст; оплата не настроена — предупреждение без кнопок.
	b.config.PaywallEntryFree = false
	if text := b.paywallPrivateUnpaidUserText(); !strings.Contains(text, "Платный вход") {
		t.Errorf("текст платного входа: %q", text)
	}
	b.config.PaymentStarsEnabled = false
	b.config.YookassaShopID = ""
	tg.reset()
	_ = b.sendPaywallUnpaidPrivateScreen(itUser)
	if msgs := tg.sent("sendMessage"); len(msgs) != 1 || !strings.Contains(msgs[0].Form.Get("text"), "ещё не настроена") {
		t.Errorf("без способов оплаты — одно сообщение с предупреждением: %+v", msgs)
	}
}

// /start у выбывшего заводит одну заявку на оплату, сколько бы раз он ни жал.
func TestPaywallEnsureCreatesSinglePendingRequest(t *testing.T) {
	b, _, db := newIntegrationBot(t, bothMethodsConfig)
	seedMember(t, db, itUser, "leopard", true)
	fakeYookassaCreate(t)

	b.ensurePaywallInvoiceSent(itUser)
	b.ensurePaywallInvoiceSent(itUser)
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM paywall_access_requests WHERE user_id = $1`, itUser).Scan(&n); err != nil || n != 1 {
		t.Fatalf("заявок: %d %v", n, err)
	}
	first, err := b.paywallGetOrCreatePendingReqID(itUser)
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := b.paywallGetOrCreatePendingReqID(itUser); again != first {
		t.Errorf("та же заявка при повторе: %d и %d", first, again)
	}
	if other, _ := b.paywallGetOrCreatePendingReqID(itUser + 1); other == first || other == 0 {
		t.Errorf("у другого пользователя своя заявка: %d", other)
	}
}

// Отправка способов оплаты: ссылка ЮKassa на сумму из настроек и счёт в звёздах,
// оба привязаны к заявке пользователя.
func TestPaywallSendPaymentOffers(t *testing.T) {
	b, tg, db := newIntegrationBot(t, bothMethodsConfig)
	seedMember(t, db, itUser, "leopard", true)
	created := fakeYookassaCreate(t)
	reqID, _ := b.db.InsertPaywallAccessRequest(itUser, itPack)
	payload := paywallPayloadPrefix + itoa(reqID)

	b.paywallSendPaymentOffers(itUser, reqID)

	if created.Amount.Value != "210.00" || created.Amount.Currency != "RUB" || created.Metadata["invoice_payload"] != payload || created.Metadata["user_telegram_id"] != itoa(itUser) {
		t.Errorf("платёж в кассе: %+v", created)
	}
	var saved string
	if err := db.QueryRow(`SELECT COALESCE(yookassa_payment_id, '') FROM paywall_access_requests WHERE id = $1`, reqID).Scan(&saved); err != nil || saved != "yk-new" {
		t.Errorf("id платежа должен сохраниться в заявке: %q %v", saved, err)
	}
	linkSent := false
	for _, c := range tg.sent("sendMessage") {
		if strings.Contains(c.Form.Get("text")+c.Form.Get("reply_markup"), "https://kassa.test/pay-new") {
			linkSent = true
		}
	}
	if !linkSent {
		t.Error("пользователю должна уйти ссылка на оплату")
	}
	invoices := tg.sent("sendInvoice")
	if len(invoices) != 1 || invoices[0].Form.Get("currency") != "XTR" || invoices[0].Form.Get("payload") != payload || !strings.Contains(invoices[0].Form.Get("prices"), `"amount":350`) || invoices[0].Form.Get("chat_id") != itoa(itUser) {
		t.Errorf("счёт в звёздах: %+v", invoices)
	}
}

// Кнопки выбора способа: звёзды присылают счёт, карта — ссылку кассы.
func TestPaywallMethodCallbacks(t *testing.T) {
	b, tg, db := newIntegrationBot(t, bothMethodsConfig)
	seedMember(t, db, itUser, "leopard", true)
	fakeYookassaCreate(t)
	cb := func(data string) *tgbotapi.CallbackQuery {
		return &tgbotapi.CallbackQuery{
			ID: "cb", Data: data, From: &tgbotapi.User{ID: itUser},
			Message: &tgbotapi.Message{MessageID: 10, Chat: &tgbotapi.Chat{ID: itUser, Type: "private"}},
		}
	}

	b.handlePaywallPayStarsCallback(cb(paywallCallbackPayStars))
	if inv := tg.sent("sendInvoice"); len(inv) != 1 || inv[0].Form.Get("currency") != "XTR" {
		t.Fatalf("после «звёздами» должен уйти счёт: %+v", inv)
	}

	tg.reset()
	b.handlePaywallPayYookassaCallback(cb(paywallCallbackPayYookassa))
	seen := false
	for _, c := range tg.calls {
		if strings.Contains(c.Form.Get("text")+c.Form.Get("reply_markup"), "https://kassa.test/pay-new") {
			seen = true
		}
	}
	if !seen {
		t.Errorf("после «картой» должна уйти ссылка кассы: %+v", tg.calls)
	}

	tg.reset()
	b.handlePaywallBackToMethodsCallback(cb("назад"))
	if len(tg.calls) == 0 {
		t.Error("«назад» должно вернуть выбор способа")
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM paywall_access_requests WHERE user_id = $1`, itUser).Scan(&n); err != nil || n != 1 {
		t.Errorf("все кнопки работают с одной заявкой: %d %v", n, err)
	}
}
