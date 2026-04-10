package config

import (
	"os"
	"testing"
)

func TestPaymentAmountMinorFromEnv_XTR(t *testing.T) {
	cases := []struct {
		name   string
		env    map[string]string
		want   int
		cur    string
	}{
		{
			name: "stars env wins",
			env: map[string]string{
				"PAYMENT_AMOUNT_STARS":       "42",
				"PAYMENT_AMOUNT_MINOR_UNITS": "999",
			},
			want: 42,
			cur:  "XTR",
		},
		{
			name: "minor units when stars empty",
			env: map[string]string{
				"PAYMENT_AMOUNT_STARS":       "",
				"PAYMENT_AMOUNT_MINOR_UNITS": "15",
			},
			want: 15,
			cur:  "XTR",
		},
		{
			name: "default 100 when both empty",
			env: map[string]string{
				"PAYMENT_AMOUNT_STARS":       "",
				"PAYMENT_AMOUNT_MINOR_UNITS": "",
			},
			want: 100,
			cur:  "XTR",
		},
		{
			name: "invalid stars falls through to minor",
			env: map[string]string{
				"PAYMENT_AMOUNT_STARS":       "x",
				"PAYMENT_AMOUNT_MINOR_UNITS": "20",
			},
			want: 20,
			cur:  "XTR",
		},
		{
			name: "invalid stars and no minor -> default",
			env: map[string]string{
				"PAYMENT_AMOUNT_STARS":       "0",
				"PAYMENT_AMOUNT_MINOR_UNITS": "",
			},
			want: 100,
			cur:  "XTR",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			got := paymentAmountMinorFromEnv(tc.cur)
			if got != tc.want {
				t.Fatalf("paymentAmountMinorFromEnv(%q) = %d, want %d", tc.cur, got, tc.want)
			}
		})
	}
}

func TestPaymentAmountMinorFromEnv_RUB(t *testing.T) {
	t.Setenv("PAYMENT_AMOUNT_RUB", "12.50")
	t.Setenv("PAYMENT_AMOUNT_MINOR_UNITS", "1")
	got := paymentAmountMinorFromEnv("RUB")
	if got != 1250 {
		t.Fatalf("rub float: got %d want 1250", got)
	}
}

func TestPaymentStarsAddonAmountFromEnv(t *testing.T) {
	t.Setenv("PAYMENT_STARS_AMOUNT", "7")
	if paymentStarsAddonAmountFromEnv(false) != 0 {
		t.Fatal("disabled must ignore PAYMENT_STARS_AMOUNT")
	}
	if paymentStarsAddonAmountFromEnv(true) != 7 {
		t.Fatalf("got %d", paymentStarsAddonAmountFromEnv(true))
	}
	t.Setenv("PAYMENT_STARS_AMOUNT", "")
	t.Setenv("PAYMENT_AMOUNT_STARS", "9")
	if paymentStarsAddonAmountFromEnv(true) != 9 {
		t.Fatalf("fallback PAYMENT_AMOUNT_STARS: got %d", paymentStarsAddonAmountFromEnv(true))
	}
}

func TestPaymentAmountMinorFromEnv_RUB_defaultMinor(t *testing.T) {
	t.Setenv("PAYMENT_AMOUNT_RUB", "")
	t.Setenv("PAYMENT_AMOUNT_MINOR_UNITS", "")
	// getEnv default 10000
	got := paymentAmountMinorFromEnv("RUB")
	if got != 10000 {
		t.Fatalf("default minor: got %d want 10000", got)
	}
}

func TestConfig_PaywallUsesStars(t *testing.T) {
	if !(&Config{PaymentCurrency: "XTR", PaymentAmountMinorUnits: 1}).PaywallUsesStars() {
		t.Fatal("XTR with amount should use stars")
	}
	if (&Config{PaymentCurrency: "XTR", PaymentAmountMinorUnits: 0}).PaywallUsesStars() {
		t.Fatal("XTR zero amount should not use stars")
	}
	if (&Config{PaymentCurrency: "RUB"}).PaywallUsesStars() {
		t.Fatal("RUB without addon should not use stars")
	}
	if !(&Config{PaymentCurrency: "RUB", PaymentStarsEnabled: true, PaymentStarsAmount: 5}).PaywallUsesStars() {
		t.Fatal("RUB + PAYMENT_STARS addon should use stars")
	}
}

func TestConfig_PaywallPaymentReady(t *testing.T) {
	cases := []struct {
		name string
		c    Config
		want bool
	}{
		{"xtr amount", Config{PaymentCurrency: "XTR", PaymentAmountMinorUnits: 1}, true},
		{"xtr zero amount", Config{PaymentCurrency: "XTR", PaymentAmountMinorUnits: 0}, false},
		{"token", Config{PaymentCurrency: "RUB", PaymentProviderToken: "tok"}, true},
		{"yookassa", Config{PaymentCurrency: "RUB", PaymentAmountMinorUnits: 100, YookassaShopID: "s", YookassaSecretKey: "k", YookassaAmountMinor: 100, YookassaCurrency: "RUB"}, true},
		{"stars addon", Config{PaymentCurrency: "RUB", PaymentStarsEnabled: true, PaymentStarsAmount: 10}, true},
		{"nothing", Config{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if g := tc.c.PaywallPaymentReady(); g != tc.want {
				t.Fatalf("PaywallPaymentReady() = %v, want %v", g, tc.want)
			}
		})
	}
}

func TestConfig_PaywallUsesTelegramInvoice(t *testing.T) {
	cases := []struct {
		name string
		c    Config
		want bool
	}{
		{"xtr no token", Config{PaymentCurrency: "XTR", PaymentAmountMinorUnits: 1}, true},
		{"rub stars addon", Config{PaymentCurrency: "RUB", PaymentStarsEnabled: true, PaymentStarsAmount: 1}, true},
		{"rub token", Config{PaymentCurrency: "RUB", PaymentProviderToken: "t"}, true},
		{"rub no token no addon", Config{PaymentCurrency: "RUB"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if g := tc.c.PaywallUsesTelegramInvoice(); g != tc.want {
				t.Fatalf("PaywallUsesTelegramInvoice() = %v, want %v", g, tc.want)
			}
		})
	}
}

func TestYookassaAmountWithXTR(t *testing.T) {
	t.Setenv("PAYMENT_YOOKASSA_AMOUNT_RUB", "")
	t.Setenv("PAYMENT_AMOUNT_RUB", "250")
	t.Setenv("PAYMENT_YOOKASSA_AMOUNT_MINOR_UNITS", "")
	minor, cur := yookassaAmountAndCurrencyFromEnv("XTR", 100)
	if minor != 25000 || cur != "RUB" {
		t.Fatalf("got %d %s want 25000 RUB", minor, cur)
	}
}

func TestLoad_PaymentCurrencyUppercaseAndXTR(t *testing.T) {
	dir := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PAYMENT_CURRENCY", "  xtr  ")
	t.Setenv("PAYMENT_AMOUNT_STARS", "33")
	t.Setenv("PAYMENT_AMOUNT_MINOR_UNITS", "")
	// Avoid picking stray tokens from parent env if any
	t.Setenv("PAYMENT_PROVIDER_TOKEN", "")
	t.Setenv("YOOKASSA_SHOP_ID", "")
	t.Setenv("YOOKASSA_SECRET_KEY", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.PaymentCurrency != "XTR" {
		t.Fatalf("currency: got %q want XTR", cfg.PaymentCurrency)
	}
	if cfg.PaymentAmountMinorUnits != 33 {
		t.Fatalf("amount: got %d want 33", cfg.PaymentAmountMinorUnits)
	}
	if !cfg.PaywallUsesStars() || !cfg.PaywallPaymentReady() || !cfg.PaywallUsesTelegramInvoice() {
		t.Fatalf("flags: stars=%v ready=%v invoice=%v",
			cfg.PaywallUsesStars(), cfg.PaywallPaymentReady(), cfg.PaywallUsesTelegramInvoice())
	}
}
