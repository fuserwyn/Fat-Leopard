package bot

import (
	"testing"
	"time"
)

func mskTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.ParseInLocation("2006-01-02 15:04", s, time.FixedZone("MSK", 3*3600))
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

func TestLocalDailySendDue_LocalHour(t *testing.T) {
	// Напоминание на 23:00. В Белграде (МСК-2) 23:00 по МСК — это 21:00, рано.
	if due, _ := localDailySendDue(mskTime(t, "2026-10-29 23:00"), -2, 23, 0, ""); due {
		t.Fatal("Belgrade: 23:00 MSK is 21:00 local, must not fire")
	}
	// 01:00 МСК следующего дня — 23:00 по Белграду: шлём, дата — локальная (29-е).
	due, day := localDailySendDue(mskTime(t, "2026-10-30 01:10"), -2, 23, 0, "")
	if !due || day != "2026-10-29" {
		t.Fatalf("Belgrade 23:10 local: due=%v day=%q", due, day)
	}
	// Москва: 23:00 МСК.
	due, day = localDailySendDue(mskTime(t, "2026-10-29 23:00"), 0, 23, 0, "")
	if !due || day != "2026-10-29" {
		t.Fatalf("Moscow: due=%v day=%q", due, day)
	}
}

func TestLocalDailySendDue_MinuteWindowAndDedup(t *testing.T) {
	if due, _ := localDailySendDue(mskTime(t, "2026-10-29 04:19"), 0, 4, 20, ""); due {
		t.Fatal("04:19 is before 04:20 window")
	}
	if due, _ := localDailySendDue(mskTime(t, "2026-10-29 04:59"), 0, 4, 20, ""); !due {
		t.Fatal("04:59 is inside window")
	}
	if due, _ := localDailySendDue(mskTime(t, "2026-10-29 04:30"), 0, 4, 20, " 2026-10-29 "); due {
		t.Fatal("already sent today must not fire again")
	}
}

func TestLocalDailySendDue_MoveBelgradeToMoscow(t *testing.T) {
	// В Белграде 28-го в 23:00 местного (= 01:00 МСК 29-го) напоминание ушло, отмечено 28-е.
	due, sentDay := localDailySendDue(mskTime(t, "2026-10-29 01:00"), -2, 23, 0, "")
	if !due || sentDay != "2026-10-28" {
		t.Fatalf("belgrade send: due=%v day=%q", due, sentDay)
	}
	// 29-го вернулся в Москву: в 23:00 МСК напоминание должно прийти (по МСК-дате оно бы потерялось).
	if due, _ := localDailySendDue(mskTime(t, "2026-10-29 23:00"), 0, 23, 0, sentDay); !due {
		t.Fatal("after returning to Moscow reminder must fire")
	}
}

func TestLocalDailySendDue_MoveEast(t *testing.T) {
	// Мудрость в 04:20 по Москве 29-го ушла; перелёт в UTC+8 (МСК+5).
	_, sentDay := localDailySendDue(mskTime(t, "2026-10-29 04:20"), 0, 4, 20, "")
	// 04:20 30-го по местному = 23:20 МСК 29-го — новая локальная дата, шлём.
	due, day := localDailySendDue(mskTime(t, "2026-10-29 23:20"), 5, 4, 20, sentDay)
	if !due || day != "2026-10-30" {
		t.Fatalf("east: due=%v day=%q", due, day)
	}
}
