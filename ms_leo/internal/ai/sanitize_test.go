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

func TestSanitizeStripsUserRequestPreamble(t *testing.T) {
	t.Parallel()
	in := "Ответ на запрос пользователя:\n\nПривет, леопард!"
	got := SanitizeTextForUser(in)
	if strings.Contains(strings.ToLower(got), "ответ на запрос пользователя") {
		t.Errorf("expected preamble stripped, got %q", got)
	}
	if !strings.Contains(got, "Привет") {
		t.Errorf("expected body kept, got %q", got)
	}
}
