package bot

import (
	"errors"
	"testing"

	"leo-bot/internal/i18n"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Каждой типовой ошибке Telegram — своя подсказка, и все они не пустые и разные по смыслу.
func TestPaywallInvoiceShortHintBranches(t *testing.T) {
	cases := []struct {
		err  *tgbotapi.Error
		want string
	}{
		{&tgbotapi.Error{Code: 400, Message: "CURRENCY_TOTAL_AMOUNT_INVALID"}, "Payment didn't pass validation. Tap /start and request an invoice again."},
		{&tgbotapi.Error{Code: 400, Message: "INVOICE_PAYLOAD_INVALID"}, "Telegram rejected the invoice. Update the app or pay by card with another button."},
		{&tgbotapi.Error{Code: 403, Message: "Forbidden"}, "Unblock the bot: ⋮ in chat → Unblock."},
		{&tgbotapi.Error{Code: 400, Message: "Bad Request: chat not found"}, "Send the bot any message in DM and tap the button again."},
		{&tgbotapi.Error{Code: 500, Message: "internal"}, "Couldn't send the invoice. Try card payment with another button or /start later."},
	}
	for _, tc := range cases {
		if got := paywallInvoiceShortHintForLang(i18n.EN, tc.err); got != tc.want {
			t.Fatalf("%q: %q, want %q", tc.err.Message, got, tc.want)
		}
	}
}

func TestPaywallYookassaShortHint(t *testing.T) {
	if paywallYookassaShortHintForLang(i18n.EN, nil) != "" {
		t.Fatal("nil err")
	}
	cases := map[string]string{
		"yookassa: credentials empty":     "YooKassa payments are temporarily unavailable (keys not set). Contact an admin.",
		"amount must be positive":         "YooKassa payments are temporarily unavailable (invalid amount). Contact an admin.",
		"return_url must be http(s)":      "YooKassa payments are temporarily unavailable (invalid return URL). Contact an admin.",
		"yookassa: HTTP 401 unauthorized": "YooKassa rejected the request (check store keys). Try again later.",
		"yookassa: HTTP 403":              "YooKassa rejected the request (check store keys). Try again later.",
		"yookassa: HTTP 400 bad":          "YooKassa returned a payment parameter error. Try again later.",
		"context deadline exceeded":       "YooKassa is slow to respond. Try again in a minute.",
		"dial tcp: i/o timeout":           "YooKassa is slow to respond. Try again in a minute.",
		"something else":                  "Payment link wasn't created. Try again later.",
	}
	for in, want := range cases {
		if got := paywallYookassaShortHintForLang(i18n.EN, errors.New(in)); got != want {
			t.Fatalf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestStarsWordsRU(t *testing.T) {
	words := map[int]string{1: "Star", 2: "Stars", 4: "Stars", 5: "Stars", 11: "Stars", 14: "Stars", 21: "Star", 100: "Stars", 102: "Stars"}
	for n, want := range words {
		if got := starsWordEN(n); got != want {
			t.Fatalf("starsWordEN(%d) = %q, want %q", n, got, want)
		}
	}
	verbs := map[int]string{1: "will be charged", 21: "will be charged", 2: "will be charged", 11: "will be charged", 50: "will be charged"}
	for n, want := range verbs {
		if got := starsChargeVerbEN(n); got != want {
			t.Fatalf("starsChargeVerbEN(%d) = %q, want %q", n, got, want)
		}
	}
}
