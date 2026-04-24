package yookassa

import (
	"strings"
	"testing"
)

func TestCreatePayment_validation(t *testing.T) {
	meta := map[string]string{"user_telegram_id": "1", "invoice_payload": "pw_1"}
	_, _, err := CreatePayment("", "secret", 100, "RUB", "x", "https://t.me", "", meta)
	if err == nil || !strings.Contains(err.Error(), "shop_id") {
		t.Fatalf("empty shop: %v", err)
	}
	_, _, err = CreatePayment("shop", "", 100, "RUB", "x", "https://t.me", "", meta)
	if err == nil || !strings.Contains(err.Error(), "secret") {
		t.Fatalf("empty secret: %v", err)
	}
	_, _, err = CreatePayment("shop", "secret", 0, "RUB", "x", "https://t.me", "", meta)
	if err == nil || !strings.Contains(err.Error(), "amount") {
		t.Fatalf("zero amount: %v", err)
	}
	_, _, err = CreatePayment("shop", "secret", 100, "RUB", "x", "t.me", "", meta)
	if err == nil || !strings.Contains(err.Error(), "return_url") {
		t.Fatalf("bad return url: %v", err)
	}
}

func TestNewIDempotenceKey(t *testing.T) {
	a, err := newIDempotenceKey()
	if err != nil {
		t.Fatal(err)
	}
	b, err := newIDempotenceKey()
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 32 || len(b) != 32 {
		t.Fatalf("want 32 hex chars, got %d and %d", len(a), len(b))
	}
	if a == b {
		t.Fatal("keys should differ (extremely unlikely if equal, check rand)")
	}
}

func TestRefundPaymentValidation(t *testing.T) {
	err := RefundPayment("", "secret", "payment-id", 100, "RUB")
	if err == nil || !strings.Contains(err.Error(), "shop_id") {
		t.Fatalf("empty shop: %v", err)
	}
	err = RefundPayment("shop", "secret", "", 100, "RUB")
	if err == nil || !strings.Contains(err.Error(), "payment_id") {
		t.Fatalf("empty payment id: %v", err)
	}
	err = RefundPayment("shop", "secret", "payment-id", 0, "RUB")
	if err == nil || !strings.Contains(err.Error(), "amount") {
		t.Fatalf("zero amount: %v", err)
	}
}
