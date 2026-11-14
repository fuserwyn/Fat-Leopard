package bot

import (
	"strings"
	"testing"

	"leo-bot/internal/config"
	"leo-bot/internal/i18n"
)

func TestStory1RemovalDMContentAndButton(t *testing.T) {
	text := removalDMText(i18n.EN)
	if !strings.Contains(text, "7 days without movement") {
		t.Fatalf("expected removal DM to mention 7 days, got: %q", text)
	}
	if !strings.Contains(text, "Pack progress reset") {
		t.Fatalf("expected removal DM to mention progress reset, got: %q", text)
	}

	markup := removalDMReplyMarkup(i18n.EN)
	if markup == nil || len(markup.InlineKeyboard) == 0 || len(markup.InlineKeyboard[0]) == 0 {
		t.Fatal("expected inline keyboard with return button")
	}
	btn := markup.InlineKeyboard[0][0]
	if btn.Text != "Rejoin the pack" {
		t.Fatalf("button text mismatch: %q", btn.Text)
	}
	if btn.CallbackData == nil || *btn.CallbackData != paywallCallbackReturnToPack {
		t.Fatalf("button callback mismatch: %+v", btn.CallbackData)
	}

	ru := removalDMText(i18n.RU)
	if !strings.Contains(ru, "7 дней без движения") {
		t.Fatalf("expected RU removal DM, got: %q", ru)
	}
	if got := removalDMReplyMarkup(i18n.RU).InlineKeyboard[0][0].Text; got != "Вернуться в стаю" {
		t.Fatalf("RU button text mismatch: %q", got)
	}
}

func TestStory2ReturnKeyboardVariants(t *testing.T) {
	// С апреля 2026 цена пишется прямо в подписи кнопки (см. требование пользователя
	// «и в карте и в звёздах сразу в способах оплаты писать стоимость»). Тест проверяет
	// и наличие нужных способов, и присутствие цены в каждой кнопке.
	tests := []struct {
		name              string
		cfg               *config.Config
		wantButtonsByText []string
	}{
		{
			name: "stars and provider",
			cfg: &config.Config{
				PaymentStarsEnabled:     true,
				PaymentStarsAmount:      210,
				PaymentProviderToken:    "provider-token",
				PaymentCurrency:         "RUB",
				PaymentAmountMinorUnits: 21000,
			},
			wantButtonsByText: []string{"💳 Pay by card (Telegram) — 210 ₽", "⭐ Telegram Stars — 210 ⭐"},
		},
		{
			name: "stars only",
			cfg: &config.Config{
				PaymentCurrency:         "XTR",
				PaymentAmountMinorUnits: 210,
			},
			wantButtonsByText: []string{"⭐ Telegram Stars — 210 ⭐"},
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
			wantButtonsByText: []string{"💳 Bank card — Russia — 210 ₽"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := &Bot{config: tc.cfg}
			kb := b.paywallReturnInlineKeyboard(1)
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

func TestStory2ReturnPromptText(t *testing.T) {
	// Mini-app-only архитектура: «возвращения» и упоминаний группы быть не должно.
	// Текст должен явно говорить про вход в MiniApp + что цена на кнопке.
	txt := paywallReturnPromptText("")
	if strings.Contains(txt, "Возвращение") || strings.Contains(txt, "возвращ") {
		t.Fatalf("prompt must not mention 'возвращение' (no group concept anymore): %q", txt)
	}
	if strings.Contains(strings.ToLower(txt), "групп") {
		t.Fatalf("prompt must not mention group: %q", txt)
	}
	if !strings.Contains(txt, "Fat Leopard MiniApp") {
		t.Fatalf("prompt should reference Fat Leopard MiniApp: %q", txt)
	}
	if !strings.Contains(strings.ToLower(txt), "price") {
		t.Fatalf("prompt should mention that price is on the button: %q", txt)
	}
}

