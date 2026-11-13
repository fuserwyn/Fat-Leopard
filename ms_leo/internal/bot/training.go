package bot

import (
	"fmt"
	"strings"
	"time"

	"leo-bot/internal/domain"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// TrainingOutcome описывает результат первого отчёта #training_done за день.
type TrainingOutcome struct {
	EarnRewards      bool
	NewStreakDays    int
	Weekly           bool // 7 дней
	TwoWeek          bool // 14 дней
	ThreeWeek        bool // 21 день
	Monthly          bool // 30 дней
	FortyTwo         bool // 42 дня
	Fifty            bool // 50 дней
	Sixty            bool // 60 дней
	Quarterly        bool // 90 дней
	Hundred          bool // 100 дней
	OneHundredEighty bool // 180 дней
	TwoHundred       bool // 200 дней
	TwoHundredForty  bool // 240 дней
}

// ComputeStreakDays — единое правило пересчёта стрика при засчитанном отчёте о тренировке.
// Используется и для предпросмотра (calculateTrainingDayOutcome), и для фактического обновления
// (leopard_money_training_done.go), чтобы эти пути не расходились.
//
//	lastTrainingDate — дата последнего засчитанного отчёта (YYYY-MM-DD в локальном TZ пользователя),
//	  nil или пустая строка, если отчётов ещё не было.
//	prevStreak — текущее значение streak_days.
//	today       — «сейчас» в локальном TZ пользователя.
//
// Возвращает новое значение стрика и sameDay=true, если отчёт повторный за тот же день
// (стрик не меняется, начисления делать не нужно).
func ComputeStreakDays(lastTrainingDate *string, prevStreak int, today time.Time) (newStreak int, sameDay bool) {
	todayStr := today.Format("2006-01-02")
	last := ""
	if lastTrainingDate != nil {
		last = strings.TrimSpace(*lastTrainingDate)
	}

	if last == todayStr {
		return prevStreak, true
	}
	if last == "" {
		// Отчётов ещё не было. Если стрик уже задан (например, начислен админом) — продолжаем его,
		// иначе начинаем с 1.
		if prevStreak > 0 {
			return prevStreak + 1, false
		}
		return 1, false
	}
	if last == today.AddDate(0, 0, -1).Format("2006-01-02") {
		return prevStreak + 1, false
	}
	// Пропуск ≥ 1 календарного дня — новый стрик начинается с 1 (см. EffectiveStreakDays для отображения 0 до отчёта).
	return 1, false
}

// EffectiveStreakDays — текущий стрик с учётом сгорания без нового отчёта.
//
// После последней тренировки есть один полный календарный день «на догон»; если в него не
// позанимались — в полночь следующего дня (daysSinceLastTraining >= 2) стрик = 0.
// daysSinceLastTraining == -1 — дата последней тренировки неизвестна (например, стрик начислен админом).
func EffectiveStreakDays(storedStreak, daysSinceLastTraining int) int {
	if storedStreak <= 0 {
		return 0
	}
	if daysSinceLastTraining >= 2 {
		return 0
	}
	return storedStreak
}

// calculateTrainingDayOutcome описывает, что произойдёт при следующем засчитанном отчёте #training_done
// в этот календарный день пользователя: первая тренировка дня — начисления и обновление стрика;
// повтор в тот же день — без начислений (EarnRewards=false).
func (b *Bot) calculateTrainingDayOutcome(messageLog *domain.MessageLog) TrainingOutcome {
	localNow := b.getUserLocalNow(messageLog.TimezoneOffsetFromMoscow)
	today := localNow.Format("2006-01-02")
	lastDate := messageLog.LastTrainingDate
	if messageLog.LastTrainingDate != nil {
		if ss, se, ok := b.sickWindowLocalDates(messageLog, today); ok {
			adj := sickAdjustedLastTrainingDate(*messageLog.LastTrainingDate, today, ss, se)
			lastDate = &adj
		}
	}
	newStreak, sameDay := ComputeStreakDays(lastDate, messageLog.StreakDays, localNow)
	if sameDay {
		return TrainingOutcome{EarnRewards: false, NewStreakDays: newStreak}
	}

	return TrainingOutcome{
		EarnRewards:      true,
		NewStreakDays:    newStreak,
		Weekly:           newStreak == 7,
		TwoWeek:          newStreak == 14,
		ThreeWeek:        newStreak == 21,
		Monthly:          newStreak == 30,
		FortyTwo:         newStreak == 42,
		Fifty:            newStreak == 50,
		Sixty:            newStreak == 60,
		Quarterly:        newStreak == 90,
		Hundred:          newStreak == 100,
		OneHundredEighty: newStreak == 180,
		TwoHundred:       newStreak == 200,
		TwoHundredForty:  newStreak == 240,
	}
}

func (b *Bot) sendStreakReward(
	msg *tgbotapi.Message,
	username string,
	streakDays int,
	rewardCups int,
	title string,
	subtitle string,
) {
	totalCups, err := b.db.GetUserCups(msg.From.ID, b.packTrainingStateChatID(msg))
	if err != nil {
		b.logger.Errorf("Failed to get total cups for %s reward: %v", title, err)
		totalCups = 0
	}

	messageText := fmt.Sprintf(`%s!

%s, streak: %d %s in a row.
%s

🏆 +%d %s (total: %d)`,
		title,
		username,
		streakDays,
		daysWordForm(streakDays),
		subtitle,
		rewardCups,
		cupsWordForm(rewardCups),
		totalCups,
	)

	reply := tgbotapi.NewMessage(msg.Chat.ID, messageText)
	if _, err := b.api.Send(reply); err != nil {
		b.logger.Errorf("Failed to send %s reward message: %v", title, err)
	}
}

type GenderForms struct {
	Champion    string // чемпион / чемпионка
	Accumulated string // накопил / накопила
	Transformed string // превратил / превратила
	Became      string // становился / становилась
	Ruler       string // владыка / владычица
	Warrior     string // воин / воительница
	Entered     string // вошёл / вошла
	Proved      string // доказал / доказала
	Deserved    string // заслужил / заслужила
	Titan       string // титан / титанисса
	Olympian    string // олимпийский чемпион / олимпийская чемпионка
	Invincible  string // непобедимый / непобедимая
	Reached     string // достиг / достигла
}

func (b *Bot) getGenderForms(gender string) GenderForms {
	_ = gender
	return GenderForms{
		Champion:    "champion",
		Accumulated: "earned",
		Transformed: "turned into",
		Became:      "became",
		Ruler:       "ruler",
		Warrior:     "warrior",
		Entered:     "entered",
		Proved:      "proved",
		Deserved:    "earned",
		Titan:       "titan",
		Olympian:    "Olympic champion",
		Invincible:  "unstoppable",
		Reached:     "reached",
	}
}

// getWordForm возвращает правильную форму слова "слов" в зависимости от числа
// 1, 21, 31, 41... → "слово"
// 2, 3, 4, 22, 23, 24, 32, 33, 34... → "слова"
// 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 25, 26... → "слов"
func getWordForm(count int) string {
	// Берем последнюю цифру и предпоследнюю для определения склонения
	lastDigit := count % 10
	secondLastDigit := (count / 10) % 10

	// Если предпоследняя цифра 1 (10-19), всегда "слов"
	if secondLastDigit == 1 {
		return "слов"
	}

	// Иначе смотрим на последнюю цифру
	switch lastDigit {
	case 1:
		return "слово"
	case 2, 3, 4:
		return "слова"
	default:
		return "слов"
	}
}

// russianPlural — одна из форм (ед., неск. 2–4, много) для положительного целого count.
func russianPlural(count int, one, few, many string) string {
	if count < 0 {
		count = -count
	}
	n := count % 100
	if n >= 11 && n <= 14 {
		return many
	}
	switch count % 10 {
	case 1:
		return one
	case 2, 3, 4:
		return few
	default:
		return many
	}
}

func trainingsWordForm(count int) string {
	if count == 1 {
		return "workout"
	}
	return "workouts"
}

func cupsWordForm(count int) string {
	if count == 1 {
		return "cup"
	}
	return "cups"
}

func daysWordForm(count int) string {
	if count == 1 {
		return "day"
	}
	return "days"
}

// streakMilestone описывает один milestone стрика: количество дней, бонусные кубки и текст для отправки.
type streakMilestone struct {
	Days     int
	Cups     int
	Title    string
	Subtitle string
}

// streakMilestones — единственное место где задаются пороги стрика и бонусы за них.
// Добавить новый milestone = одна строка в этом списке.
var streakMilestones = []streakMilestone{
	{7, 42, "🏆🏆🏆 INCREDIBLE! 🏆🏆🏆", "7 days — first milestone. You get stronger every day!"},
	{14, 42, "🏆🏆🏆🏆 IMPRESSIVE! 🏆🏆🏆🏆", "Two weeks without a miss — huge push!"},
	{21, 42, "🏆🏆🏆🏆🏆 SPLENDID! 🏆🏆🏆🏆🏆", "Three weeks — the habit is part of you!"},
	{30, 420, "🏆🏆🏆🏆🏆🏆 VICTORY MONTH! 🏆🏆🏆🏆🏆🏆", "A month of dedication — champion level!"},
	{42, 42, "🏆🏆🏆🏆🏆🏆 LEGENDARY! 🏆🏆🏆🏆🏆🏆", "42 days — legendary mark. You're on your way!"},
	{50, 42, "🏆🏆🏆🏆🏆🏆🏆 ELITE! 🏆🏆🏆🏆🏆🏆🏆", "50 days — proof you can do anything!"},
	{60, 420, "🏆 60 days!", "🔥 Two months without a slip — legend grows."},
	{90, 420, "🏆 90 days!", "🏁 A quarter of discipline — how elites train."},
	{100, 4200, "🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆 IMMORTAL LEGEND! 🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆", "100 days — rare air. You're making history!"},
	{180, 420, "🏆 180 days!", fmt.Sprintf("🔥 Half a year of streak — +420 %s!", cupsWordForm(420))},
	{200, 4200, "🌸 BUCKET OF CUPS! 🌸", "200 days in a row — you're a pack legend!"},
	{240, 4200, "🏆 240 days!", "🔥 240-day streak — titan level!"},
}

// MilestoneCups возвращает бонусные кубки за milestone текущего стрика (0 если milestone не достигнут).
func (o TrainingOutcome) MilestoneCups() int {
	if !o.EarnRewards {
		return 0
	}
	for _, m := range streakMilestones {
		if o.NewStreakDays == m.Days {
			return m.Cups
		}
	}
	return 0
}

// sendMilestoneReward отправляет поздравление с milestone стрика если текущий streak совпадает с одним из порогов.
func (b *Bot) sendMilestoneReward(msg *tgbotapi.Message, username string, streakDays int) {
	for _, m := range streakMilestones {
		if m.Days == streakDays {
			b.sendStreakReward(msg, username, streakDays, m.Cups, m.Title, m.Subtitle)
			return
		}
	}
}
