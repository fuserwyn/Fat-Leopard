package leopardmoney

import "testing"

func TestTrainingCupsFromParts_specExamples(t *testing.T) {
	cases := []struct {
		name     string
		d, in    int
		cat      string
		wantCups int
	}{
		{"1min int1 strength", 1, 1, "strength", 1},
		{"5min int1 yoga", 5, 1, "yoga", 1},
		{"30min int3 yoga", 30, 3, "yoga", 14},
		{"60min int3 strength", 60, 3, "strength", 36},
		{"45min int4 strength", 45, 4, "strength", 36},
		{"60min int4 run", 60, 4, "run", 58},
		{"30min int5 hiit", 30, 5, "hiit", 45},
		{"90min int5 hiit", 90, 5, "hiit", 135},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := TrainingCupsFromParts(tc.d, tc.in, tc.cat)
			if got != tc.wantCups {
				t.Fatalf("got %d want %d", got, tc.wantCups)
			}
		})
	}
}

func TestParseTrainingDoneReport(t *testing.T) {
	text := "силовая, 60 мин, инт. 3/5\nnotes"
	d, in, cat, ok := ParseTrainingDoneReport(text)
	if !ok || d != 60 || in != 3 || cat != "strength" {
		t.Fatalf("got ok=%v d=%d in=%d cat=%q", ok, d, in, cat)
	}
	if TrainingCupsFromParts(d, in, cat) != 36 {
		t.Fatal("cups for 60/3/strength")
	}
	legacy := "#training_done — бег, 15 мин, инт. 2/5"
	d2, in2, cat2, ok2 := ParseTrainingDoneReport(legacy)
	if !ok2 || d2 != 15 || in2 != 2 || cat2 != "run" {
		t.Fatalf("legacy parse: ok=%v d=%d in=%d cat=%q", ok2, d2, in2, cat2)
	}
	full := "бег, 15 мин, интенсивность 3/5"
	d3, in3, cat3, ok3 := ParseTrainingDoneReport(full)
	if !ok3 || d3 != 15 || in3 != 3 || cat3 != "run" {
		t.Fatalf("full intensity label: ok=%v d=%d in=%d cat=%q", ok3, d3, in3, cat3)
	}
}

func TestParseTrainingDoneReport_multiSport(t *testing.T) {
	// Кубки — за самый эффективный вид (макс. коэффициент): плавание (1.2) против йоги (0.8).
	text := "йога + плавание, 30 мин, инт. 3/5"
	d, in, cat, ok := ParseTrainingDoneReport(text)
	if !ok || d != 30 || in != 3 || cat != "swim" {
		t.Fatalf("got ok=%v d=%d in=%d cat=%q (want swim)", ok, d, in, cat)
	}
	// 30×3×1.2/5 = 21.6 → 22 кубка (за плавание), а не 14 (за йогу).
	if got := TrainingCupsFromReportText(text); got != 22 {
		t.Fatalf("cups=%d want 22 (best of йога/плавание)", got)
	}

	// Все виды доступны для подсказки в исходном порядке, дубли схлопнуты.
	_, _, cats, ok := ParseTrainingDoneReportCategories("плавание + бег + плавание, 20 мин, инт. 2/5")
	if !ok || len(cats) != 2 || cats[0] != "swim" || cats[1] != "run" {
		t.Fatalf("categories=%v ok=%v", cats, ok)
	}

	// «+» с hiit (1.5) даёт максимум независимо от порядка.
	if got := TrainingCupsFromReportText("ходьба + hiit, 30 мин, инт. 5/5"); got != 45 {
		t.Fatalf("cups=%d want 45 (hiit best)", got)
	}
}

func TestAggregateTrainingCategoryCounts(t *testing.T) {
	texts := []string{
		"бег, 15 мин, инт. 2/5",
		"бег + плавание, 30 мин, инт. 3/5",
		"просто текст без формата",
	}
	got := AggregateTrainingCategoryCounts(texts)
	if got["run"] != 2 || got["swim"] != 1 || got["other"] != 1 {
		t.Fatalf("counts=%v want run=2 swim=1 other=1", got)
	}
}

func TestLevelFromTotalCups(t *testing.T) {
	if LevelFromTotalCups(0) != 1 || LevelFromTotalCups(419) != 1 {
		t.Fatal("L1")
	}
	if LevelFromTotalCups(420) != 2 || LevelFromTotalCups(1259) != 2 {
		t.Fatal("L2")
	}
	if LevelFromTotalCups(13020) != 6 || LevelFromTotalCups(50000) != 6 {
		t.Fatal("L6 max")
	}
}
