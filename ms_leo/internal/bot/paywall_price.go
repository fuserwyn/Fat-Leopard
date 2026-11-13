package bot

import (
	"fmt"
	"strings"

	"leo-bot/internal/game/leopardmoney"
)

const (
	minPaywallPriceRub = 1
	maxPaywallPriceRub = 100_000
)

func parsePaywallPriceRub(rub int) (amountMinor int, err error) {
	if rub < minPaywallPriceRub || rub > maxPaywallPriceRub {
		return 0, fmt.Errorf("price must be from %d to %d ₽", minPaywallPriceRub, maxPaywallPriceRub)
	}
	return rub * 100, nil
}

func formatPaywallAmountShort(amountMinor int, currency string) string {
	if amountMinor <= 0 {
		return ""
	}
	cur := strings.ToUpper(strings.TrimSpace(currency))
	if cur == "RUB" || cur == "" {
		rub := amountMinor / 100
		kop := amountMinor % 100
		if kop == 0 {
			return fmt.Sprintf("%d ₽", rub)
		}
		return fmt.Sprintf("%d,%02d ₽", rub, kop)
	}
	if cur == "XTR" {
		return fmt.Sprintf("%d ⭐", amountMinor)
	}
	return fmt.Sprintf("%d %s", amountMinor, cur)
}

func (b *Bot) packPaywallOverrideMinor() int {
	if b == nil || b.db == nil || b.config == nil || b.config.MonetizedChatID == 0 {
		return 0
	}
	n, ok, err := b.db.GetPackPaywallAmountMinor(b.config.MonetizedChatID)
	if err != nil || !ok || n <= 0 {
		return 0
	}
	return n
}

func (b *Bot) defaultPaywallAmountMinor() int {
	if b != nil && b.config != nil {
		if b.config.YookassaAmountMinor > 0 {
			return b.config.YookassaAmountMinor
		}
		if !strings.EqualFold(strings.TrimSpace(b.config.PaymentCurrency), "XTR") && b.config.PaymentAmountMinorUnits > 0 {
			return b.config.PaymentAmountMinorUnits
		}
	}
	return leopardmoney.EntryRub * 100
}

func (b *Bot) paywallYookassaAmountMinor() int {
	if n := b.packPaywallOverrideMinor(); n > 0 {
		return n
	}
	if b != nil && b.config != nil {
		return b.config.YookassaAmountMinor
	}
	return 0
}

func (b *Bot) paywallProviderAmountMinor() int {
	if b != nil && b.config != nil && strings.EqualFold(strings.TrimSpace(b.config.PaymentCurrency), "XTR") {
		return b.config.PaymentAmountMinorUnits
	}
	if n := b.packPaywallOverrideMinor(); n > 0 {
		return n
	}
	if b != nil && b.config != nil {
		return b.config.PaymentAmountMinorUnits
	}
	return 0
}

func (b *Bot) paywallYookassaReady() bool {
	if b == nil || b.config == nil {
		return false
	}
	if b.config.YookassaShopID == "" || b.config.YookassaSecretKey == "" {
		return false
	}
	return b.paywallYookassaAmountMinor() > 0 && strings.TrimSpace(b.config.YookassaCurrency) != ""
}

func (b *Bot) paywallPaymentReady() bool {
	if b == nil || b.config == nil {
		return false
	}
	if b.config.PaywallUsesStars() {
		return true
	}
	if b.config.PaywallUsesTelegramProviderInvoice() {
		return true
	}
	return b.paywallYookassaReady()
}

// AccessPriceRub — текущая цена access в рублях (оверрайд админа или дефолт сервера).
func (b *Bot) AccessPriceRub() int {
	minor := 0
	if n := b.packPaywallOverrideMinor(); n > 0 {
		minor = n
	} else {
		minor = b.defaultPaywallAmountMinor()
	}
	if minor <= 0 {
		return leopardmoney.EntryRub
	}
	return minor / 100
}
