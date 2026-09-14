package prompts

import (
	"testing"
	"time"
)

func TestCatalogHasEmbedFiles(t *testing.T) {
	if len(Catalog()) != 15 {
		t.Fatalf("slots: %d", len(Catalog()))
	}
	for _, s := range Catalog() {
		if s.embedded == "" || s.File == "" {
			t.Fatalf("empty %s", s.Key)
		}
	}
}

func TestApplyOverrides(t *testing.T) {
	base := DefaultBundle()
	got := ApplyOverrides(base, map[string]string{"answer_user_question": "новый характер"})
	if got.AnswerUserQuestion != "новый характер" {
		t.Fatal(got.AnswerUserQuestion)
	}
	if got.DailySummary != base.DailySummary {
		t.Fatal("other fields stay")
	}
}

func TestDailyWisdomTrainingVariantRotates(t *testing.T) {
	base := DefaultBundle()
	if base.DailyWisdomVariation1 == "" || base.DailyWisdomVariation2 == "" {
		t.Fatal("variation embeds empty")
	}
	a := base.DailyWisdomTrainingVariant(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	b := base.DailyWisdomTrainingVariant(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
	c := base.DailyWisdomTrainingVariant(time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC))
	if a == "" || b == "" || c == "" {
		t.Fatal("empty variant")
	}
	if a == b && b == c {
		t.Fatal("variants did not rotate")
	}
	got := ApplyOverrides(base, map[string]string{"daily_wisdom_variation1": "вариант один"})
	if got.DailyWisdomVariation1 != "вариант один" {
		t.Fatal(got.DailyWisdomVariation1)
	}
}
