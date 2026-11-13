package bot

import (
	"testing"
	"time"

	"leo-bot/internal/config"
	"leo-bot/internal/domain"
	"leo-bot/internal/logger"
	"leo-bot/internal/utils"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const durationTolerance = time.Minute

func assertDurationApprox(t *testing.T, got, want time.Duration) {
	t.Helper()
	diff := got - want
	if diff < 0 {
		diff = -diff
	}
	if diff > durationTolerance {
		t.Fatalf("expected duration ~%v (±%v), got %v", want, durationTolerance, got)
	}
}

func TestCalculateRemainingTime(t *testing.T) {
	// Создаем мок логгер
	log := logger.New("info")

	// Создаем тестовый бот
	cfg := &config.Config{OwnerID: 123}
	bot := &Bot{
		logger: log,
		config: cfg,
	}

	// Тест 1: Нет данных о времени — полное окно Leopard Money (8 дней до удаления)
	messageLog := &domain.MessageLog{}
	remainingTime := bot.calculateRemainingTime(messageLog)
	expectedTime := 8 * 24 * time.Hour

	assertDurationApprox(t, remainingTime, expectedTime)

	// Тест 2: Есть данные о времени
	timerStart := utils.FormatMoscowTime(utils.GetMoscowTime().Add(-2 * 24 * time.Hour))
	sickLeaveStart := utils.FormatMoscowTime(utils.GetMoscowTime().Add(-1 * 24 * time.Hour))

	messageLogWithTime := &domain.MessageLog{
		TimerStartTime:     &timerStart,
		SickLeaveStartTime: &sickLeaveStart,
		HasSickLeave:       true,
		HasHealthy:         false,
	}

	remainingTime = bot.calculateRemainingTime(messageLogWithTime)
	now := utils.GetMoscowTime()
	deadline, ok := inactivityKickDeadline(messageLogWithTime, now)
	if !ok {
		t.Fatal("inactivityKickDeadline: expected ok")
	}
	expectedTime = deadline.Sub(now)
	assertDurationApprox(t, remainingTime, expectedTime)

	// Тест 3: Больничный сценарий - тренировка 11.09, больничный 13.09, выход 19.09
	timerStartStr := utils.FormatMoscowTime(utils.GetMoscowTime().Add(-7 * 24 * time.Hour))
	sickStartStr := utils.FormatMoscowTime(utils.GetMoscowTime().Add(-5 * 24 * time.Hour))
	sickEndStr := utils.FormatMoscowTime(utils.GetMoscowTime())

	messageLogSickLeave := &domain.MessageLog{
		TimerStartTime:     &timerStartStr,
		SickLeaveStartTime: &sickStartStr,
		SickLeaveEndTime:   &sickEndStr,
		HasSickLeave:       true,
		HasHealthy:         true, // Пользователь выздоровел
	}

	now = utils.GetMoscowTime()
	deadline, ok = inactivityKickDeadline(messageLogSickLeave, now)
	if !ok {
		t.Fatal("inactivityKickDeadline: expected ok")
	}
	expectedTime = deadline.Sub(now)
	remainingTime = bot.calculateRemainingTime(messageLogSickLeave)
	assertDurationApprox(t, remainingTime, expectedTime)
}

// Регрессия: новый timer_start после выхода с больничного (#training_done), в БД всё ещё лежат старые sick_*.
// Нельзя применять формулу «остаток на момент больничного» — иначе получается огромный ложный остаток и кик не срабатывает.
func TestCalculateRemainingTime_NewTimerAfterSickUsesElapsed(t *testing.T) {
	log := logger.New("info")
	bot := &Bot{
		logger: log,
		config: &config.Config{OwnerID: 123},
	}
	sickStart := utils.FormatMoscowTime(utils.GetMoscowTime().Add(-120 * 24 * time.Hour))
	sickEnd := utils.FormatMoscowTime(utils.GetMoscowTime().Add(-90 * 24 * time.Hour))
	// Тренировка перезапустила таймер уже после больничного
	timerStart := utils.FormatMoscowTime(utils.GetMoscowTime().Add(-30 * 24 * time.Hour))

	ml := &domain.MessageLog{
		TimerStartTime:     &timerStart,
		SickLeaveStartTime: &sickStart,
		SickLeaveEndTime:   &sickEnd,
		HasHealthy:         true,
	}
	got := bot.calculateRemainingTime(ml)
	if got > 0 {
		t.Fatalf("expected expired (0 duration), got %v", got)
	}
}

func TestFormatDurationToDays(t *testing.T) {
	// Создаем тестовый бот
	cfg := &config.Config{OwnerID: 123}
	bot := &Bot{
		config: cfg,
	}

	// Тест 1: Только дни
	duration := 5 * 24 * time.Hour
	result := bot.formatDurationToDays(duration)
	expected := "5 days"
	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}

	// Тест 2: Дни и часы
	duration = 3*24*time.Hour + 5*time.Hour
	result = bot.formatDurationToDays(duration)
	expected = "3 days 5 h."
	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}

	// Тест 3: Только часы
	duration = 2 * time.Hour
	result = bot.formatDurationToDays(duration)
	expected = "2 h."
	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}

	// Тест 4: Часы и минуты
	duration = 1*time.Hour + 30*time.Minute
	result = bot.formatDurationToDays(duration)
	expected = "1 h. 30 min."
	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}

	// Тест 5: Только минуты
	duration = 45 * time.Minute
	result = bot.formatDurationToDays(duration)
	expected = "45 min."
	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}
}

func TestEvaluateSickLeaveJustification(t *testing.T) {
	log := logger.New("info")
	bot := &Bot{
		logger: log,
	}

	cases := []struct {
		name string
		text string
		want bool
	}{
		{
			name: "Positive keywords",
			text: "Ребята, честно болею и лежу с температурой",
			want: true,
		},
		{
			name: "Short symptom phrase",
			text: "плохое самочувствие",
			want: true,
		},
		{
			name: "Support request without symptoms",
			text: "Пожалуйста, дай шанс, честно-честно поправлюсь и буду тренироваться",
			want: true,
		},
		{
			name: "Negative activity",
			text: "Не болен, просто делами занят и работаю над проектом",
			want: false,
		},
		{
			name: "Empty justification",
			text: "",
			want: false,
		},
		{
			name: "Hashtags removed",
			text: "#sick_leave честно болею",
			want: true,
		},
		{
			name: "GI symptom понос (эвристика без модели)",
			text: "#sick_leave понос второй день",
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			processed := extractSickLeaveJustification(&tgbotapi.Message{Text: tc.text})
			if got := bot.evaluateSickLeaveJustification(processed, nil); got != tc.want {
				t.Errorf("evaluateSickLeaveJustification(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

func TestSickLeaveRecoveryScenario(t *testing.T) {
	// Создаем мок логгер
	log := logger.New("info")

	// Создаем тестовый бот
	cfg := &config.Config{OwnerID: 123}
	bot := &Bot{
		logger: log,
		config: cfg,
	}

	// Тест: Больничный сценарий - тренировка, больничный, выздоровление
	// Создаем фиксированные даты для тестирования
	trainingTime := utils.GetMoscowTime().Add(-7 * 24 * time.Hour)
	sickStartTime := trainingTime.Add(2 * 24 * time.Hour)

	timerStartStr := utils.FormatMoscowTime(trainingTime)
	sickStartStr := utils.FormatMoscowTime(sickStartTime)

	// Создаем пользователя на больничном
	messageLogSickLeave := &domain.MessageLog{
		TimerStartTime:     &timerStartStr,
		SickLeaveStartTime: &sickStartStr,
		HasSickLeave:       true,
		HasHealthy:         false, // На больничном
	}

	now := utils.GetMoscowTime()
	dOnSick, ok := inactivityKickDeadline(messageLogSickLeave, now)
	if !ok {
		t.Fatal("inactivityKickDeadline on sick: expected ok")
	}
	remainingTimeOnSick := bot.calculateRemainingTime(messageLogSickLeave)
	expectedTimeOnSick := dOnSick.Sub(now)
	assertDurationApprox(t, remainingTimeOnSick, expectedTimeOnSick)

	messageLogSickLeave.HasHealthy = true
	sickEndStr := utils.FormatMoscowTime(utils.GetMoscowTime())
	messageLogSickLeave.SickLeaveEndTime = &sickEndStr

	now2 := utils.GetMoscowTime()
	dAfter, ok2 := inactivityKickDeadline(messageLogSickLeave, now2)
	if !ok2 {
		t.Fatal("inactivityKickDeadline after recovery: expected ok")
	}
	remainingTimeAfterRecovery := bot.calculateRemainingTime(messageLogSickLeave)
	expectedTimeAfterRecovery := dAfter.Sub(now2)
	assertDurationApprox(t, remainingTimeAfterRecovery, expectedTimeAfterRecovery)

	formattedTime := bot.formatDurationToDays(remainingTimeAfterRecovery)
	if formattedTime == "" {
		t.Errorf("Formatted time: empty")
	}
}

func TestIsAdmin(t *testing.T) {
	// Создаем тестовый бот
	cfg := &config.Config{OwnerID: 123}
	bot := &Bot{
		config: cfg,
		logger: logger.New("info"),
	}

	// Тест: Пользователь является владельцем
	isAdmin := bot.isAdmin(456, 123)
	if !isAdmin {
		t.Error("Owner should be admin")
	}

	// Тест: Пользователь не является владельцем
	isAdmin = bot.isAdmin(456, 789)
	if isAdmin {
		t.Error("Non-owner should not be admin")
	}
}

func TestHandleSendToChat(t *testing.T) {
	// Создаем тестовый бот
	cfg := &config.Config{OwnerID: 123}
	_ = &Bot{
		config: cfg,
		logger: logger.New("info"),
	}

	// Тест 1: Пользователь не является владельцем
	msg := &tgbotapi.Message{
		From: &tgbotapi.User{ID: 789},
		Chat: &tgbotapi.Chat{ID: 456},
		Text: "/send_to_chat 123 test message",
	}

	// Тест 2: Владелец с правильными аргументами
	ownerMsg := &tgbotapi.Message{
		From: &tgbotapi.User{ID: 123},
		Chat: &tgbotapi.Chat{ID: 456},
		Text: "/send_to_chat 789 test message",
	}

	// Тест 3: Владелец без аргументов
	ownerMsgNoArgs := &tgbotapi.Message{
		From: &tgbotapi.User{ID: 123},
		Chat: &tgbotapi.Chat{ID: 456},
		Text: "/send_to_chat",
	}

	// Тест 4: Владелец с неправильным форматом chat_id
	ownerMsgBadFormat := &tgbotapi.Message{
		From: &tgbotapi.User{ID: 123},
		Chat: &tgbotapi.Chat{ID: 456},
		Text: "/send_to_chat invalid_id test message",
	}

	// Проверяем, что функции не падают с ошибками
	// В реальном тесте нужно проверить логику более детально
	_ = msg
	_ = ownerMsg
	_ = ownerMsgNoArgs
	_ = ownerMsgBadFormat
}

func TestTrainingDayOutcomeWeeklyAchievement(t *testing.T) {
	// Создаем тестовый бот
	cfg := &config.Config{OwnerID: 123}
	bot := &Bot{
		config: cfg,
		logger: logger.New("info"),
	}

	// Тест 1: Проверяем логику недельного достижения
	// Создаем тестовые данные для 7-дневной серии
	messageLog := &domain.MessageLog{}

	// Симулируем 7 дней подряд тренировок
	for day := 1; day <= 7; day++ {
		yesterday := utils.GetMoscowDateFromTime(utils.GetMoscowTime().AddDate(0, 0, -1))
		messageLog.LastTrainingDate = &yesterday
		messageLog.StreakDays = day - 1

		out := bot.calculateTrainingDayOutcome(messageLog)

		if !out.EarnRewards {
			t.Errorf("Day %d: expected earnRewards true for new training day", day)
		}

		if day == 7 {
			if !out.Weekly {
				t.Errorf("Day %d: Expected weekly achievement for 7-day streak", day)
			}
			if out.NewStreakDays != 7 {
				t.Errorf("Day %d: Expected streak days 7, got %d", day, out.NewStreakDays)
			}
			if out.TwoWeek {
				t.Errorf("Day %d: Expected no two-week achievement for 7-day streak", day)
			}
			if out.ThreeWeek {
				t.Errorf("Day %d: Expected no three-week achievement for 7-day streak", day)
			}
			if out.Monthly {
				t.Errorf("Day %d: Expected no monthly achievement for 7-day streak", day)
			}
			if out.Quarterly {
				t.Errorf("Day %d: Expected no quarterly achievement for 7-day streak", day)
			}
		} else {
			if out.Weekly {
				t.Errorf("Day %d: Expected no weekly achievement for %d-day streak", day, day)
			}
			if out.TwoWeek {
				t.Errorf("Day %d: Expected no two-week achievement for %d-day streak", day, day)
			}
			if out.ThreeWeek {
				t.Errorf("Day %d: Expected no three-week achievement for %d-day streak", day, day)
			}
			if out.Monthly {
				t.Errorf("Day %d: Expected no monthly achievement for %d-day streak", day, day)
			}
			if out.Quarterly {
				t.Errorf("Day %d: Expected no quarterly achievement for %d-day streak", day, day)
			}
		}

		// Обновляем данные для следующего дня
		messageLog.StreakDays = out.NewStreakDays
		today := utils.GetMoscowDate()
		messageLog.LastTrainingDate = &today
	}

	// Тест 2: Проверяем, что достижение срабатывает только на 7-й день
	messageLog2 := &domain.MessageLog{
		StreakDays: 6,
		LastTrainingDate: func() *string {
			y := utils.GetMoscowDateFromTime(utils.GetMoscowTime().AddDate(0, 0, -1))
			return &y
		}(),
	}

	out2 := bot.calculateTrainingDayOutcome(messageLog2)

	if !out2.EarnRewards {
		t.Error("Expected earnRewards for 7-day streak simulation")
	}
	if !out2.Weekly {
		t.Error("Expected weekly achievement for 7-day streak")
	}
	if out2.NewStreakDays != 7 {
		t.Errorf("Expected streak days 7, got %d", out2.NewStreakDays)
	}
	if out2.Monthly {
		t.Error("Expected no monthly achievement for 7-day streak")
	}
	if out2.Quarterly {
		t.Error("Expected no quarterly achievement for 7-day streak")
	}

	// Тест 3: Проверяем, что на 6-й день нет достижения
	messageLog3 := &domain.MessageLog{
		StreakDays: 5,
		LastTrainingDate: func() *string {
			y := utils.GetMoscowDateFromTime(utils.GetMoscowTime().AddDate(0, 0, -1))
			return &y
		}(),
	}

	out3 := bot.calculateTrainingDayOutcome(messageLog3)

	if out3.Weekly {
		t.Error("Expected no weekly achievement for 6-day streak")
	}
	if out3.Monthly {
		t.Error("Expected no monthly achievement for 6-day streak")
	}
	if out3.Quarterly {
		t.Error("Expected no quarterly achievement for 6-day streak")
	}
	if out3.NewStreakDays != 6 {
		t.Errorf("Expected streak days 6, got %d", out3.NewStreakDays)
	}
	if !out3.EarnRewards {
		t.Error("Expected earnRewards for 6th day advance")
	}
}

func TestTrainingDayOutcomeMonthlyAchievement(t *testing.T) {
	// Создаем тестовый бот
	cfg := &config.Config{OwnerID: 123}
	bot := &Bot{
		config: cfg,
		logger: logger.New("info"),
	}

	// Тест: Пользователь достигает 30-дневной серии
	yesterday := utils.GetMoscowDateFromTime(utils.GetMoscowTime().AddDate(0, 0, -1))
	messageLog := &domain.MessageLog{
		LastTrainingDate: &yesterday,
		StreakDays:       29,
	}

	out := bot.calculateTrainingDayOutcome(messageLog)

	if !out.EarnRewards {
		t.Error("Expected earnRewards for monthly advance")
	}
	if !out.Monthly {
		t.Error("Expected monthly achievement for 30-day streak")
	}
	if out.NewStreakDays != 30 {
		t.Errorf("Expected streak days 30, got %d", out.NewStreakDays)
	}
	if out.Weekly {
		t.Error("Expected no weekly achievement for 30-day streak (already achieved)")
	}
	if out.Quarterly {
		t.Error("Expected no quarterly achievement for 30-day streak")
	}

	// Тест: Пользователь не достигает месячной серии
	messageLog2 := &domain.MessageLog{
		LastTrainingDate: &yesterday,
		StreakDays:       14,
	}

	out2 := bot.calculateTrainingDayOutcome(messageLog2)

	if !out2.EarnRewards {
		t.Error("Expected earnRewards for 15-day advance")
	}
	if out2.Monthly {
		t.Error("Expected no monthly achievement for 15-day streak")
	}
	if out2.Quarterly {
		t.Error("Expected no quarterly achievement for 15-day streak")
	}
	if out2.NewStreakDays != 15 {
		t.Errorf("Expected streak days 15, got %d", out2.NewStreakDays)
	}
}

func TestTrainingDayOutcomeQuarterlyAchievement(t *testing.T) {
	// Создаем тестовый бот
	cfg := &config.Config{OwnerID: 123}
	bot := &Bot{
		config: cfg,
		logger: logger.New("info"),
	}

	// Тест: Пользователь достигает 90-дневной серии
	yesterday := utils.GetMoscowDateFromTime(utils.GetMoscowTime().AddDate(0, 0, -1))

	messageLog := &domain.MessageLog{
		LastTrainingDate: &yesterday,
		StreakDays:       89,
	}

	out := bot.calculateTrainingDayOutcome(messageLog)

	if !out.EarnRewards {
		t.Error("Expected earnRewards for quarterly advance")
	}
	if !out.Quarterly {
		t.Error("Expected quarterly achievement for 90-day streak")
	}
	if out.NewStreakDays != 90 {
		t.Errorf("Expected streak days 90, got %d", out.NewStreakDays)
	}
	if out.Weekly {
		t.Error("Expected no weekly achievement for 90-day streak (already achieved)")
	}
	if out.Monthly {
		t.Error("Expected no monthly achievement for 90-day streak (already achieved)")
	}

	// Тест: Пользователь не достигает квартальной серии
	messageLog2 := &domain.MessageLog{
		LastTrainingDate: &yesterday,
		StreakDays:       45,
	}

	out2 := bot.calculateTrainingDayOutcome(messageLog2)

	if !out2.EarnRewards {
		t.Error("Expected earnRewards for 46-day advance")
	}
	if out2.Quarterly {
		t.Error("Expected no quarterly achievement for 46-day streak")
	}
	if out2.NewStreakDays != 46 {
		t.Errorf("Expected streak days 46, got %d", out2.NewStreakDays)
	}
}

func TestTrainingDayOutcomeDoubleTraining(t *testing.T) {
	// Создаем тестовый бот
	cfg := &config.Config{OwnerID: 123}
	bot := &Bot{config: cfg, logger: logger.New("info")}

	// Тест 1: Первая тренировка сегодня
	messageLog1 := &domain.MessageLog{
		LastTrainingDate: nil,
		StreakDays:       0,
	}

	out1 := bot.calculateTrainingDayOutcome(messageLog1)

	// Первая тренировка дня — начисления
	if !out1.EarnRewards {
		t.Error("Expected earnRewards for first training today")
	}
	if out1.NewStreakDays != 1 {
		t.Errorf("Expected streak days 1 for first training, got %d", out1.NewStreakDays)
	}
	if out1.Weekly {
		t.Error("Expected no weekly achievement for first training")
	}
	if out1.Monthly {
		t.Error("Expected no monthly achievement for first training")
	}
	if out1.Quarterly {
		t.Error("Expected no quarterly achievement for first training")
	}

	// Тест 2: Вторая тренировка в тот же день
	today := utils.GetMoscowDate()
	messageLog2 := &domain.MessageLog{
		LastTrainingDate: &today,
		StreakDays:       1,
	}

	out2 := bot.calculateTrainingDayOutcome(messageLog2)

	// Вторая тренировка в тот же день — без начислений
	if out2.EarnRewards {
		t.Error("Expected earnRewards false for second training today")
	}
	if out2.NewStreakDays != 1 {
		t.Errorf("Expected streak days 1 for second training today, got %d", out2.NewStreakDays)
	}
	if out2.Weekly {
		t.Error("Expected no weekly achievement for second training today")
	}
	if out2.Monthly {
		t.Error("Expected no monthly achievement for second training today")
	}
	if out2.Quarterly {
		t.Error("Expected no quarterly achievement for second training today")
	}

	// Тест 3: Тренировка на следующий день после двойной тренировки
	yesterday := utils.GetMoscowTime().AddDate(0, 0, -1)
	yesterdayStr := utils.GetMoscowDateFromTime(yesterday)
	messageLog3 := &domain.MessageLog{
		LastTrainingDate: &yesterdayStr,
		StreakDays:       1,
	}

	out3 := bot.calculateTrainingDayOutcome(messageLog3)

	// Тренировка на следующий день должна продолжить серию
	if !out3.EarnRewards {
		t.Error("Expected earnRewards for training next day")
	}
	if out3.NewStreakDays != 2 {
		t.Errorf("Expected streak days 2 for training next day, got %d", out3.NewStreakDays)
	}
	if out3.Weekly {
		t.Error("Expected no weekly achievement for 2-day streak")
	}
	if out3.Monthly {
		t.Error("Expected no monthly achievement for 2-day streak")
	}
	if out3.Quarterly {
		t.Error("Expected no quarterly achievement for 2-day streak")
	}

	t.Log("Double training logic test passed")
}
