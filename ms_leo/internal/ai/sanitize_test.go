package ai

import (
	"strings"
	"testing"
)

func TestStripAsteriskStageRemarks(t *testing.T) {
	t.Parallel()
	s := "Текст ответа.\n\n*Рычит* 🐆"
	got := SanitizeTextForUser(s)
	if strings.Contains(got, "*Рычит*") || strings.Contains(got, "Рычит*") {
		t.Errorf("expected *Рычит* removed, got %q", got)
	}
}

func TestStripAsteriskOwnLine(t *testing.T) {
	t.Parallel()
	s := "Ок\n\n*Ррр* 🐆\n"
	got := stripAsteriskStageRemarks(s)
	if strings.Contains(got, "Ррр") {
		t.Errorf("expected line removed, got %q", got)
	}
}
