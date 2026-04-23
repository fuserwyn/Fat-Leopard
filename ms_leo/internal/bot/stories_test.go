package bot

import (
	"strings"
	"testing"

	"leo-bot/internal/config"
)

func TestStory1RemovalDMContentAndButton(t *testing.T) {
	text := removalDMText()
	if !strings.Contains(text, "7 дней без движения") {
		t.Fatalf("expected removal DM to mention 7 days, got: %q", text)
	}
	if !strings.Contains(text, "XP сгорел") {
		t.Fatalf("expected removal DM to mention XP burn, got: %q", text)
	}

	markup := removalDMReplyMarkup()
	if markup == nil || len(markup.InlineKeyboard) == 0 || len(markup.InlineKeyboard[0]) == 0 {
		t.Fatal("expected inline keyboard with return button")
	}
	btn := markup.InlineKeyboard[0][0]
	if btn.Text != "Вернуться в стаю" {
		t.Fatalf("button text mismatch: %q", btn.Text)
	}
	if btn.CallbackData == nil || *btn.CallbackData != paywallCallbackReturnToPack {
		t.Fatalf("button callback mismatch: %+v", btn.CallbackData)
	}
}

func TestStory2ReturnKeyboardVariants(t *testing.T) {
	tests := []struct {
		name              string
		cfg               *config.Config
		wantButtonsByText []string
	}{
		{
			name: "stars and provider",
			cfg: &config.Config{
				PaymentStarsEnabled:   true,
				PaymentStarsAmount:    210,
				PaymentProviderToken:  "provider-token",
				PaymentCurrency:       "RUB",
				PaymentAmountMinorUnits: 21000,
			},
			wantButtonsByText: []string{"⭐ Оплатить Stars", "💳 Оплатить картой"},
		},
		{
			name: "stars only",
			cfg: &config.Config{
				PaymentCurrency:         "XTR",
				PaymentAmountMinorUnits: 210,
			},
			wantButtonsByText: []string{"⭐ Оплатить Stars"},
		},
		{
			name: "card only yookassa",
			cfg: &config.Config{
				YookassaShopID:      "shop",
				YookassaSecretKey:   "key",
				YookassaAmountMinor: 21000,
				YookassaCurrency:    "RUB",
				PaymentCurrency:     "RUB",
			},
			wantButtonsByText: []string{"💳 Оплатить картой"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := &Bot{config: tc.cfg}
			kb := b.paywallReturnInlineKeyboard()
			if kb == nil {
				t.Fatal("expected keyboard")
			}
			var got []string
			for _, row := range kb.InlineKeyboard {
				for _, btn := range row {
					got = append(got, btn.Text)
				}
			}
			if len(got) != len(tc.wantButtonsByText) {
				t.Fatalf("buttons count: got=%v want=%v", got, tc.wantButtonsByText)
			}
			for i := range got {
				if got[i] != tc.wantButtonsByText[i] {
					t.Fatalf("buttons mismatch: got=%v want=%v", got, tc.wantButtonsByText)
				}
			}
		})
	}
}

func TestStory2ReturnPromptTextIncludesPrice(t *testing.T) {
	txt := paywallReturnPromptText("210 ₽ или 210 ⭐")
	if !strings.Contains(txt, "Возвращение в стаю") {
		t.Fatalf("unexpected prompt text: %q", txt)
	}
	if !strings.Contains(txt, "Оплатить Stars") || !strings.Contains(txt, "Оплатить картой") {
		t.Fatalf("prompt does not include both methods: %q", txt)
	}
	if !strings.Contains(txt, "210 ₽ или 210 ⭐") {
		t.Fatalf("prompt does not include price: %q", txt)
	}
}

func TestStory3ReturnedMemberWelcomeText(t *testing.T) {
	txt := returnedMemberWelcomeText()
	checks := []string{
		"Ты снова в стае",
		"42 XP",
		"Ачивок нет",
		"Семь дней подряд — ачивка",
	}
	for _, c := range checks {
		if !strings.Contains(txt, c) {
			t.Fatalf("returned welcome text missing %q in %q", c, txt)
		}
	}
}

