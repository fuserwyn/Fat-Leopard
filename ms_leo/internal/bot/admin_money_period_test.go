package bot

import (
	"errors"
	"testing"
	"time"
)

func TestAdminMoneyPeriod(t *testing.T) {
	from, to, fs, ts, err := adminMoneyPeriod("2026-09-30", "2026-09-01")
	if err != nil {
		t.Fatal(err)
	}
	if fs != "2026-09-01" || ts != "2026-09-30" {
		t.Errorf("даты не поменялись местами: %q %q", fs, ts)
	}
	wantFrom := time.Date(2026, 9, 1, 0, 0, 0, 0, adminMoneyMSK)
	wantTo := time.Date(2026, 10, 1, 0, 0, 0, 0, adminMoneyMSK)
	if !from.Equal(wantFrom) || !to.Equal(wantTo) {
		t.Errorf("границы: %v %v", from, to)
	}

	from, to, fs, ts, err = adminMoneyPeriod("", "")
	if err != nil || !from.IsZero() || !to.IsZero() || fs != "" || ts != "" {
		t.Errorf("без периода: %v %v %q %q %v", from, to, fs, ts, err)
	}

	if _, _, _, _, err := adminMoneyPeriod("01.09.2026", ""); !errors.Is(err, ErrAdminActionInvalid) {
		t.Errorf("кривая дата должна давать ErrAdminActionInvalid: %v", err)
	}
}

func TestAdminMoneyPeriodLabel(t *testing.T) {
	cases := map[[2]string]string{
		{"", ""}:                     "за всё время",
		{"2026-09-01", "2026-09-30"}: "за 01.09.26–30.09.26",
		{"2026-09-05", "2026-09-05"}: "за 05.09.26",
		{"2026-09-01", ""}:           "с 01.09.26",
		{"", "2026-09-30"}:           "по 30.09.26",
	}
	for in, want := range cases {
		if got := adminMoneyPeriodLabel(in[0], in[1]); got != want {
			t.Errorf("%v: %q, ждали %q", in, got, want)
		}
	}
}
