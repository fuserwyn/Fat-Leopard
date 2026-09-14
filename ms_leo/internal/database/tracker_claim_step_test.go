package database

import (
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func TestClaimTrackerStep(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	d := &Database{db: db}

	mock.ExpectExec(`UPDATE pack_tracker_tasks`).
		WithArgs(int64(109), "уведомили о выкате").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE pack_tracker_tasks`).
		WithArgs(int64(109), "уведомили о выкате").
		WillReturnResult(sqlmock.NewResult(0, 0))

	first, err := d.ClaimTrackerStep(109, "уведомили о выкате")
	if err != nil || !first {
		t.Fatalf("первый контейнер должен забрать отметку: %v, %v", first, err)
	}
	second, err := d.ClaimTrackerStep(109, "уведомили о выкате")
	if err != nil || second {
		t.Fatalf("второй контейнер не должен слать повтор: %v, %v", second, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
