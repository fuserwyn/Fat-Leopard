package leopardmoney

import "testing"

func TestAchievementsCountForStreak(t *testing.T) {
	if AchievementsCountForStreak(0) != 0 {
		t.Fatalf("0 streak")
	}
	if AchievementsCountForStreak(7) != 1 {
		t.Fatalf("7 days = 1 ach")
	}
	if AchievementsCountForStreak(43) != 4 {
		t.Fatalf("43 days = 4 ach (7,14,30,42)")
	}
	if AchievementsCountForStreak(365) != 8 {
		t.Fatalf("365 days = 8 ach")
	}
	if AchievementsCountForStreak(420) != 9 {
		t.Fatalf("420 days = 9 ach")
	}
	if AchievementsCountForStreak(999) != 10 {
		t.Fatalf("999 days = 10 ach (500 reached)")
	}
	if AchievementsCountForStreak(1000) != MaxAchievements {
		t.Fatalf("1000 days = all ach")
	}
}

func TestStreakAchievementIndex(t *testing.T) {
	cases := []struct {
		streak int
		want   int
	}{
		{7, 0},
		{14, 1},
		{30, 2},
		{42, 3},
		{60, 4},
		{90, 5},
		{180, 6},
		{365, 7},
		{420, 8},
		{500, 9},
		{1000, 10},
		{499, -1},
		{999, -1},
		{0, -1},
		{6, -1},
		{8, -1},
		{21, -1}, // старый порог, больше не действует
		{50, -1}, // старый порог, больше не действует
		{100, -1}, // старый порог, больше не действует
	}
	for _, c := range cases {
		if got := StreakAchievementIndex(c.streak); got != c.want {
			t.Errorf("StreakAchievementIndex(%d) = %d, want %d", c.streak, got, c.want)
		}
	}
}

func TestLevelNames(t *testing.T) {
	expected := []string{"", "Meerkat", "Gazelle", "Zebra", "Cheetah", "Lion", "Elephant"}
	if len(LevelNames) != len(expected) {
		t.Fatalf("len(LevelNames) = %d, want %d", len(LevelNames), len(expected))
	}
	for i, name := range expected {
		if LevelNames[i] != name {
			t.Errorf("LevelNames[%d] = %q, want %q", i, LevelNames[i], name)
		}
	}
}

func TestLevelName(t *testing.T) {
	cases := []struct {
		level int
		want  string
	}{
		{1, "Meerkat"},
		{2, "Gazelle"},
		{3, "Zebra"},
		{4, "Cheetah"},
		{5, "Lion"},
		{6, "Elephant"},
		{0, ""},
		{7, ""},
		{-1, ""},
	}
	for _, c := range cases {
		if got := LevelName(c.level); got != c.want {
			t.Errorf("LevelName(%d) = %q, want %q", c.level, got, c.want)
		}
	}
}

func TestTrainingCupsFromParts_extra(t *testing.T) {
	cases := []struct {
		dur   int
		inten int
		cat   string
		want  int
	}{
		{10, 3, "run", 7},       // 10*3*1.2/5 = 7.2 → 7
		{1, 1, "yoga", 1},       // 1*1*0.8/5 = 0.16 → min 1
		{0, 1, "run", 1},        // d clamped to 1; 1*1*1.2/5 = 0.24 → min 1
		{500, 1, "run", 115},    // d clamped to 480; 480*1.2/5 = 115.2 → 115
		{25, 5, "hiit", 38},     // 25*5*1.5/5 = 37.5 → 38
		{20, 2, "walk", 6},      // 20*2*0.8/5 = 6.4 → 6
		{60, 5, "crossfit", 90}, // 60*5*1.5/5 = 90
	}
	for _, c := range cases {
		if got := TrainingCupsFromParts(c.dur, c.inten, c.cat); got != c.want {
			t.Errorf("TrainingCupsFromParts(%d, %d, %q) = %d, want %d", c.dur, c.inten, c.cat, got, c.want)
		}
	}
}
