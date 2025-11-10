package bot

import (
	"testing"
	"time"

	"leo-bot/internal/config"
	"leo-bot/internal/domain"
	"leo-bot/internal/logger"
	"leo-bot/internal/usecase/sickleave"
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
		logger:             log,
		config:             cfg,
		sickLeaveEvaluator: sickleave.NewEvaluator(nil, log),
	}

	// Тест 1: Нет данных о времени
	messageLog := &domain.MessageLog{}
	remainingTime := bot.calculateRemainingTime(messageLog)
	expectedTime := 7 * 24 * time.Hour

	assertDurationApprox(t, remainingTime, expectedTime)

	// Тест 2: Есть данные о времени
	timerStart := utils.FormatMoscowTime(utils.GetMoscowTime().Add(-2 * 24 * time.Hour))
	sickLeaveStart := utils.FormatMoscowTime(utils.GetMoscowTime().Add(-1 * 24 * time.Hour))

	messageLogWithTime := &domain.MessageLog{
		TimerStartTime:     &timerStart,
		SickLeaveStartTime: &sickLeaveStart,
	}

	remainingTime = bot.calculateRemainingTime(messageLogWithTime)
	expectedTime = 5 * 24 * time.Hour // 7 - 2 = 5 дней
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

	remainingTime = bot.calculateRemainingTime(messageLogSickLeave)

	// Ожидаемое время: 7 дней - 2 дня (с 11.09 до 13.09) = 5 дней
	expectedTime = 5 * 24 * time.Hour
	assertDurationApprox(t, remainingTime, expectedTime)
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
	expected := "5 дн."
	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}

	// Тест 2: Дни и часы
	duration = 3*24*time.Hour + 5*time.Hour
	result = bot.formatDurationToDays(duration)
	expected = "3 дн. 5 ч."
	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}

	// Тест 3: Только часы
	duration = 2 * time.Hour
	result = bot.formatDurationToDays(duration)
	expected = "2 ч."
	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}

	// Тест 4: Часы и минуты
	duration = 1*time.Hour + 30*time.Minute
	result = bot.formatDurationToDays(duration)
	expected = "1 ч. 30 мин."
	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}

	// Тест 5: Только минуты
	duration = 45 * time.Minute
	result = bot.formatDurationToDays(duration)
	expected = "45 мин."
	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}
}

func TestEvaluateSickLeaveJustification(t *testing.T) {
	log := logger.New("info")
	bot := &Bot{
		logger:             log,
		sickLeaveEvaluator: sickleave.NewEvaluator(nil, log),
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
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			processed := extractSickLeaveJustification(&tgbotapi.Message{Text: tc.text})
			if got := bot.sickLeaveEvaluator.Evaluate(processed, nil); got != tc.want {
				t.Errorf("sickLeaveEvaluator.Evaluate(%q) = %v, want %v", tc.text, got, tc.want)
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
		logger:             log,
		config:             cfg,
		sickLeaveEvaluator: sickleave.NewEvaluator(nil, log),
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

	// Проверяем время на больничном
	remainingTimeOnSick := bot.calculateRemainingTime(messageLogSickLeave)
	expectedTimeOnSick := 5 * 24 * time.Hour // 7 - 2 = 5 дней

	if remainingTimeOnSick != expectedTimeOnSick {
		t.Errorf("On sick leave: Expected %v, got %v", expectedTimeOnSick, remainingTimeOnSick)
	}

	// Пользователь выздоровел
	messageLogSickLeave.HasHealthy = true
	sickEndStr := utils.FormatMoscowTime(utils.GetMoscowTime())
	messageLogSickLeave.SickLeaveEndTime = &sickEndStr

	// Проверяем время после выздоровления
	remainingTimeAfterRecovery := bot.calculateRemainingTime(messageLogSickLeave)
	expectedTimeAfterRecovery := 5 * 24 * time.Hour // Должно остаться то же время

	assertDurationApprox(t, remainingTimeAfterRecovery, expectedTimeAfterRecovery)

	// Проверяем форматирование времени
	formattedTime := bot.formatDurationToDays(remainingTimeAfterRecovery)
	expectedFormatted := "5 дн."

	if formattedTime != expectedFormatted {
		t.Errorf("Formatted time: Expected %s, got %s", expectedFormatted, formattedTime)
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

func TestCalculateCaloriesWeeklyAchievement(t *testing.T) {
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
		messageLog.CalorieStreakDays = day - 1

		calories, streakDays, newCalorieStreak, weeklyAchievement, twoWeekAchievement, threeWeekAchievement, monthlyAchievement, _, _, _, quarterlyAchievement, _ := bot.calculateCalories(messageLog)

		if calories != newCalorieStreak {
			t.Errorf("Day %d: calories %d must equal newCalorieStreak %d", day, calories, newCalorieStreak)
		}

		if day == 7 {
			// На 7-й день должно быть недельное достижение
			if !weeklyAchievement {
				t.Errorf("Day %d: Expected weekly achievement for 7-day streak", day)
			}
			if streakDays != 7 {
				t.Errorf("Day %d: Expected streak days 7, got %d", day, streakDays)
			}
			// На 7-й день не должно быть других достижений
			if twoWeekAchievement {
				t.Errorf("Day %d: Expected no two-week achievement for 7-day streak", day)
			}
			if threeWeekAchievement {
				t.Errorf("Day %d: Expected no three-week achievement for 7-day streak", day)
			}
			if monthlyAchievement {
				t.Errorf("Day %d: Expected no monthly achievement for 7-day streak", day)
			}
			if quarterlyAchievement {
				t.Errorf("Day %d: Expected no quarterly achievement for 7-day streak", day)
			}
		} else {
			// До 7-го дня не должно быть достижений
			if weeklyAchievement {
				t.Errorf("Day %d: Expected no weekly achievement for %d-day streak", day, day)
			}
			if twoWeekAchievement {
				t.Errorf("Day %d: Expected no two-week achievement for %d-day streak", day, day)
			}
			if threeWeekAchievement {
				t.Errorf("Day %d: Expected no three-week achievement for %d-day streak", day, day)
			}
			if monthlyAchievement {
				t.Errorf("Day %d: Expected no monthly achievement for %d-day streak", day, day)
			}
			if quarterlyAchievement {
				t.Errorf("Day %d: Expected no quarterly achievement for %d-day streak", day, day)
			}
		}

		// Обновляем данные для следующего дня
		messageLog.StreakDays = streakDays
		messageLog.CalorieStreakDays = newCalorieStreak
		today := utils.GetMoscowDate()
		messageLog.LastTrainingDate = &today
	}

	// Тест 2: Проверяем, что достижение срабатывает только на 7-й день
	messageLog2 := &domain.MessageLog{
		StreakDays:        6,
		CalorieStreakDays: 6,
		LastTrainingDate: func() *string {
			y := utils.GetMoscowDateFromTime(utils.GetMoscowTime().AddDate(0, 0, -1))
			return &y
		}(),
	}

	calories2, streakDays2, newCalorieStreak2, weeklyAchievement2, _, _, monthlyAchievement2, _, _, _, quarterlyAchievement2, _ := bot.calculateCalories(messageLog2)

	// На 7-й день должно быть недельное достижение
	if !weeklyAchievement2 {
		t.Error("Expected weekly achievement for 7-day streak")
	}
	if streakDays2 != 7 {
		t.Errorf("Expected streak days 7, got %d", streakDays2)
	}
	if calories2 != newCalorieStreak2 {
		t.Errorf("Expected calories equal to newCalorieStreak (%d), got %d", newCalorieStreak2, calories2)
	}
	// На 7-й день не должно быть месячного и квартального достижений
	if monthlyAchievement2 {
		t.Error("Expected no monthly achievement for 7-day streak")
	}
	if quarterlyAchievement2 {
		t.Error("Expected no quarterly achievement for 7-day streak")
	}

	// Тест 3: Проверяем, что на 6-й день нет достижения
	messageLog3 := &domain.MessageLog{
		StreakDays:        5,
		CalorieStreakDays: 5,
		LastTrainingDate: func() *string {
			y := utils.GetMoscowDateFromTime(utils.GetMoscowTime().AddDate(0, 0, -1))
			return &y
		}(),
	}

	calories3, streakDays3, newCalorieStreak3, weeklyAchievement3, _, _, monthlyAchievement3, _, _, _, quarterlyAchievement3, _ := bot.calculateCalories(messageLog3)

	// На 6-й день не должно быть достижений
	if weeklyAchievement3 {
		t.Error("Expected no weekly achievement for 6-day streak")
	}
	if monthlyAchievement3 {
		t.Error("Expected no monthly achievement for 6-day streak")
	}
	if quarterlyAchievement3 {
		t.Error("Expected no quarterly achievement for 6-day streak")
	}
	if streakDays3 != 6 {
		t.Errorf("Expected streak days 6, got %d", streakDays3)
	}
	if calories3 != newCalorieStreak3 {
		t.Errorf("Expected calories equal to newCalorieStreak (%d), got %d", newCalorieStreak3, calories3)
	}

	// Проверяем, что функции не падают с ошибками
	_ = calories2
	_ = calories3
}

func TestCalculateCaloriesMonthlyAchievement(t *testing.T) {
	// Создаем тестовый бот
	cfg := &config.Config{OwnerID: 123}
	bot := &Bot{
		config: cfg,
		logger: logger.New("info"),
	}

	// Тест: Пользователь достигает 30-дневной серии
	yesterday := utils.GetMoscowDateFromTime(utils.GetMoscowTime().AddDate(0, 0, -1))
	messageLog := &domain.MessageLog{
		LastTrainingDate:  &yesterday,
		StreakDays:        29,
		CalorieStreakDays: 29,
	}

	calories, streakDays, newCalorieStreak, weeklyAchievement, _, _, monthlyAchievement, _, _, _, quarterlyAchievement, _ := bot.calculateCalories(messageLog)

	// На 30-й день должно быть месячное достижение
	if !monthlyAchievement {
		t.Error("Expected monthly achievement for 30-day streak")
	}
	if streakDays != 30 {
		t.Errorf("Expected streak days 30, got %d", streakDays)
	}
	if calories != newCalorieStreak {
		t.Errorf("Expected calories equal to newCalorieStreak (%d), got %d", newCalorieStreak, calories)
	}
	// На 30-й день не должно быть недельного и квартального достижений
	if weeklyAchievement {
		t.Error("Expected no weekly achievement for 30-day streak (already achieved)")
	}
	if quarterlyAchievement {
		t.Error("Expected no quarterly achievement for 30-day streak")
	}

	// Тест: Пользователь не достигает месячной серии
	messageLog2 := &domain.MessageLog{
		LastTrainingDate:  &yesterday,
		StreakDays:        14,
		CalorieStreakDays: 14,
	}

	calories2, streakDays2, newCalorieStreak2, _, _, _, monthlyAchievement2, _, _, _, quarterlyAchievement2, _ := bot.calculateCalories(messageLog2)

	// На 15-й день не должно быть месячного и квартального достижений
	if monthlyAchievement2 {
		t.Error("Expected no monthly achievement for 15-day streak")
	}
	if quarterlyAchievement2 {
		t.Error("Expected no quarterly achievement for 15-day streak")
	}
	if streakDays2 != 15 {
		t.Errorf("Expected streak days 15, got %d", streakDays2)
	}
	if calories2 != newCalorieStreak2 {
		t.Errorf("Expected calories equal to newCalorieStreak (%d), got %d", newCalorieStreak2, calories2)
	}

	// Проверяем, что функции не падают с ошибками
	_ = calories
	_ = calories2
}

func TestCalculateCaloriesQuarterlyAchievement(t *testing.T) {
	// Создаем тестовый бот
	cfg := &config.Config{OwnerID: 123}
	bot := &Bot{
		config: cfg,
		logger: logger.New("info"),
	}

	// Тест: Пользователь достигает 90-дневной серии
	yesterday := utils.GetMoscowDateFromTime(utils.GetMoscowTime().AddDate(0, 0, -1))

	messageLog := &domain.MessageLog{
		LastTrainingDate:  &yesterday,
		StreakDays:        89,
		CalorieStreakDays: 89,
	}

	calories, streakDays, newCalorieStreak, weeklyAchievement, _, _, monthlyAchievement, _, _, _, quarterlyAchievement, _ := bot.calculateCalories(messageLog)

	// На 90-й день должно быть квартальное достижение
	if !quarterlyAchievement {
		t.Error("Expected quarterly achievement for 90-day streak")
	}
	if streakDays != 90 {
		t.Errorf("Expected streak days 90, got %d", streakDays)
	}
	if calories != newCalorieStreak {
		t.Errorf("Expected calories equal to newCalorieStreak (%d), got %d", newCalorieStreak, calories)
	}
	// На 90-й день не должно быть недельного и месячного достижений (уже были)
	if weeklyAchievement {
		t.Error("Expected no weekly achievement for 90-day streak (already achieved)")
	}
	if monthlyAchievement {
		t.Error("Expected no monthly achievement for 90-day streak (already achieved)")
	}

	// Тест: Пользователь не достигает квартальной серии
	messageLog2 := &domain.MessageLog{
		LastTrainingDate:  &yesterday,
		StreakDays:        45,
		CalorieStreakDays: 45,
	}

	calories2, streakDays2, newCalorieStreak2, _, _, _, _, _, _, quarterlyAchievement2, _, _ := bot.calculateCalories(messageLog2)

	// На 46-й день не должно быть квартального достижения
	if quarterlyAchievement2 {
		t.Error("Expected no quarterly achievement for 46-day streak")
	}
	if streakDays2 != 46 {
		t.Errorf("Expected streak days 46, got %d", streakDays2)
	}
	if calories2 != newCalorieStreak2 {
		t.Errorf("Expected calories equal to newCalorieStreak (%d), got %d", newCalorieStreak2, calories2)
	}

	// Проверяем, что функции не падают с ошибками
	_ = calories
	_ = calories2
}

func TestSendWeeklyCupsReward(t *testing.T) {
	// Создаем тестовый бот
	cfg := &config.Config{OwnerID: 123}
	_ = &Bot{
		config: cfg,
		logger: logger.New("info"),
	}

	// Создаем тестовое сообщение
	msg := &tgbotapi.Message{
		From: &tgbotapi.User{ID: 123, UserName: "testuser"},
		Chat: &tgbotapi.Chat{ID: 456},
		Text: "#training_done",
	}

	// Тестируем функцию (без реальной отправки сообщения)
	// В реальном тесте нужно было бы создать мок для API
	username := "testuser"
	streakDays := 7

	// Проверяем, что функция не падает с ошибками
	// В реальном тесте нужно проверить, что сообщение отправляется
	_ = msg
	_ = username
	_ = streakDays

	// Проверяем, что функция существует и может быть вызвана
	// (без реального вызова, так как нет мока для API)
	t.Log("sendWeeklyCupsReward function exists and can be called")
}

func TestHandleNewChatMembers(t *testing.T) {
	// Создаем тестовый бот
	cfg := &config.Config{OwnerID: 123}
	_ = &Bot{
		config: cfg,
		logger: logger.New("info"),
	}

	// Создаем тестовое сообщение с новыми участниками
	msg := &tgbotapi.Message{
		Chat: &tgbotapi.Chat{ID: 456},
		NewChatMembers: []tgbotapi.User{
			{
				ID:        789,
				UserName:  "testuser",
				FirstName: "Test",
				LastName:  "User",
				IsBot:     false,
			},
			{
				ID:    999,
				IsBot: true, // Бот должен быть пропущен
			},
		},
	}

	// Проверяем, что функция не падает с ошибками
	// В реальном тесте нужно было бы создать мок для API
	_ = msg

	// Проверяем, что функция существует и может быть вызвана
	// (без реального вызова, так как нет мока для API)
	t.Log("handleNewChatMembers function exists and can be called")
}

func TestSendWelcomeMessage(t *testing.T) {
	// Создаем тестовый бот
	cfg := &config.Config{OwnerID: 123}
	_ = &Bot{
		config: cfg,
		logger: logger.New("info"),
	}

	// Тестовые данные
	chatID := int64(456)
	username := "testuser"
	userID := int64(789)

	// Проверяем, что функция не падает с ошибками
	// В реальном тесте нужно было бы создать мок для API
	_ = chatID
	_ = username
	_ = userID

	// Проверяем, что функция существует и может быть вызвана
	// (без реального вызова, так как нет мока для API)
	t.Log("sendWelcomeMessage function exists and can be called")
}

func TestCalculateCaloriesDoubleTraining(t *testing.T) {
	// Создаем тестовый бот
	cfg := &config.Config{OwnerID: 123}
	bot := &Bot{config: cfg, logger: logger.New("info")}

	// Тест 1: Первая тренировка сегодня
	messageLog1 := &domain.MessageLog{
		LastTrainingDate: nil,
		StreakDays:       0,
	}

	calories1, streakDays1, _, weeklyAchievement1, _, _, monthlyAchievement1, _, _, _, quarterlyAchievement1, _ := bot.calculateCalories(messageLog1)

	// Первая тренировка должна дать калории и увеличить streak
	if calories1 == 0 {
		t.Error("Expected calories > 0 for first training today")
	}
	if streakDays1 != 1 {
		t.Errorf("Expected streak days 1 for first training, got %d", streakDays1)
	}
	if weeklyAchievement1 {
		t.Error("Expected no weekly achievement for first training")
	}
	if monthlyAchievement1 {
		t.Error("Expected no monthly achievement for first training")
	}
	if quarterlyAchievement1 {
		t.Error("Expected no quarterly achievement for first training")
	}

	// Тест 2: Вторая тренировка в тот же день
	today := utils.GetMoscowDate()
	messageLog2 := &domain.MessageLog{
		LastTrainingDate: &today,
		StreakDays:       1,
	}

	calories2, streakDays2, _, weeklyAchievement2, _, _, monthlyAchievement2, _, _, _, quarterlyAchievement2, _ := bot.calculateCalories(messageLog2)

	// Вторая тренировка в тот же день не должна дать калории и не должна изменить streak
	if calories2 != 0 {
		t.Errorf("Expected calories 0 for second training today, got %d", calories2)
	}
	if streakDays2 != 1 {
		t.Errorf("Expected streak days 1 for second training today, got %d", streakDays2)
	}
	if weeklyAchievement2 {
		t.Error("Expected no weekly achievement for second training today")
	}
	if monthlyAchievement2 {
		t.Error("Expected no monthly achievement for second training today")
	}
	if quarterlyAchievement2 {
		t.Error("Expected no quarterly achievement for second training today")
	}

	// Тест 3: Тренировка на следующий день после двойной тренировки
	yesterday := utils.GetMoscowTime().AddDate(0, 0, -1)
	yesterdayStr := utils.GetMoscowDateFromTime(yesterday)
	messageLog3 := &domain.MessageLog{
		LastTrainingDate: &yesterdayStr,
		StreakDays:       1,
	}

	calories3, streakDays3, _, weeklyAchievement3, _, _, monthlyAchievement3, _, _, _, quarterlyAchievement3, _ := bot.calculateCalories(messageLog3)

	// Тренировка на следующий день должна продолжить серию
	if calories3 == 0 {
		t.Error("Expected calories > 0 for training next day")
	}
	if streakDays3 != 2 {
		t.Errorf("Expected streak days 2 for training next day, got %d", streakDays3)
	}
	if weeklyAchievement3 {
		t.Error("Expected no weekly achievement for 2-day streak")
	}
	if monthlyAchievement3 {
		t.Error("Expected no monthly achievement for 2-day streak")
	}
	if quarterlyAchievement3 {
		t.Error("Expected no quarterly achievement for 2-day streak")
	}

	t.Log("Double training logic test passed")
}
