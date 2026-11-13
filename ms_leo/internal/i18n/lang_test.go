package i18n

import "testing"

func TestFromTelegram(t *testing.T) {
	if FromTelegram("ru") != RU {
		t.Fatalf("ru")
	}
	if FromTelegram("ru-RU") != RU {
		t.Fatalf("ru-RU")
	}
	if FromTelegram("en") != EN {
		t.Fatalf("en")
	}
	if FromTelegram("") != EN {
		t.Fatalf("empty default en")
	}
}

func TestT(t *testing.T) {
	got := T(RU, "guest_feed_btn")
	if got != "👀 Посмотреть ленту стаи" {
		t.Fatalf("got %q", got)
	}
	got = T(EN, "guest_feed_btn")
	if got != "👀 View pack feed" {
		t.Fatalf("got %q", got)
	}
	got = T(RU, "paywall_btn_card", 99)
	if got != "💳 Оплатить картой · 99 ₽" {
		t.Fatalf("got %q", got)
	}
}
