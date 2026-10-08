package bot

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"leo-bot/internal/utils"

	initdata "github.com/telegram-mini-apps/init-data-golang"
)

func mskDaysAgo(n int) string {
	return utils.GetMoscowTime().AddDate(0, 0, -n).Format("2006-01-02")
}

type challengeRow struct {
	Status      string
	Days        int
	LastCounted string
	Finished    bool
}

func challengeOf(t *testing.T, db *sql.DB, userID int64) challengeRow {
	t.Helper()
	var r challengeRow
	var last sql.NullString
	if err := db.QueryRow(`
		SELECT status, days_done, last_counted_date::text, finished_at IS NOT NULL
		FROM challenge_participants WHERE user_id = $1 ORDER BY id DESC LIMIT 1`, userID).
		Scan(&r.Status, &r.Days, &last, &r.Finished); err != nil {
		t.Fatal(err)
	}
	r.LastCounted = last.String
	return r
}

// setChallengeProgress — участник прошёл days дней, последний засчитан daysAgo дней назад.
func setChallengeProgress(t *testing.T, db *sql.DB, userID int64, days, daysAgo int) {
	t.Helper()
	if _, err := db.Exec(`UPDATE challenge_participants SET days_done = $2, last_counted_date = $3
		WHERE user_id = $1 AND status = 'active'`, userID, days, mskDaysAgo(daysAgo)); err != nil {
		t.Fatal(err)
	}
}

func acceptChallenge(t *testing.T, b *Bot, userID int64, code string) ChallengeParticipationView {
	t.Helper()
	v, err := b.AcceptChallengeForViewer(userID, initdata.InitData{}, code)
	if err != nil {
		t.Fatalf("принять %s: %v", code, err)
	}
	return v
}

// Стандартные челленджи заводит миграция; принятый засчитывает день за тренировкой,
// второй активный взять нельзя, последний день закрывает челлендж.
func TestChallengeAcceptCountAndComplete(t *testing.T) {
	b, tg, db := newIntegrationBot(t, nil)
	seedMember(t, db, itUser, "leopard", false)

	st, err := b.GetChallengesStateForViewer(itUser, initdata.InitData{})
	if err != nil {
		t.Fatal(err)
	}
	var lengths []int
	for _, c := range st.Standard {
		lengths = append(lengths, c.LengthDays)
		if c.Custom || !strings.HasPrefix(c.Link, "https://t.me/leo_it_bot?start=ch-") {
			t.Errorf("стандартный челлендж: %+v", c)
		}
	}
	if len(lengths) != len(StandardChallengeLengths) {
		t.Fatalf("стандартные длины: %v", lengths)
	}
	for i, n := range StandardChallengeLengths {
		if lengths[i] != n {
			t.Fatalf("стандартные длины: %v", lengths)
		}
	}
	if st.Active != nil || st.CanCreate || st.Invite != nil {
		t.Errorf("новичок: %+v", st)
	}

	if _, err := b.AcceptChallengeForViewer(itUser, initdata.InitData{}, "nope"); !errors.Is(err, ErrChallengeNotFound) {
		t.Errorf("несуществующий код: %v", err)
	}
	v := acceptChallenge(t, b, itUser, "days7")
	if v.Status != "active" || v.DaysDone != 0 || v.StartDate != mskDaysAgo(0) {
		t.Errorf("принят: %+v", v)
	}
	if _, err := b.AcceptChallengeForViewer(itUser, initdata.InitData{}, "days14"); !errors.Is(err, ErrChallengeAlreadyActive) {
		t.Errorf("второй активный: %v", err)
	}

	logWorkout(b, itUser, "#training_done бег")
	logWorkout(b, itUser, "#training_done ещё раз")
	if r := challengeOf(t, db, itUser); r.Status != "active" || r.Days != 1 || r.LastCounted != mskDaysAgo(0) {
		t.Errorf("первый день (повтор не считается): %+v", r)
	}

	// Шесть дней позади, вчера — шестой; сегодняшняя тренировка — седьмой.
	setChallengeProgress(t, db, itUser, 6, 1)
	setTrainingHistory(t, db, itUser, 6, 6, 1)
	tg.reset()
	logWorkout(b, itUser, "#training_done финиш")
	if r := challengeOf(t, db, itUser); r.Status != "completed" || r.Days != 7 || !r.Finished {
		t.Errorf("последний день: %+v", r)
	}
	if !strings.Contains(strings.Join(tg.textsTo(itUser), "\n"), "пройден") {
		t.Errorf("сообщение о победе: %v", tg.textsTo(itUser))
	}
	eventually(t, "событие challenge_completed", func() bool {
		return count(t, db, `SELECT COUNT(*) FROM events WHERE event_name = 'challenge_completed' AND telegram_id = $1`, itUser) == 1
	})

	// Пройденный — в истории, новый можно брать сразу; сегодня уже тренировался — день засчитан.
	st, _ = b.GetChallengesStateForViewer(itUser, initdata.InitData{})
	if st.Active != nil || len(st.History) != 1 || st.History[0].Status != "completed" || st.History[0].FinishedAt == "" {
		t.Errorf("после победы: %+v", st)
	}
	if v := acceptChallenge(t, b, itUser, "days14"); v.DaysDone != 1 || v.LastCountedDate != mskDaysAgo(0) {
		t.Errorf("тренировка сегодня засчитана при принятии: %+v", v)
	}
}

// Пропуск, при котором стрик сгорел, проваливает челлендж; попытка спасения
// и больничный — нет.
func TestChallengeMissedDayRules(t *testing.T) {
	b, _, db := newIntegrationBot(t, nil)
	const (
		burned = itUser
		saved  = itUser + 1
		sick   = itUser + 2
	)
	for _, id := range []int64{burned, saved, sick} {
		seedMember(t, db, id, "leopard", false)
		acceptChallenge(t, b, id, "days30")
	}

	// Последний засчитан 3 дня назад, тренировок с тех пор нет — стрик сгорел.
	setChallengeProgress(t, db, burned, 5, 3)
	setTrainingHistory(t, db, burned, 5, 5, 3)
	logWorkout(b, burned, "#training_done вернулся")
	if r := challengeOf(t, db, burned); r.Status != "failed" || r.Days != 5 || !r.Finished {
		t.Errorf("сгоревший стрик: %+v", r)
	}
	eventually(t, "событие challenge_failed", func() bool {
		return count(t, db, `SELECT COUNT(*) FROM events WHERE event_name = 'challenge_failed' AND telegram_id = $1`, burned) == 1
	})
	// Провален — новый можно взять сразу.
	acceptChallenge(t, b, burned, "days7")

	// Позавчера засчитан, вчера пропуск закрыт попыткой (last_training_date = вчера).
	setChallengeProgress(t, db, saved, 5, 2)
	setTrainingHistory(t, db, saved, 5, 5, 1)
	logWorkout(b, saved, "#training_done после попытки")
	if r := challengeOf(t, db, saved); r.Status != "active" || r.Days != 6 || r.LastCounted != mskDaysAgo(0) {
		t.Errorf("попытка спасла: %+v", r)
	}

	// Больничный с позавчера: пропуски не жгут ни стрик, ни челлендж.
	setChallengeProgress(t, db, sick, 5, 4)
	setTrainingHistory(t, db, sick, 5, 5, 4)
	sickStart := utils.FormatMoscowTime(utils.GetMoscowTime().AddDate(0, 0, -3))
	if _, err := db.Exec(`UPDATE training_state SET has_sick_leave = TRUE, has_healthy = FALSE, sick_leave_start_time = $2 WHERE user_id = $1`, sick, sickStart); err != nil {
		t.Fatal(err)
	}
	if n := b.sweepChallenges(); n != 0 {
		t.Errorf("обход не должен проваливать больничного: %d", n)
	}
	logWorkout(b, sick, "#training_done после болезни")
	if r := challengeOf(t, db, sick); r.Status != "active" || r.Days != 6 {
		t.Errorf("больничный: %+v", r)
	}
}

// Без тренировок челлендж проваливается обходом или при открытии экрана, но не
// раньше, чем закроется окно спасения стрика.
func TestChallengeIdleCheck(t *testing.T) {
	b, tg, db := newIntegrationBot(t, nil)
	const (
		gone  = itUser
		risky = itUser + 1
		fresh = itUser + 2
	)
	for _, id := range []int64{gone, risky, fresh} {
		seedMember(t, db, id, "leopard", false)
		acceptChallenge(t, b, id, "days14")
	}
	setChallengeProgress(t, db, gone, 4, 4)
	setTrainingHistory(t, db, gone, 4, 4, 4)
	// Пропустил вчера: стрик сгорел, но попыткой ещё спасается.
	setChallengeProgress(t, db, risky, 4, 2)
	setTrainingHistory(t, db, risky, 4, 4, 2)

	tg.reset()
	if n := b.sweepChallenges(); n != 1 {
		t.Errorf("провалено обходом: %d", n)
	}
	if r := challengeOf(t, db, gone); r.Status != "failed" {
		t.Errorf("давно не тренировался: %+v", r)
	}
	if !strings.Contains(strings.Join(tg.textsTo(gone), "\n"), "прерван") {
		t.Errorf("сообщение о провале: %v", tg.textsTo(gone))
	}
	st, err := b.GetChallengesStateForViewer(risky, initdata.InitData{})
	if err != nil {
		t.Fatal(err)
	}
	if st.Active == nil || !st.Active.AtRisk || st.Active.DaysDone != 4 {
		t.Errorf("под угрозой: %+v", st.Active)
	}
	// Ещё не начатый челлендж не проваливается.
	if st, _ := b.GetChallengesStateForViewer(fresh, initdata.InitData{}); st.Active == nil || st.Active.AtRisk {
		t.Errorf("не начатый: %+v", st.Active)
	}

	// Бросить можно только активный.
	if err := b.LeaveChallengeForViewer(risky, initdata.InitData{}); err != nil {
		t.Fatal(err)
	}
	if r := challengeOf(t, db, risky); r.Status != "failed" {
		t.Errorf("брошен: %+v", r)
	}
	if err := b.LeaveChallengeForViewer(risky, initdata.InitData{}); !errors.Is(err, ErrChallengeNotActive) {
		t.Errorf("бросить нечего: %v", err)
	}
}

// Ссылка ch-<код> в /start: челлендж запоминается и предлагается в мини-аппе,
// источник — в аналитике, сам челлендж не стартует.
func TestChallengeInviteFromStartLink(t *testing.T) {
	b, tg, db := newIntegrationBot(t, nil)

	b.handleUpdate(commandUpdate(itUser, "friend", "/start ch-days30"))

	if n := count(t, db, `SELECT COUNT(*) FROM challenge_participants WHERE user_id = $1`, itUser); n != 0 {
		t.Fatalf("челлендж не должен стартовать сам: %d", n)
	}
	if !strings.Contains(strings.Join(tg.textsTo(itUser), "\n"), "30 дней подряд") {
		t.Errorf("приветствие упоминает челлендж: %v", tg.textsTo(itUser))
	}
	eventually(t, "источник ch-days30", func() bool {
		return count(t, db, `SELECT COUNT(*) FROM events WHERE event_name = 'bot_started' AND telegram_id = $1 AND source = 'ch-days30'`, itUser) == 1
	})
	st, err := b.GetChallengesStateForViewer(itUser, initdata.InitData{})
	if err != nil {
		t.Fatal(err)
	}
	if st.Invite == nil || st.Invite.Code != "days30" {
		t.Fatalf("приглашение: %+v", st.Invite)
	}
	if err := b.DismissChallengeInviteForViewer(itUser, initdata.InitData{}); err != nil {
		t.Fatal(err)
	}
	if st, _ := b.GetChallengesStateForViewer(itUser, initdata.InitData{}); st.Invite != nil {
		t.Errorf("отклонено: %+v", st.Invite)
	}

	// Неизвестный код ничего не запоминает; принятие приглашения его снимает.
	b.handleUpdate(commandUpdate(itUser, "friend", "/start ch-unknown1"))
	if n := count(t, db, `SELECT COUNT(*) FROM challenge_invites WHERE user_id = $1`, itUser); n != 0 {
		t.Errorf("неизвестный код: %d", n)
	}
	b.handleUpdate(commandUpdate(itUser, "friend", "/start ch-days7"))
	acceptChallenge(t, b, itUser, "days7")
	if n := count(t, db, `SELECT COUNT(*) FROM challenge_invites WHERE user_id = $1`, itUser); n != 0 {
		t.Errorf("принятое приглашение остаётся: %d", n)
	}
}

// Свой челлендж — только после пройденного 100-дневного; длина 3–365, название
// до 40 символов, модерация как у комментариев. По его ссылке приходят друзья.
func TestChallengeCreateCustom(t *testing.T) {
	b, _, db := newIntegrationBot(t, nil)
	seedMember(t, db, itUser, "leopard", false)
	seedMember(t, db, itUser+1, "friend", false)
	none := initdata.InitData{}

	if _, err := b.CreateChallengeForViewer(itUser, none, "Свой", 10); !errors.Is(err, ErrChallengeCreateForbidden) {
		t.Fatalf("без 100 дней: %v", err)
	}
	acceptChallenge(t, b, itUser, "days100")
	setChallengeProgress(t, db, itUser, 99, 1)
	setTrainingHistory(t, db, itUser, 99, 99, 1)
	logWorkout(b, itUser, "#training_done сотый день")
	if r := challengeOf(t, db, itUser); r.Status != "completed" {
		t.Fatalf("100 дней: %+v", r)
	}

	for _, n := range []int{2, 366} {
		if _, err := b.CreateChallengeForViewer(itUser, none, "Свой", n); !errors.Is(err, ErrChallengeBadLength) {
			t.Errorf("длина %d: %v", n, err)
		}
	}
	for _, title := range []string{" ", strings.Repeat("а", 41)} {
		if _, err := b.CreateChallengeForViewer(itUser, none, title, 10); !errors.Is(err, ErrChallengeBadTitle) {
			t.Errorf("название %q: %v", title, err)
		}
	}
	var mod *ModerationBlockedError
	if _, err := b.CreateChallengeForViewer(itUser, none, "бегаем блять", 10); !errors.As(err, &mod) {
		t.Errorf("модерация: %v", err)
	}

	c, err := b.CreateChallengeForViewer(itUser, none, "  Планка\nкаждый день ", 21)
	if err != nil {
		t.Fatal(err)
	}
	if c.Title != "Планка каждый день" || c.LengthDays != 21 || !c.Custom || c.Link != "https://t.me/leo_it_bot?start=ch-"+c.Code {
		t.Errorf("свой челлендж: %+v", c)
	}
	st, _ := b.GetChallengesStateForViewer(itUser, none)
	if !st.CanCreate || len(st.Mine) != 1 || st.Mine[0].Code != c.Code {
		t.Errorf("экран автора: %+v", st)
	}
	eventually(t, "событие challenge_created", func() bool {
		return count(t, db, `SELECT COUNT(*) FROM events WHERE event_name = 'challenge_created' AND telegram_id = $1`, itUser) == 1
	})

	// Друг по ссылке принимает чужой челлендж.
	b.handleUpdate(commandUpdate(itUser+1, "friend", "/start ch-"+c.Code))
	if st, _ := b.GetChallengesStateForViewer(itUser+1, none); st.Invite == nil || st.Invite.Title != c.Title || st.CanCreate {
		t.Errorf("приглашение другу: %+v", st)
	}
	if v := acceptChallenge(t, b, itUser+1, c.Code); v.Challenge.LengthDays != 21 || !v.Challenge.Custom {
		t.Errorf("друг принял: %+v", v)
	}
}
