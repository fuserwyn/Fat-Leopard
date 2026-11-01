package config

import (
	"reflect"
	"testing"
)

func TestParseAmountTiers(t *testing.T) {
	cases := []struct {
		in   string
		want []int
	}{
		{"50,150,500", []int{50, 150, 500}},
		{" 500 , 50 ,150 ", []int{50, 150, 500}},
		{"100,100,abc,0,-5,300", []int{100, 300}},
		{"", nil},
		{"нет чисел", nil},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := parseAmountTiers(tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseAmountTiers(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// Сумма доната приходит из мини-аппа, поэтому её обязательно сверяем с номиналами:
// иначе клиент мог бы выставить себе счёт на 1 звезду или на произвольную сумму.
func TestDonateTierAllowed(t *testing.T) {
	c := &Config{
		DonateStarsTiers:   []int{50, 150},
		DonateCardTiersRub: []int{100},
		YookassaShopID:     "shop",
		YookassaSecretKey:  "secret",
	}
	if !c.DonateStarsTierAllowed(150) || !c.DonateCardTierAllowed(100) {
		t.Fatal("номинал из списка должен проходить")
	}
	if c.DonateStarsTierAllowed(1) || c.DonateCardTierAllowed(0) || c.DonateStarsTierAllowed(-50) {
		t.Fatal("сумма вне списка не должна проходить")
	}
	if !c.DonateStarsReady() || !c.DonateCardReady() {
		t.Fatal("оба способа настроены")
	}
}

func TestDonateReadinessRequiresConfig(t *testing.T) {
	// Звёздам не нужны ни provider token, ни ключи ЮKassa — только номиналы.
	stars := &Config{DonateStarsTiers: []int{50}}
	if !stars.DonateStarsReady() {
		t.Fatal("звёзды должны быть готовы без платёжных ключей")
	}
	// Карта без ключей ЮKassa недоступна, даже если номиналы заданы.
	card := &Config{DonateCardTiersRub: []int{100}}
	if card.DonateCardReady() {
		t.Fatal("карта без ключей ЮKassa не должна быть доступна")
	}
	// Пустые номиналы выключают способ.
	empty := &Config{YookassaShopID: "shop", YookassaSecretKey: "secret"}
	if empty.DonateStarsReady() || empty.DonateCardReady() {
		t.Fatal("без номиналов способы выключены")
	}
}

func TestDonateStarsTiersAlwaysIncludeOneAndFive(t *testing.T) {
	t.Setenv("DONATE_STARS_TIERS", "50,150,500")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.DonateStarsTierAllowed(1) || !cfg.DonateStarsTierAllowed(5) ||
		!cfg.DonateStarsTierAllowed(10) || !cfg.DonateStarsTierAllowed(100) ||
		!cfg.DonateStarsTierAllowed(1000) || !cfg.DonateStarsTierAllowed(50) {
		t.Fatalf("tiers=%v", cfg.DonateStarsTiers)
	}
	if cfg.DonateStarsTierAllowed(150) {
		t.Fatalf("150 должен быть скрыт даже из env: %v", cfg.DonateStarsTiers)
	}
}

func TestExcludeAmountTiers(t *testing.T) {
	got := excludeAmountTiers([]int{1, 50, 150, 500}, []int{150})
	if !reflect.DeepEqual(got, []int{1, 50, 500}) {
		t.Fatalf("%v", got)
	}
}

func TestDonateCardTiersAlwaysInclude1000(t *testing.T) {
	t.Setenv("DONATE_CARD_TIERS_RUB", "100,300")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.DonateCardTierAllowed(1000) || !cfg.DonateCardTierAllowed(100) {
		t.Fatalf("card tiers=%v", cfg.DonateCardTiersRub)
	}
}

func TestPaywallEntryFreeDefaultsToTrue(t *testing.T) {
	t.Setenv("PAYWALL_ENABLED", "true")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.PaywallEntryFree {
		t.Fatal("по умолчанию вход бесплатный (PAYWALL_ENTRY_FREE=true)")
	}

	t.Setenv("PAYWALL_ENTRY_FREE", "false")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.PaywallEntryFree {
		t.Fatal("PAYWALL_ENTRY_FREE=false возвращает платный вход")
	}
}

func TestServerPaymentRotationDefaultOrder(t *testing.T) {
	got := parseAdminIDs(defaultServerPaymentAdminIDs)
	want := []int64{166039118, 202498592, 414802636} // Стас, Анна, lofi
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("место %d: got %d want %d", i, got[i], want[i])
		}
	}
}
