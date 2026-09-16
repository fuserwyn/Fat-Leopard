package trainingfeed

import "testing"

func TestAllowedEmojisForReportIncludesSportOnlyFromReport(t *testing.T) {
	runReport := "бег, 15 мин, инт. 3/5"
	allowed := AllowedEmojisForReport(runReport)
	if !isEmojiInList("🏃", allowed) {
		t.Fatal("expected run emoji for run report")
	}
	if isEmojiInList("🏊", allowed) {
		t.Fatal("swim emoji must not appear on run report")
	}
	for _, e := range TrainingFeedApprovingEmojis {
		if !isEmojiInList(e, allowed) {
			t.Fatalf("approving emoji %q missing from allowed list", e)
		}
	}
}

func TestLeoReactionEmojiStableAndApproving(t *testing.T) {
	report := "йога, 20 мин, инт. 2/5"
	cats := CategoryIDsFromReport(report)
	a := LeoReactionEmoji(101, cats)
	b := LeoReactionEmoji(202, cats)
	if a == "" || b == "" {
		t.Fatal("expected non-empty emoji")
	}
	if !IsEmojiAllowedForCategories(a, cats) || !IsEmojiAllowedForCategories(b, cats) {
		t.Fatalf("leo emojis must be allowed: %q %q", a, b)
	}
	if LeoReactionEmoji(101, cats) != a {
		t.Fatal("emoji must be stable for the same post id")
	}
}

func TestRemapReactionEmojiFixesInvalid(t *testing.T) {
	report := "плавание, 30 мин, инт. 4/5"
	remapped := RemapReactionEmoji("🏃", 55, report)
	if remapped == "🏃" {
		t.Fatal("run emoji must be remapped on swim report")
	}
	if !IsEmojiAllowedForReport(remapped, report) {
		t.Fatalf("remapped emoji %q not allowed", remapped)
	}
}

func TestRemapReactionEmojiKeepsValid(t *testing.T) {
	report := "бег, 10 мин, инт. 3/5"
	if got := RemapReactionEmoji("🏃", 1, report); got != "🏃" {
		t.Fatalf("valid emoji must stay, got %q", got)
	}
}
