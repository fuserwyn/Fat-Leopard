package bot

import "testing"

// Автореакции ставят все админы, кроме автора; дубли id (OWNER_ID есть и в ADMIN_IDS) не задваивают.
func TestTrainingFeedAutoReactionAdmins(t *testing.T) {
	got := trainingFeedAutoReactionAdmins([]int64{1, 2, 3, 2, 0}, 3)
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("got %v", got)
	}
	if got := trainingFeedAutoReactionAdmins(nil, 3); len(got) != 0 {
		t.Errorf("пустой список админов: %v", got)
	}
}
