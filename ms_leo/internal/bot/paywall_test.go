package bot

import (
	"errors"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func TestParsePaywallPayload(t *testing.T) {
	cases := []struct {
		in     string
		wantID int64
		wantOK bool
	}{
		{"pw_1", 1, true},
		{"pw_999", 999, true},
		{"  pw_7  ", 7, true},
		{"pw_", 0, false},
		{"pw_abc", 0, false},
		{"pw_0", 0, false},
		{"other", 0, false},
		{"", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			id, ok := parsePaywallPayload(tc.in)
			if ok != tc.wantOK || id != tc.wantID {
				t.Fatalf("parsePaywallPayload(%q) = (%d, %v), want (%d, %v)", tc.in, id, ok, tc.wantID, tc.wantOK)
			}
		})
	}
}

func TestPaywallInvoiceErrLogAndShortHint(t *testing.T) {
	if paywallInvoiceErrLog(nil) != "" || paywallInvoiceShortHintForUser(nil) != "" {
		t.Fatal("nil err")
	}
	if paywallInvoiceErrLog(errors.New("net")) != "net" {
		t.Fatal("plain error log")
	}
	h := paywallInvoiceShortHintForUser(errors.New("net"))
	if h == "" {
		t.Fatal("expected generic short hint")
	}
	var tgAPI error = &tgbotapi.Error{Code: 400, Message: "Bad Request: PAYMENT_PROVIDER_INVALID"}
	log := paywallInvoiceErrLog(tgAPI)
	if log == "" {
		t.Fatal("expected tg log line")
	}
	sh := paywallInvoiceShortHintForUser(tgAPI)
	if sh == "" || len(sh) > 300 {
		t.Fatalf("short hint: %q", sh)
	}
}

func TestPaywallDecideNeedsPayment(t *testing.T) {
	cases := []struct {
		name            string
		entryFree       bool
		isAdmin         bool
		kicked          bool
		hasActiveAccess bool
		want            bool
	}{
		// Платный вход.
		// kicked+hasActiveAccess = окно после оплаты до ReactivateReturnedUser — не просим платить снова.
		{name: "paid entry: paid but not yet reactivated", kicked: true, hasActiveAccess: true, want: false},
		{name: "paid entry: deleted without active access", kicked: true, want: true},
		{name: "paid entry: active access keeps user in pack", hasActiveAccess: true, want: false},
		{name: "paid entry: no access requires payment", want: true},

		// Бесплатный вход: платит только выбывший за неактивность.
		{name: "free entry: newcomer pays nothing", entryFree: true, want: false},
		{name: "free entry: kicked must pay to return", entryFree: true, kicked: true, want: true},
		{name: "free entry: kicked after payment before reactivate", entryFree: true, kicked: true, hasActiveAccess: true, want: false},

		{name: "admin never pays", entryFree: false, isAdmin: true, kicked: true, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := paywallDecideNeedsPayment(tc.entryFree, tc.isAdmin, tc.kicked, tc.hasActiveAccess)
			if got != tc.want {
				t.Fatalf("paywallDecideNeedsPayment(free=%v, admin=%v, kicked=%v, access=%v) = %v, want %v",
					tc.entryFree, tc.isAdmin, tc.kicked, tc.hasActiveAccess, got, tc.want)
			}
		})
	}
}
