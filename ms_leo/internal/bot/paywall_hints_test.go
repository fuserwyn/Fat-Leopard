package bot

import (
	"errors"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Каждой типовой ошибке Telegram — своя подсказка, и все они не пустые и разные по смыслу.
func TestPaywallInvoiceShortHintBranches(t *testing.T) {
	cases := []struct {
		err  *tgbotapi.Error
		want string
	}{
		{&tgbotapi.Error{Code: 400, Message: "CURRENCY_TOTAL_AMOUNT_INVALID"}, "Платёж не прошёл проверку. Нажми /start и запроси счёт снова."},
		{&tgbotapi.Error{Code: 400, Message: "INVOICE_PAYLOAD_INVALID"}, "Счёт отклонён Telegram. Обнови приложение или выбери оплату картой другой кнопкой."},
		{&tgbotapi.Error{Code: 403, Message: "Forbidden"}, "Разблокируй бота: ⋮ в чате → Разблокировать."},
		{&tgbotapi.Error{Code: 400, Message: "Bad Request: chat not found"}, "Напиши боту любое сообщение в личке и снова нажми кнопку."},
		{&tgbotapi.Error{Code: 500, Message: "internal"}, "Не вышло отправить счёт. Попробуй оплату картой другой кнопкой или /start позже."},
	}
	for _, tc := range cases {
		if got := paywallInvoiceShortHintForUser(tc.err); got != tc.want {
			t.Fatalf("%q: %q, want %q", tc.err.Message, got, tc.want)
		}
	}
}

func TestPaywallYookassaShortHint(t *testing.T) {
	if paywallYookassaShortHintForUser(nil) != "" {
		t.Fatal("nil err")
	}
	cases := map[string]string{
		"yookassa: credentials empty":     "Оплата ЮKassa временно недоступна (не заданы ключи). Напиши администратору.",
		"amount must be positive":         "Оплата ЮKassa временно недоступна (некорректная сумма). Напиши администратору.",
		"return_url must be http(s)":      "Оплата ЮKassa временно недоступна (некорректный URL возврата). Напиши администратору.",
		"yookassa: HTTP 401 unauthorized": "ЮKassa отклонила запрос (проверь ключи магазина). Попробуй позже.",
		"yookassa: HTTP 403":              "ЮKassa отклонила запрос (проверь ключи магазина). Попробуй позже.",
		"yookassa: HTTP 400 bad":          "ЮKassa вернула ошибку параметров платежа. Попробуй позже.",
		"context deadline exceeded":       "ЮKassa долго отвечает. Попробуй ещё раз через минуту.",
		"dial tcp: i/o timeout":           "ЮKassa долго отвечает. Попробуй ещё раз через минуту.",
		"something else":                  "Ссылка на оплату не создалась. Попробуй позже.",
	}
	for in, want := range cases {
		if got := paywallYookassaShortHintForUser(errors.New(in)); got != want {
			t.Fatalf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestStarsWordsRU(t *testing.T) {
	words := map[int]string{1: "звезда", 2: "звезды", 4: "звезды", 5: "звёзд", 11: "звёзд", 14: "звёзд", 21: "звезда", 100: "звёзд", 102: "звезды"}
	for n, want := range words {
		if got := starsWordRU(n); got != want {
			t.Fatalf("starsWordRU(%d) = %q, want %q", n, got, want)
		}
	}
	verbs := map[int]string{1: "спишется", 21: "спишется", 2: "спишутся", 11: "спишутся", 50: "спишутся"}
	for n, want := range verbs {
		if got := starsChargeVerbRU(n); got != want {
			t.Fatalf("starsChargeVerbRU(%d) = %q, want %q", n, got, want)
		}
	}
}
