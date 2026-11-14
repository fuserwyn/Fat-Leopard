package bot

import (
	"strings"
	"testing"

	"leo-bot/internal/database"
)

func TestTrackerAgentChoiceLabel(t *testing.T) {
	cases := map[string]string{"claude": "Claude", "Cursor": "Cursor", "": "Авто", "auto": "Авто", "gpt": "Авто"}
	for in, want := range cases {
		if got := trackerAgentChoiceLabel(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestTrackerStepHas(t *testing.T) {
	task := database.TrackerTask{Steps: []string{"Взяли в работу", trackerCursorHandoffStep}}
	if !trackerStepHas(task, trackerCursorHandoffStep) || trackerStepHas(task, trackerAttemptsExhaustedStep) {
		t.Errorf("шаги: %v", task.Steps)
	}
}

func TestTrackerLooksLikeNoteOnly(t *testing.T) {
	for _, text := range []string{"TRACKER_NO_CODE", "Ревью: на ветке только заметка", "кода нет", "репозиторий не менялся"} {
		if !trackerLooksLikeNoteOnly(text) {
			t.Errorf("%q — только заметка", text)
		}
	}
	if trackerLooksLikeNoteOnly("коммит abc123 выполнение") {
		t.Error("коммит — не заметка")
	}
}

// Кнопка «дальше» на карточке: колонка после текущей с учётом галочек ревью и теста.
func TestTrackerAdvanceColumn(t *testing.T) {
	cases := []struct {
		task database.TrackerTask
		want string
		ok   bool
	}{
		{database.TrackerTask{DevColumn: trackerColTodo}, trackerColDoing, true},
		{database.TrackerTask{DevColumn: trackerColApprove}, trackerColDoing, true},
		{database.TrackerTask{DevColumn: trackerColDoing}, trackerColReview, true},
		{database.TrackerTask{DevColumn: trackerColDoing, SkipReview: true}, trackerColTest, true},
		{database.TrackerTask{DevColumn: trackerColDoing, SkipReview: true, SkipTest: true}, trackerColDeploy, true},
		{database.TrackerTask{DevColumn: trackerColReview, SkipTest: true}, trackerColDeploy, true},
		{database.TrackerTask{DevColumn: trackerColTest}, trackerColDeploy, true},
		{database.TrackerTask{DevColumn: trackerColDeploy}, trackerColDone, true},
		{database.TrackerTask{DevColumn: trackerColDone}, "", false},
	}
	for _, c := range cases {
		got, ok := trackerAdvanceColumn(c.task)
		if got != c.want || ok != c.ok {
			t.Errorf("%s (ревью=%v тест=%v): %q %v, want %q %v",
				c.task.DevColumn, !c.task.SkipReview, !c.task.SkipTest, got, ok, c.want, c.ok)
		}
	}
}

func TestTrackerApprovalRejectKeyboard(t *testing.T) {
	kb := trackerApprovalRejectKeyboard(42)
	if len(kb.InlineKeyboard) != 2 || len(kb.InlineKeyboard[1]) != 2 {
		t.Fatalf("раскладка: %+v", kb.InlineKeyboard)
	}
	for _, row := range kb.InlineKeyboard {
		for _, btn := range row {
			if btn.CallbackData == nil || !strings.HasSuffix(*btn.CallbackData, "_42") {
				t.Errorf("кнопка %q без id задачи", btn.Text)
			}
		}
	}
}

func TestTrackerNotifyAuthor(t *testing.T) {
	if got := trackerNotifyAuthor(database.TrackerTask{HasAuthor: true, AuthorID: 7}); got != 7 {
		t.Errorf("автор: %d", got)
	}
	if got := trackerNotifyAuthor(database.TrackerTask{AuthorID: 7}); got != 0 {
		t.Errorf("без автора: %d", got)
	}
	err := &trackerShipDeployError{Base: "main", Msg: "Not Authorized"}
	if !strings.Contains(err.Error(), "влили в main") || !strings.Contains(err.Error(), "Not Authorized") {
		t.Errorf("текст ошибки: %s", err.Error())
	}
}
