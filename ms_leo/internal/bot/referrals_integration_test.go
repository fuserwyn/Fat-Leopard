package bot

import (
	"strings"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// startWith — пользователь жмёт /start с аргументом в личке бота.
func startWith(b *Bot, userID int64, arg string) {
	text := "/start"
	if arg != "" {
		text += " " + arg
	}
	b.handleStart(&tgbotapi.Message{
		From:     &tgbotapi.User{ID: userID, FirstName: "Друг"},
		Chat:     &tgbotapi.Chat{ID: userID, Type: "private"},
		Text:     text,
		Entities: []tgbotapi.MessageEntity{{Type: "bot_command", Offset: 0, Length: len("/start")}},
	})
}

// Друг по личной ссылке засчитывается при переходе, но награда — только когда
// десятый такой друг запишет первую тренировку.
func TestReferralRewardAfterTenFirstWorkouts(t *testing.T) {
	b, tg, db := newIntegrationBot(t, nil)
	seedMember(t, db, itUser, "leopard", false)

	v, err := b.GetReferralForAPI(itUser)
	if err != nil {
		t.Fatal(err)
	}
	if v.Link != "https://t.me/leo_it_bot?start=ref-555001" || v.Joined != 0 || v.NextIn != ReferralRewardEvery {
		t.Fatalf("пустая ссылка: %+v", v)
	}
	baseMax := b.GetMiniappProfileStatsForAPI(itUser, itPack).StreakSaveAttemptsMax

	const firstFriend = int64(777000)
	for i := int64(0); i < ReferralRewardEvery; i++ {
		startWith(b, firstFriend+i, "ref-555001")
	}
	// Повторный старт, старт по чужой ссылке и свой собственный не засчитываются.
	startWith(b, firstFriend, "ref-555001")
	startWith(b, itUser, "ref-555001")
	seedMember(t, db, 888001, "other", false)
	startWith(b, firstFriend+1, "ref-888001")

	v, _ = b.GetReferralForAPI(itUser)
	if v.Joined != ReferralRewardEvery || v.Qualified != 0 || v.Rewards != 0 {
		t.Fatalf("после переходов: %+v", v)
	}
	if got := b.GetMiniappProfileStatsForAPI(itUser, itPack).StreakSaveAttemptsMax; got != baseMax {
		t.Fatalf("за переходы награды нет: кап %d, был %d", got, baseMax)
	}

	// Старичок по ссылке не засчитывается.
	startWith(b, 888001, "ref-555001")
	if v, _ = b.GetReferralForAPI(itUser); v.Joined != ReferralRewardEvery {
		t.Fatalf("старичок засчитан: %+v", v)
	}

	for i := int64(0); i < ReferralRewardEvery-1; i++ {
		logWorkout(b, firstFriend+i, "#training_done пробежка")
	}
	// Вторая тренировка того же друга ничего не добавляет.
	logWorkout(b, firstFriend, "#training_done ещё")
	v, _ = b.GetReferralForAPI(itUser)
	if v.Qualified != ReferralRewardEvery-1 || v.Rewards != 0 || v.NextIn != 1 {
		t.Fatalf("девять с тренировкой: %+v", v)
	}
	tg.reset()

	logWorkout(b, firstFriend+ReferralRewardEvery-1, "#training_done пробежка")
	v, _ = b.GetReferralForAPI(itUser)
	if v.Qualified != ReferralRewardEvery || v.Rewards != 1 || v.NextIn != ReferralRewardEvery {
		t.Fatalf("десять с тренировкой: %+v", v)
	}
	if got := b.GetMiniappProfileStatsForAPI(itUser, itPack).StreakSaveAttemptsMax; got != baseMax+1 {
		t.Fatalf("кап после награды %d, ждали %d", got, baseMax+1)
	}
	notified := false
	for _, s := range tg.textsTo(itUser) {
		if strings.Contains(s, "попытку спасти стрик") {
			notified = true
		}
	}
	if !notified {
		t.Errorf("пригласившему не сообщили о награде: %v", tg.textsTo(itUser))
	}
}

// Друг сначала открыл мини-апп из профиля бота (событие) и бывал в боте без ссылки
// (визит), но в стае ещё не был — переход по ссылке всё равно засчитывается.
func TestReferralCountsFriendWhoOpenedMiniappFirst(t *testing.T) {
	b, _, db := newIntegrationBot(t, nil)
	seedMember(t, db, itUser, "leopard", false)

	const friend = int64(779100)
	if _, err := db.Exec(`INSERT INTO events (event_name, user_id, telegram_id) VALUES ('miniapp_opened', $1, $1)`, friend); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO bot_visits (user_id) VALUES ($1)`, friend); err != nil {
		t.Fatal(err)
	}
	startWith(b, friend, "ref-555001")

	v, err := b.GetReferralForAPI(itUser)
	if err != nil {
		t.Fatal(err)
	}
	if v.Joined != 1 {
		t.Fatalf("друг после мини-аппа не засчитан: %+v", v)
	}
	// Он уже в стае — повторный переход по ссылке ничего не меняет.
	startWith(b, friend, "ref-555001")
	if v, _ = b.GetReferralForAPI(itUser); v.Joined != 1 {
		t.Fatalf("повторный переход засчитан: %+v", v)
	}
}

func TestParseReferralStartPayload(t *testing.T) {
	cases := map[string]int64{
		"ref-42": 42, " ref-555001 ": 555001, "ref-": 0, "ref-abc": 0, "ref--5": 0, "src-tg": 0, "": 0, "ch-days7": 0,
	}
	for in, want := range cases {
		if got := parseReferralStartPayload(in); got != want {
			t.Errorf("parseReferralStartPayload(%q) = %d, ждали %d", in, got, want)
		}
	}
	if referralLink("", 1) != "" || referralLink("bot", 0) != "" {
		t.Error("ссылка без бота или id должна быть пустой")
	}
	for q, want := range map[int]int{-1: 0, 0: 0, 9: 0, 10: 1, 25: 2} {
		if got := ReferralRewardAttempts(q); got != want {
			t.Errorf("ReferralRewardAttempts(%d) = %d, ждали %d", q, got, want)
		}
	}
	if referralNextIn(-3) != ReferralRewardEvery || referralNextIn(13) != 7 {
		t.Error("referralNextIn")
	}
	var nilBot *Bot
	if nilBot.rememberReferralFromStart(1, "ref-2") || nilBot.referralRewardBonus(1) != 0 {
		t.Error("nil-бот")
	}
	nilBot.referralOnTraining(1)
	if v, err := nilBot.GetReferralForAPI(1); err != nil || v.Link != "" {
		t.Errorf("nil-бот: %+v %v", v, err)
	}
}
