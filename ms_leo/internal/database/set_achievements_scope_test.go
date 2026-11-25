package database

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"leo-bot/internal/game/leopardmoney"
)

func TestSetAchievementsForUserScope_Clamp(t *testing.T) {
	cases := []struct {
		name      string
		count     int
		level     int
		wantCount int
		wantLevel int
	}{
		{"all streak achievements up to 1000 days", leopardmoney.MaxAchievements, 1000, leopardmoney.MaxAchievements, 1000},
		{"above max is clamped", 99, 1000, leopardmoney.MaxAchievements, 1000},
		{"negative is clamped to zero", -3, -7, 0, 0},
		{"500-day milestone", 10, 500, 10, 500},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
			if err != nil {
				t.Fatalf("sqlmock.New: %v", err)
			}
			defer db.Close()
			d := &Database{db: db}

			mock.ExpectExec(`UPDATE training_state\s+SET achievement_count`).
				WithArgs(int64(5), int64(-100), c.wantCount, c.wantLevel, sqlmock.AnyArg()).
				WillReturnResult(sqlmock.NewResult(0, 2))

			if err := d.SetAchievementsForUserScope(5, -100, c.count, c.level); err != nil {
				t.Fatalf("SetAchievementsForUserScope: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("sqlmock: %v", err)
			}
		})
	}
}

func TestSetAchievementsForUserScope_NoopOnZeroIDs(t *testing.T) {
	d := &Database{}
	if err := d.SetAchievementsForUserScope(0, -100, 3, 30); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if err := d.SetAchievementsForUserScope(5, 0, 3, 30); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}
