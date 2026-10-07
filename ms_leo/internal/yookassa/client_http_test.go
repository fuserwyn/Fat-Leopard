package yookassa

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeKassa подменяет API ЮKassa локальным сервером на время теста.
func fakeKassa(t *testing.T, h http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(h)
	prev := apiBase
	apiBase = srv.URL
	t.Cleanup(func() {
		apiBase = prev
		srv.Close()
	})
}

func wantBasicAuth(t *testing.T, r *http.Request) {
	t.Helper()
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("shop:secret"))
	if got := r.Header.Get("Authorization"); got != want {
		t.Errorf("авторизация: %q", got)
	}
}

func TestCreatePaymentSendsRublesAndReturnsLink(t *testing.T) {
	var body createPaymentReq
	var idem string
	fakeKassa(t, func(w http.ResponseWriter, r *http.Request) {
		wantBasicAuth(t, r)
		if r.Method != http.MethodPost || r.URL.Path != "/payments" {
			t.Errorf("запрос: %s %s", r.Method, r.URL.Path)
		}
		idem = r.Header.Get("Idempotence-Key")
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("тело: %v", err)
		}
		_, _ = w.Write([]byte(`{"id":"pay-1","status":"pending","confirmation":{"type":"redirect","confirmation_url":"https://kassa.test/pay-1"}}`))
	})

	meta := map[string]string{"user_telegram_id": "42", "invoice_payload": "pw_42"}
	id, link, err := CreatePayment("shop", "secret", 21050, "", "Доступ в стаю", "https://t.me/bot", "  https://leo.test/hook  ", meta)
	if err != nil || id != "pay-1" || link != "https://kassa.test/pay-1" {
		t.Fatalf("ответ: %q %q %v", id, link, err)
	}
	// Копейки уходят рублями с двумя знаками; пустая валюта — рубли.
	if body.Amount.Value != "210.50" || body.Amount.Currency != "RUB" {
		t.Errorf("сумма: %+v", body.Amount)
	}
	if !body.Capture || body.Confirmation.Type != "redirect" || body.Confirmation.ReturnURL != "https://t.me/bot" {
		t.Errorf("подтверждение: %+v capture=%v", body.Confirmation, body.Capture)
	}
	if body.NotificationURL != "https://leo.test/hook" || body.Metadata["invoice_payload"] != "pw_42" || body.Description != "Доступ в стаю" {
		t.Errorf("поля платежа: %+v", body)
	}
	if len(idem) != 32 {
		t.Errorf("ключ идемпотентности: %q", idem)
	}
}

func TestCreatePaymentErrors(t *testing.T) {
	respond := func(code int, payload string) {
		fakeKassa(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(code)
			_, _ = w.Write([]byte(payload))
		})
	}
	call := func() error {
		_, _, err := CreatePayment("shop", "secret", 100, "RUB", "x", "https://t.me", "", nil)
		return err
	}

	respond(http.StatusUnauthorized, `{"type":"error","code":"invalid_credentials","description":"Неверный ключ"}`)
	if err := call(); err == nil || !strings.Contains(err.Error(), "HTTP 401: Неверный ключ") {
		t.Errorf("описание ошибки кассы: %v", err)
	}
	respond(http.StatusBadGateway, `upstream down`)
	if err := call(); err == nil || !strings.Contains(err.Error(), "HTTP 502: upstream down") {
		t.Errorf("сырой ответ: %v", err)
	}
	respond(http.StatusOK, `{"id":"pay-1","confirmation":{}}`)
	if err := call(); err == nil || !strings.Contains(err.Error(), "empty id or confirmation_url") {
		t.Errorf("без ссылки на оплату: %v", err)
	}
	respond(http.StatusOK, `не json`)
	if err := call(); err == nil || !strings.Contains(err.Error(), "decode payment") {
		t.Errorf("битый ответ: %v", err)
	}

	// Касса недоступна вовсе.
	prev := apiBase
	apiBase = "http://127.0.0.1:1"
	defer func() { apiBase = prev }()
	if err := call(); err == nil || !strings.Contains(err.Error(), "yookassa request") {
		t.Errorf("сеть: %v", err)
	}
}

func TestGetPaymentParsesAmountAndMetadata(t *testing.T) {
	payload := `{"id":"pay-1","status":"succeeded","paid":true,"amount":{"value":"210.50","currency":"rub"},"metadata":{"user_telegram_id":" 42 ","n":7,"skip":null}}`
	fakeKassa(t, func(w http.ResponseWriter, r *http.Request) {
		wantBasicAuth(t, r)
		if r.Method != http.MethodGet || r.URL.Path != "/payments/pay-1" {
			t.Errorf("запрос: %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(payload))
	})
	got, err := GetPayment("shop", "secret", " pay-1 ")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "pay-1" || got.Status != "succeeded" || !got.Paid || got.AmountMinor != 21050 || got.Currency != "RUB" {
		t.Errorf("платёж: %+v", got)
	}
	if got.Metadata["user_telegram_id"] != "42" || got.Metadata["n"] != "7" {
		t.Errorf("метаданные: %v", got.Metadata)
	}
	if _, ok := got.Metadata["skip"]; ok {
		t.Error("пустое поле метаданных не должно попадать в ответ")
	}

	// Сумма числом и с запятой — тоже копейки без потери.
	payload = `{"id":"p","status":"pending","amount":{"value":99.99,"currency":"RUB"}}`
	if got, err = GetPayment("shop", "secret", "pay-1"); err != nil || got.AmountMinor != 9999 || got.Paid {
		t.Errorf("сумма числом: %+v %v", got, err)
	}
	payload = `{"id":"p","status":"pending","amount":{"value":"0,29","currency":"RUB"}}`
	if got, err = GetPayment("shop", "secret", "pay-1"); err != nil || got.AmountMinor != 29 {
		t.Errorf("сумма с запятой: %+v %v", got, err)
	}
}

func TestGetPaymentErrors(t *testing.T) {
	if _, err := GetPayment("shop", "secret", "  "); err == nil {
		t.Error("пустой id платежа должен отклоняться")
	}
	code, payload := http.StatusNotFound, `{"description":"Платёж не найден"}`
	fakeKassa(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(code)
		_, _ = w.Write([]byte(payload))
	})
	if _, err := GetPayment("shop", "secret", "x"); err == nil || !strings.Contains(err.Error(), "HTTP 404: Платёж не найден") {
		t.Errorf("описание ошибки: %v", err)
	}
	code, payload = http.StatusInternalServerError, `boom`
	if _, err := GetPayment("shop", "secret", "x"); err == nil || !strings.Contains(err.Error(), "HTTP 500: boom") {
		t.Errorf("сырой ответ: %v", err)
	}
	code, payload = http.StatusOK, `[]`
	if _, err := GetPayment("shop", "secret", "x"); err == nil || !strings.Contains(err.Error(), "decode get payment") {
		t.Errorf("битый ответ: %v", err)
	}
}

func TestRefundPayment(t *testing.T) {
	var body createRefundReq
	code, payload := http.StatusOK, `{"id":"ref-1","status":"succeeded"}`
	fakeKassa(t, func(w http.ResponseWriter, r *http.Request) {
		wantBasicAuth(t, r)
		if r.Method != http.MethodPost || r.URL.Path != "/refunds" || len(r.Header.Get("Idempotence-Key")) != 32 {
			t.Errorf("запрос: %s %s", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.WriteHeader(code)
		_, _ = w.Write([]byte(payload))
	})
	if err := RefundPayment("shop", "secret", " pay-1 ", 9900, " rub "); err != nil {
		t.Fatal(err)
	}
	if body.PaymentID != "pay-1" || body.Amount.Value != "99.00" || body.Amount.Currency != "RUB" {
		t.Errorf("возврат: %+v", body)
	}
	if err := RefundPayment("shop", "secret", "pay-1", 100, ""); err != nil || body.Amount.Currency != "RUB" {
		t.Errorf("валюта по умолчанию: %+v %v", body, err)
	}

	code, payload = http.StatusBadRequest, `{"description":"Платёж уже возвращён"}`
	if err := RefundPayment("shop", "secret", "pay-1", 100, "RUB"); err == nil || !strings.Contains(err.Error(), "HTTP 400: Платёж уже возвращён") {
		t.Errorf("описание ошибки: %v", err)
	}
	code, payload = http.StatusServiceUnavailable, `later`
	if err := RefundPayment("shop", "secret", "pay-1", 100, "RUB"); err == nil || !strings.Contains(err.Error(), "HTTP 503: later") {
		t.Errorf("сырой ответ: %v", err)
	}
}
