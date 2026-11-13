package bot

import "testing"

func TestLeoTextNeedsEnglishTranslate(t *testing.T) {
	if !leoTextNeedsEnglishTranslate("Привет, хищник") {
		t.Fatal("cyrillic")
	}
	if leoTextNeedsEnglishTranslate("Hello predator") {
		t.Fatal("latin only")
	}
	if leoTextNeedsEnglishTranslate("") {
		t.Fatal("empty")
	}
}

func TestTranslateLeoAlreadyEnglish(t *testing.T) {
	var b Bot
	got, err := b.TranslateLeoTextToEnglish("Stay steady today.")
	if err != nil {
		t.Fatal(err)
	}
	if got != "Stay steady today." {
		t.Fatalf("got %q", got)
	}
}
