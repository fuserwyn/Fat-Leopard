package leopardmoney

import "testing"

func TestLevelFromTotalCups_boundaries(t *testing.T) {
	cases := []struct {
		total int
		want  int
	}{
		{-100, 1}, // защита от отрицательных
		{0, 1},
		{419, 1},
		{420, 2}, // нижняя граница L2
		{1259, 2},
		{1260, 3},
		{2939, 3},
		{2940, 4},
		{6299, 4},
		{6300, 5},
		{13019, 5},
		{13020, 6},
		{26459, 6},
		{26460, 6},
		{1000000, 6}, // макс. уровень не растёт выше Слона
	}
	for _, c := range cases {
		if got := LevelFromTotalCups(c.total); got != c.want {
			t.Errorf("LevelFromTotalCups(%d) = %d, want %d", c.total, got, c.want)
		}
	}
}

func TestLastAchievementMilestoneForStreak(t *testing.T) {
	cases := []struct {
		streak int
		want   int
	}{
		{0, 0},
		{6, 0},
		{7, 7},
		{13, 7},
		{14, 14},
		{29, 14},
		{30, 30},
		{41, 30},
		{42, 42},
		{200, 180}, // 200 не порог; последний достигнутый — 180
		{365, 365},
		{420, 420},
		{1000, 420}, // дальше порогов нет
	}
	for _, c := range cases {
		if got := LastAchievementMilestoneForStreak(c.streak); got != c.want {
			t.Errorf("LastAchievementMilestoneForStreak(%d) = %d, want %d", c.streak, got, c.want)
		}
	}
}

// AchievementsCountForStreak никогда не должен превышать MaxAchievements и быть монотонным.
func TestAchievementsCountForStreak_Bounds(t *testing.T) {
	if got := AchievementsCountForStreak(-5); got != 0 {
		t.Errorf("negative streak should give 0, got %d", got)
	}
	if got := AchievementsCountForStreak(1_000_000); got > MaxAchievements {
		t.Errorf("count %d exceeds MaxAchievements %d", got, MaxAchievements)
	}
	prev := 0
	for s := 0; s <= 500; s++ {
		got := AchievementsCountForStreak(s)
		if got < prev {
			t.Fatalf("AchievementsCountForStreak not monotonic at streak=%d: %d < %d", s, got, prev)
		}
		prev = got
	}
}

func TestParseTrainingDoneReport_table(t *testing.T) {
	cases := []struct {
		name    string
		text    string
		wantDur int
		wantInt int
		wantCat string
		wantOK  bool
	}{
		{"miniapp run line", "бег, 15 мин, инт. 3/5", 15, 3, "run", true},
		{"tennis", "теннис, 60 мин, инт. 3/5", 60, 3, "tennis", true},
		{"padel", "падел, 45 мин, инт. 4/5", 45, 4, "padel", true},
		{"football", "футбол, 60 мин, инт. 3/5", 60, 3, "football", true},
		{"volleyball", "волейбол, 45 мин, инт. 4/5", 45, 4, "volleyball", true},
		{"gymnastics", "гимнастика, 45 мин, инт. 3/5", 45, 3, "gymnastics", true},
		{"with hashtag prefix", "#training_done — йога, 30 мин, инт. 2/5", 30, 2, "yoga", true},
		{"intensity defaults to 1", "ходьба, 20 мин", 20, 1, "walk", true},
		{"unknown label -> other", "квиддич, 10 мин, инт. 4/5", 10, 4, "other", true},
		{"no header -> not ok", "просто текст без формата", 0, 0, "other", false},
		{"zero duration -> not ok", "бег, 0 мин", 0, 0, "other", false},
		{"multiline takes first line", "бег, 15 мин, инт. 5/5\nбыло тяжело", 15, 5, "run", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dur, in, cat, ok := ParseTrainingDoneReport(c.text)
			if dur != c.wantDur || in != c.wantInt || cat != c.wantCat || ok != c.wantOK {
				t.Errorf("ParseTrainingDoneReport(%q) = (%d, %d, %q, %v), want (%d, %d, %q, %v)",
					c.text, dur, in, cat, ok, c.wantDur, c.wantInt, c.wantCat, c.wantOK)
			}
		})
	}
}

func TestActivityCoeff(t *testing.T) {
	cases := []struct {
		cat  string
		want float64
	}{
		{"yoga", 0.8},
		{"walk", 0.8},
		{"workout", 1.0},
		{"run", 1.2},
		{"swim", 1.2},
		{"tennis", 1.2},
		{"padel", 1.2},
		{"football", 1.2},
		{"volleyball", 1.2},
		{"gymnastics", 1.0},
		{"hiit", 1.5},
		{"crossfit", 1.5},
		{"  RUN  ", 1.2}, // тримминг + регистр
		{"unknown", 1.0}, // дефолт
		{"", 1.0},
	}
	for _, c := range cases {
		if got := ActivityCoeff(c.cat); got != c.want {
			t.Errorf("ActivityCoeff(%q) = %v, want %v", c.cat, got, c.want)
		}
	}
}
