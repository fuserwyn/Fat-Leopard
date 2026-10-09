package bot

import (
	"testing"

	"leo-bot/internal/config"
)

func TestGuestFeedName(t *testing.T) {
	cases := map[string]string{
		"":          "Участник стаи",
		"  ":        "Участник стаи",
		"user12345": "Участник стаи",
		" Анна ":    "Анна",
		"userka":    "userka",
	}
	for in, want := range cases {
		if got := guestFeedName(in); got != want {
			t.Errorf("guestFeedName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGuestJoinLink(t *testing.T) {
	if got := guestJoinLink(""); got != "" {
		t.Fatalf("пустое имя бота: %q", got)
	}
	if got := guestJoinLink(" leo_bot "); got != "https://t.me/leo_bot?start=src-guest_feed" {
		t.Fatalf("ссылка: %q", got)
	}
	if got := parseStartSource(guestJoinStartPayload); got != "guest_feed" {
		t.Fatalf("источник визита: %q", got)
	}
}

func TestPackGuestFeedForViewer_NoDB(t *testing.T) {
	var nilBot *Bot
	v, err := nilBot.PackGuestFeedForViewer(1)
	if err != nil || v.Items == nil || len(v.Items) != 0 || v.InPack {
		t.Fatalf("nil bot: %+v %v", v, err)
	}
	// Стая не настроена — гостю закрывать нечего, мини-апп открывается как обычно.
	v, err = (&Bot{config: &config.Config{}}).PackGuestFeedForViewer(1)
	if err != nil || !v.InPack || len(v.Items) != 0 {
		t.Fatalf("без MONETIZED_CHAT_ID: %+v %v", v, err)
	}
	// Стая есть, а базы нет — пустая витрина, не паника.
	v, err = (&Bot{config: &config.Config{MonetizedChatID: -1}}).PackGuestFeedForViewer(1)
	if err != nil || v.InPack || len(v.Items) != 0 {
		t.Fatalf("без базы: %+v %v", v, err)
	}
}

func TestPaywallUnpaidInlineKeyboard_GuestFeedButton(t *testing.T) {
	cfg := &config.Config{PaymentCurrency: "XTR", PaymentAmountMinorUnits: 100}
	b := &Bot{config: cfg}
	kb := b.paywallUnpaidInlineKeyboard()
	if kb == nil {
		t.Fatal("нет клавиатуры оплаты")
	}
	for _, row := range kb.InlineKeyboard {
		for _, btn := range row {
			if btn.WebApp != nil {
				t.Fatalf("без MINIAPP_WEB_APP_URL кнопки ленты быть не должно: %+v", btn)
			}
		}
	}

	cfg.MiniappWebAppURL = "https://app.example.test/"
	kb = b.paywallUnpaidInlineKeyboard()
	last := kb.InlineKeyboard[len(kb.InlineKeyboard)-1][0]
	if last.Text != guestFeedButtonText || last.WebApp == nil || last.WebApp.URL != "https://app.example.test/" {
		t.Fatalf("кнопка гостевой ленты: %+v", last)
	}

	// Нет ни одного способа оплаты — и кнопки ленты нет: экран оплаты не отправляется.
	if kb := (&Bot{config: &config.Config{MiniappWebAppURL: "https://app.example.test/"}}).paywallUnpaidInlineKeyboard(); kb != nil {
		t.Fatalf("без способов оплаты клавиатура не нужна: %+v", kb)
	}
}
