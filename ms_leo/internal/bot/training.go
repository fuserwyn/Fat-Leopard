package bot

import (
	"fmt"
	"strings"
	"time"

	"leo-bot/internal/domain"
	"leo-bot/internal/utils"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (b *Bot) processTrainingDone(msg *tgbotapi.Message) {
	username := ""
	if msg.From.UserName != "" {
		username = "@" + msg.From.UserName
	} else if msg.From.FirstName != "" {
		username = msg.From.FirstName
		if msg.From.LastName != "" {
			username += " " + msg.From.LastName
		}
	} else {
		username = fmt.Sprintf("User%d", msg.From.ID)
	}

	trainingLog := &domain.TrainingLog{
		UserID:     msg.From.ID,
		ChatID:     msg.Chat.ID,
		Username:   username,
		LastReport: utils.FormatMoscowTime(utils.GetMoscowTime()),
	}

	if err := b.db.SaveTrainingLog(trainingLog); err != nil {
		b.logger.Errorf("Failed to save training log: %v", err)
	}

	messageLog, err := b.db.GetMessageLog(msg.From.ID, msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get message log: %v", err)
		return
	}

	if messageLog.SickApprovalPending {
		b.cancelSickApprovalWatcher(msg.From.ID)
		messageLog.SickApprovalPending = false
		messageLog.SickApprovalDeadline = nil
		messageLog.SickApprovalMessageID = nil
		if err := b.db.SaveMessageLog(messageLog); err != nil {
			b.logger.Errorf("Failed to clear sick approval flags after training: %v", err)
		}
	}

	userGender := strings.TrimSpace(strings.ToLower(messageLog.Gender))
	if userGender == "" {
		userGender = b.detectGenderFromName(msg.From.FirstName)
		if userGender != "" {
			if err := b.updateUserGender(msg.From.ID, msg.Chat.ID, userGender); err != nil {
				b.logger.Warnf("Failed to update user gender: %v", err)
			}
		}
	}

	caloriesToAdd, newStreakDays, newCalorieStreakDays, weeklyAchievement, twoWeekAchievement, threeWeekAchievement, monthlyAchievement, fortyTwoDayAchievement, fiftyDayAchievement, sixtyDayAchievement, quarterlyAchievement, hundredDayAchievement, oneHundredEightyDayAchievement, twoHundredDayAchievement, twoHundredFortyDayAchievement := b.calculateCalories(messageLog)

	b.logger.Infof("DEBUG handleTrainingDone: caloriesToAdd=%d, newStreakDays=%d, newCalorieStreakDays=%d, weeklyAchievement=%t, twoWeekAchievement=%t, threeWeekAchievement=%t, monthlyAchievement=%t, fortyTwoDayAchievement=%t, fiftyDayAchievement=%t, sixtyDayAchievement=%t, quarterlyAchievement=%t, hundredDayAchievement=%t, oneHundredEightyDayAchievement=%t, twoHundredDayAchievement=%t, twoHundredFortyDayAchievement=%t",
		caloriesToAdd, newStreakDays, newCalorieStreakDays, weeklyAchievement, twoWeekAchievement, threeWeekAchievement, monthlyAchievement, fortyTwoDayAchievement, fiftyDayAchievement, sixtyDayAchievement, quarterlyAchievement, hundredDayAchievement, oneHundredEightyDayAchievement, twoHundredDayAchievement, twoHundredFortyDayAchievement)

	if err := b.db.AddCalories(msg.From.ID, msg.Chat.ID, caloriesToAdd); err != nil {
		b.logger.Errorf("Failed to add calories: %v", err)
	} else {
		b.logger.Infof("DEBUG: Successfully added %d calories", caloriesToAdd)
	}

	if caloriesToAdd > 0 {
		updatedCalories, err := b.db.GetUserCalories(msg.From.ID, msg.Chat.ID)
		if err != nil {
			b.logger.Errorf("Failed to get updated calories: %v", err)
		} else if updatedCalories >= 100 && updatedCalories-caloriesToAdd < 100 {
			messageText := fmt.Sprintf("🎉 Поздравляю! 🎉\n\n%s, достигнуто %d калорий!\n\n🔄 Теперь можешь совершить обмен!\n💡 Напиши #change для обмена 100 калорий на 42 кубка!", username, updatedCalories)

			if b.aiClient != nil {
				action := tgbotapi.NewChatAction(msg.Chat.ID, tgbotapi.ChatTyping)
				b.api.Send(action)
				stopTyping := make(chan struct{})
				defer close(stopTyping)
				go func() {
					ticker := time.NewTicker(4 * time.Second)
					defer ticker.Stop()
					for {
						select {
						case <-ticker.C:
							b.api.Send(action)
						case <-stopTyping:
							return
						}
					}
				}()

				totalCups, _ := b.db.GetUserCups(msg.From.ID, msg.Chat.ID)
				q := "Сделай короткую приписку (1–2 предложения): дружелюбно и по делу предложи обмен через #change. Обязательно поясни, что после обмена калории обнулятся и начнут накапливаться заново; обмен имеет смысл, если ожидается перерыв в тренировках. Укажи, что серия и кубки продолжаются как обычно. Не повторяй цифры из текста, без Markdown."
				var ctxBuilder strings.Builder
				ctxBuilder.WriteString(fmt.Sprintf("Пользователь: %s\n", username))
				genderNormalized := strings.TrimSpace(strings.ToLower(userGender))
				if genderNormalized != "" {
					var genderText string
					if genderNormalized == "f" {
						genderText = "женский"
					} else if genderNormalized == "m" {
						genderText = "мужской"
					}
					if genderText != "" {
						ctxBuilder.WriteString(fmt.Sprintf("Пол: %s\n", genderText))
					}
				}
				ctxBuilder.WriteString(fmt.Sprintf("Текущие калории: %d\n", updatedCalories))
				ctxBuilder.WriteString(fmt.Sprintf("Текущие кубки: %d\n", totalCups))
				if add, err := b.aiClient.AnswerUserQuestion(q, ctxBuilder.String()); err == nil {
					add = strings.TrimSpace(strings.ReplaceAll(add, "**", ""))
					if add != "" {
						messageText = messageText + "\n\n" + add
					}
				} else {
					b.logger.Warnf("AI addendum generation (exchange) failed: %v", err)
				}
			}

			exchangeMessage := tgbotapi.NewMessage(msg.Chat.ID, messageText)

			b.logger.Infof("Sending 100 calories achievement message to chat %d", msg.Chat.ID)
			_, err = b.api.Send(exchangeMessage)
			if err != nil {
				b.logger.Errorf("Failed to send 100 calories achievement message: %v", err)
			} else {
				b.logger.Infof("Successfully sent 100 calories achievement message to chat %d", msg.Chat.ID)
			}
		}
	}

	if caloriesToAdd > 0 {
		today := b.getUserLocalDate(messageLog.TimezoneOffsetFromMoscow)

		if err := b.db.UpdateStreak(msg.From.ID, msg.Chat.ID, newStreakDays, today); err != nil {
			b.logger.Errorf("Failed to update streak: %v", err)
		}

		if err := b.db.UpdateCalorieStreakWithDate(msg.From.ID, msg.Chat.ID, newCalorieStreakDays, today); err != nil {
			b.logger.Errorf("Failed to update calorie streak: %v", err)
		}
	}

	wasOnSickLeave := messageLog.HasSickLeave && !messageLog.HasHealthy

	if caloriesToAdd > 0 {
		if err := b.db.AddCups(msg.From.ID, msg.Chat.ID, 1); err != nil {
			b.logger.Errorf("Failed to add daily cup: %v", err)
		}

		if weeklyAchievement {
			if err := b.db.AddCups(msg.From.ID, msg.Chat.ID, 42); err != nil {
				b.logger.Errorf("Failed to add weekly cups: %v", err)
			}
		}

		if twoWeekAchievement {
			if err := b.db.AddCups(msg.From.ID, msg.Chat.ID, 42); err != nil {
				b.logger.Errorf("Failed to add two-week cups: %v", err)
			}
		}

		if threeWeekAchievement {
			if err := b.db.AddCups(msg.From.ID, msg.Chat.ID, 42); err != nil {
				b.logger.Errorf("Failed to add three-week cups: %v", err)
			}
		}

		if monthlyAchievement {
			if err := b.db.AddCups(msg.From.ID, msg.Chat.ID, 420); err != nil {
				b.logger.Errorf("Failed to add monthly cups: %v", err)
			}
		}

		if fortyTwoDayAchievement {
			if err := b.db.AddCups(msg.From.ID, msg.Chat.ID, 42); err != nil {
				b.logger.Errorf("Failed to add 42-day cups: %v", err)
			}
		}

		if fiftyDayAchievement {
			if err := b.db.AddCups(msg.From.ID, msg.Chat.ID, 42); err != nil {
				b.logger.Errorf("Failed to add 50-day cups: %v", err)
			}
		}

		if sixtyDayAchievement {
			if err := b.db.AddCups(msg.From.ID, msg.Chat.ID, 420); err != nil {
				b.logger.Errorf("Failed to add 60-day cups: %v", err)
			}
		}

		if quarterlyAchievement {
			if err := b.db.AddCups(msg.From.ID, msg.Chat.ID, 420); err != nil {
				b.logger.Errorf("Failed to add quarterly cups: %v", err)
			}
		}

		if hundredDayAchievement {
			if err := b.db.AddCups(msg.From.ID, msg.Chat.ID, 4200); err != nil {
				b.logger.Errorf("Failed to add 100-day cups: %v", err)
			}
		}

		if oneHundredEightyDayAchievement {
			if err := b.db.AddCups(msg.From.ID, msg.Chat.ID, 420); err != nil {
				b.logger.Errorf("Failed to add 180-day cups: %v", err)
			}
		}

		if twoHundredDayAchievement {
			if err := b.db.AddCups(msg.From.ID, msg.Chat.ID, 4200); err != nil {
				b.logger.Errorf("Failed to add 200-day cups: %v", err)
			}
		}

		if twoHundredFortyDayAchievement {
			if err := b.db.AddCups(msg.From.ID, msg.Chat.ID, 4200); err != nil {
				b.logger.Errorf("Failed to add 240-day cups: %v", err)
			}
		}
	}

	if wasOnSickLeave {
		warningMessage := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("⚠️ Внимание, %s!\n\nТы забыл отправить #healthy перед тренировкой!\n\n✅ Я автоматически засчитал выздоровление, но в следующий раз не забывай отправлять #healthy перед #training_done", username))
		b.api.Send(warningMessage)

		messageLog.HasSickLeave = false
		messageLog.HasHealthy = true
		messageLog.SickLeaveStartTime = nil
		if err := b.db.SaveMessageLog(messageLog); err != nil {
			b.logger.Errorf("Failed to reset sick leave flags: %v", err)
		}
	}

	b.startTimer(msg.From.ID, msg.Chat.ID, username)

	if weeklyAchievement {
		b.sendWeeklyCupsReward(msg, username, newStreakDays, caloriesToAdd, userGender)
	}
	if twoWeekAchievement {
		b.sendTwoWeekCupsReward(msg, username, newStreakDays, caloriesToAdd, userGender)
	}
	if threeWeekAchievement {
		b.sendThreeWeekCupsReward(msg, username, newStreakDays, caloriesToAdd, userGender)
	}
	if monthlyAchievement {
		b.sendMonthlyCupsReward(msg, username, newStreakDays, caloriesToAdd, userGender)
	}
	if fortyTwoDayAchievement {
		b.sendFortyTwoDayCupsReward(msg, username, newStreakDays, caloriesToAdd, userGender)
	}
	if fiftyDayAchievement {
		b.sendFiftyDayCupsReward(msg, username, newStreakDays, caloriesToAdd, userGender)
	}
	if sixtyDayAchievement {
		b.sendSixtyDayCupsReward(msg, username, newStreakDays, caloriesToAdd, userGender)
	}
	if quarterlyAchievement {
		b.sendQuarterlyCupsReward(msg, username, newStreakDays, caloriesToAdd, userGender)
	}
	if hundredDayAchievement {
		b.sendHundredDayCupsReward(msg, username, newStreakDays, caloriesToAdd, userGender)
	}
	if oneHundredEightyDayAchievement {
		b.sendOneHundredEightyDayCupsReward(msg, username, newStreakDays, caloriesToAdd, userGender)
	}
	if twoHundredDayAchievement {
		b.sendTwoHundredDayCupsReward(msg, username, newStreakDays, caloriesToAdd, userGender)
	}
	if twoHundredFortyDayAchievement {
		b.sendTwoHundredFortyDayCupsReward(msg, username, newStreakDays, caloriesToAdd, userGender)
	}

	if caloriesToAdd > 0 {
		totalCups, _ := b.db.GetUserCups(msg.From.ID, msg.Chat.ID)

		// Проверяем достижение 420 кубков для розыгрыша
		// if totalCups >= 420 && totalCups-caloriesToAdd < 420 {
		// 	b.checkMerchGiveawayCompletion(msg, msg.Chat.ID)
		// }

		// Проверяем супер-уровень 10000 кубков
		if totalCups >= 10000 && totalCups-caloriesToAdd < 10000 {
			b.sendSuperLevelMessage(msg, username, totalCups, userGender)
		}
	}
}

func (b *Bot) calculateCalories(messageLog *domain.MessageLog) (int, int, int, bool, bool, bool, bool, bool, bool, bool, bool, bool, bool, bool, bool) {
	localNow := b.getUserLocalNow(messageLog.TimezoneOffsetFromMoscow)
	today := localNow.Format("2006-01-02")

	if messageLog.LastTrainingDate != nil && *messageLog.LastTrainingDate == today {
		return 0, messageLog.StreakDays, messageLog.CalorieStreakDays, false, false, false, false, false, false, false, false, false, false, false, false
	}

	newStreakDays := 1
	if messageLog.LastTrainingDate != nil {
		yesterdayStr := localNow.AddDate(0, 0, -1).Format("2006-01-02")
		if *messageLog.LastTrainingDate == yesterdayStr {
			newStreakDays = messageLog.StreakDays + 1
		} else {
			newStreakDays = 1
		}
	} else if messageLog.StreakDays > 0 {
		newStreakDays = messageLog.StreakDays + 1
	}

	newCalorieStreakDays := 1
	if messageLog.LastTrainingDate != nil {
		yesterdayStr := localNow.AddDate(0, 0, -1).Format("2006-01-02")
		if *messageLog.LastTrainingDate == yesterdayStr {
			newCalorieStreakDays = messageLog.CalorieStreakDays + 1
		} else {
			newCalorieStreakDays = 1
		}
	} else if messageLog.CalorieStreakDays > 0 {
		newCalorieStreakDays = messageLog.CalorieStreakDays + 1
	}

	caloriesToAdd := newCalorieStreakDays
	if messageLog.HasSickLeave && messageLog.HasHealthy {
		caloriesToAdd += 2
	}

	weeklyAchievement := newStreakDays == 7
	twoWeekAchievement := newStreakDays == 14
	threeWeekAchievement := newStreakDays == 21
	monthlyAchievement := newStreakDays == 30
	fortyTwoDayAchievement := newStreakDays == 42
	fiftyDayAchievement := newStreakDays == 50
	sixtyDayAchievement := newStreakDays == 60
	quarterlyAchievement := newStreakDays == 90
	hundredDayAchievement := newStreakDays == 100
	oneHundredEightyDayAchievement := newStreakDays == 180
	twoHundredDayAchievement := newStreakDays == 200
	twoHundredFortyDayAchievement := newStreakDays == 240

	return caloriesToAdd, newStreakDays, newCalorieStreakDays, weeklyAchievement, twoWeekAchievement, threeWeekAchievement, monthlyAchievement, fortyTwoDayAchievement, fiftyDayAchievement, sixtyDayAchievement, quarterlyAchievement, hundredDayAchievement, oneHundredEightyDayAchievement, twoHundredDayAchievement, twoHundredFortyDayAchievement
}

func (b *Bot) sendStreakReward(
	msg *tgbotapi.Message,
	username string,
	streakDays int,
	caloriesAdded int,
	rewardCups int,
	title string,
	subtitle string,
) {
	totalCalories, err := b.db.GetUserCalories(msg.From.ID, msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get total calories for %s reward: %v", title, err)
		totalCalories = 0
	}

	totalCups, err := b.db.GetUserCups(msg.From.ID, msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get total cups for %s reward: %v", title, err)
		totalCups = 0
	}

	messageText := fmt.Sprintf(`%s!

%s, серия: %d дней подряд.
%s

🔥 +%d калорий (всего: %d)
🏆 +%d кубков (всего: %d)`,
		title,
		username,
		streakDays,
		subtitle,
		caloriesAdded,
		totalCalories,
		rewardCups,
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
	genderNormalized := strings.TrimSpace(strings.ToLower(gender))
	if genderNormalized == "f" {
		return GenderForms{
			Champion:    "чемпионка",
			Accumulated: "накопила",
			Transformed: "превратила",
			Became:      "становилась",
			Ruler:       "владычица",
			Warrior:     "воительница",
			Entered:     "вошла",
			Proved:      "доказала",
			Deserved:    "заслужила",
			Titan:       "титанисса",
			Olympian:    "олимпийская чемпионка",
			Invincible:  "непобедимая",
			Reached:     "достигла",
		}
	}
	return GenderForms{
		Champion:    "чемпион",
		Accumulated: "накопил",
		Transformed: "превратил",
		Became:      "становился",
		Ruler:       "владыка",
		Warrior:     "воин",
		Entered:     "вошёл",
		Proved:      "доказал",
		Deserved:    "заслужил",
		Titan:       "титан",
		Olympian:    "олимпийский чемпион",
		Invincible:  "непобедимый",
		Reached:     "достиг",
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
	return russianPlural(count, "тренировка", "тренировки", "тренировок")
}

// writingSessionsWordForm — «писательская сессия» / «писательские сессии» / «писательских сессий».
func writingSessionsWordForm(count int) string {
	return russianPlural(count, "писательская сессия", "писательские сессии", "писательских сессий")
}

func cupsWordForm(count int) string {
	return russianPlural(count, "кубок", "кубка", "кубков")
}

func (b *Bot) sendWeeklyCupsReward(msg *tgbotapi.Message, username string, streakDays int, caloriesAdded int, userGender string) {
	totalCalories, _ := b.db.GetUserCalories(msg.From.ID, msg.Chat.ID)
	totalCups, _ := b.db.GetUserCups(msg.From.ID, msg.Chat.ID)
	rewardCups := 42
	forms := b.getGenderForms(userGender)

	messageText := fmt.Sprintf(`🏆🏆🏆 НЕВЕРОЯТНО! 🏆🏆🏆

%s, ты тренируешься уже %d дней подряд! 

🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆

🎯 +%d КУБКОВ ЗА ТВОЮ СЕРИЮ %d ДНЕЙ! 🎯

🔥 +%d калорий
🔥 Всего калорий: %d
🏆 +%d кубков
🏆 Всего кубков: %d
🦁 Fat Leopard гордится твоей дисциплиной! 
💪 Ты %s намерение в привычку — это первый шаг к победе!
🔥 Каждый день ты становишься сильнее и выносливее!
⭐ Ты вдохновляешь других начать свой путь!
👑 Ты %s своей воли!
🌟 Продолжай в том же духе — впереди ещё большие достижения!

#seven_days_warrior #42_cups #training_start`,
		username, streakDays, rewardCups, streakDays, caloriesAdded, totalCalories, rewardCups, totalCups, forms.Transformed, forms.Ruler)

	reply := tgbotapi.NewMessage(msg.Chat.ID, messageText)
	b.api.Send(reply)
}

func (b *Bot) sendTwoWeekCupsReward(msg *tgbotapi.Message, username string, streakDays int, caloriesAdded int, userGender string) {
	totalCalories, _ := b.db.GetUserCalories(msg.From.ID, msg.Chat.ID)
	totalCups, _ := b.db.GetUserCups(msg.From.ID, msg.Chat.ID)
	rewardCups := 42

	forms := b.getGenderForms(userGender)

	messageText := fmt.Sprintf(`🏆🏆🏆🏆 ВПЕЧАТЛЯЮЩЕ! 🏆🏆🏆🏆

%s, ты тренируешься уже %d дней подряд! 

🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆

🎯 +%d КУБКОВ ЗА ТВОЮ СЕРИЮ %d ДНЕЙ! 🎯

🔥 +%d калорий
🔥 Всего калорий: %d
🏆 +%d кубков
🏆 Всего кубков: %d
🦁 Fat Leopard видит в тебе настоящую %s! 
💪 Две недели без пропусков — мощный рывок!
🔥 Твоя сила воли растёт с каждым днём!
⭐ Ты уже не просто начинающий — ты на правильном пути!
👑 Каждая тренировка приближает тебя к цели!
🌟 Продолжай в том же духе — ты на пути к величию!

#fourteen_days_champion #42_cups #training_momentum`,
		username, streakDays, rewardCups, streakDays, caloriesAdded, totalCalories, rewardCups, totalCups, forms.Warrior)

	reply := tgbotapi.NewMessage(msg.Chat.ID, messageText)
	b.api.Send(reply)
}

func (b *Bot) sendThreeWeekCupsReward(msg *tgbotapi.Message, username string, streakDays int, caloriesAdded int, userGender string) {
	totalCalories, _ := b.db.GetUserCalories(msg.From.ID, msg.Chat.ID)
	totalCups, _ := b.db.GetUserCups(msg.From.ID, msg.Chat.ID)
	rewardCups := 42

	forms := b.getGenderForms(userGender)

	messageText := fmt.Sprintf(`🏆🏆🏆🏆🏆 ВЕЛИКОЛЕПНО! 🏆🏆🏆🏆🏆

%s, ты тренируешься уже %d дней подряд! 

🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆

🎯 +%d КУБКОВ ЗА ТВОЮ СЕРИЮ %d ДНЕЙ! 🎯

🔥 +%d калорий
🔥 Всего калорий: %d
🏆 +%d кубков
🏆 Всего кубков: %d
🦁 Fat Leopard восхищается твоей дисциплиной! 
💪 Три недели абсолютной преданности — ты %s в топ 5%%!
🔥 Твоя привычка стала частью тебя — это образ жизни!
⭐ Ты %s себе и всем, что можешь всё!
👑 Это уже не усилие — это твоя суть!
🌟 Впереди ещё больше побед — не сбавляй темп!

#twenty_one_days_elite #42_cups #training_lifestyle`,
		username, streakDays, rewardCups, streakDays, caloriesAdded, totalCalories, rewardCups, totalCups, forms.Entered, forms.Proved)

	reply := tgbotapi.NewMessage(msg.Chat.ID, messageText)
	b.api.Send(reply)
}

func (b *Bot) sendMonthlyCupsReward(msg *tgbotapi.Message, username string, streakDays int, caloriesAdded int, userGender string) {
	totalCalories, _ := b.db.GetUserCalories(msg.From.ID, msg.Chat.ID)
	totalCups, _ := b.db.GetUserCups(msg.From.ID, msg.Chat.ID)
	rewardCups := 420
	forms := b.getGenderForms(userGender)

	messageText := fmt.Sprintf(`🏆🏆🏆🏆🏆🏆 МЕСЯЦ ПОБЕДЫ! 🏆🏆🏆🏆🏆🏆

%s, ты тренируешься уже %d дней подряд! 

🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆

🎯 +%d КУБКОВ ЗА ТВОЮ СЕРИЮ %d ДНЕЙ! 🎯

🔥 +%d калорий
🔥 Всего калорий: %d
🏆 +%d кубков
🏆 Всего кубков: %d
🦁 Fat Leopard не может поверить в твою силу воли! 
💪 Ты %s %s дисциплины!
🔥 Месяц абсолютной преданности — это уровень чемпионов!
⭐ Ты не просто тренируешься — ты создаёшь новую версию себя!
👑 Каждый день ты %s лучше, сильнее, увереннее!
🌟 Твоя дисциплина — пример для всех — ты %s!
💎 Ты %s, что можешь достичь ЛЮБОЙ цели — небо не предел!

#thirty_days_champion #420_cups #training_perfection`,
		username, streakDays, rewardCups, streakDays, caloriesAdded, totalCalories, rewardCups, totalCups, forms.Invincible, forms.Titan, forms.Became, forms.Champion, forms.Proved)

	reply := tgbotapi.NewMessage(msg.Chat.ID, messageText)
	b.api.Send(reply)
}

func (b *Bot) sendFortyTwoDayCupsReward(msg *tgbotapi.Message, username string, streakDays int, caloriesAdded int, userGender string) {
	totalCalories, _ := b.db.GetUserCalories(msg.From.ID, msg.Chat.ID)
	totalCups, _ := b.db.GetUserCups(msg.From.ID, msg.Chat.ID)
	rewardCups := 42
	forms := b.getGenderForms(userGender)

	messageText := fmt.Sprintf(`🏆🏆🏆🏆🏆🏆 ЛЕГЕНДАРНО! 🏆🏆🏆🏆🏆🏆

%s, ты тренируешься уже %d дней подряд! 

🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆

🎯 +%d КУБКОВ ЗА ТВОЮ СЕРИЮ %d ДНЕЙ! 🎯

🔥 +%d калорий
🔥 Всего калорий: %d
🏆 +%d кубков
🏆 Всего кубков: %d
🦁 Fat Leopard признаёт тебя символом стаи! 
💪 42 дня — это легендарная отметка чемпионов!
🔥 Шесть недель абсолютной преданности — уровень мастерства!
⭐ Ты не просто тренируешься — ты создаёшь новую версию себя!
👑 Твоя сила воли теперь невероятна — ты это %s!
🌟 Продолжай — ты на пути к легенде!

#forty_two_days_legend #42_cups #training_mastery`,
		username, streakDays, rewardCups, streakDays, caloriesAdded, totalCalories, rewardCups, totalCups, forms.Deserved)

	reply := tgbotapi.NewMessage(msg.Chat.ID, messageText)
	b.api.Send(reply)
}

func (b *Bot) sendFiftyDayCupsReward(msg *tgbotapi.Message, username string, streakDays int, caloriesAdded int, userGender string) {
	totalCalories, _ := b.db.GetUserCalories(msg.From.ID, msg.Chat.ID)
	totalCups, _ := b.db.GetUserCups(msg.From.ID, msg.Chat.ID)
	rewardCups := 42

	forms := b.getGenderForms(userGender)

	messageText := fmt.Sprintf(`🏆🏆🏆🏆🏆🏆🏆 ЭЛИТА! 🏆🏆🏆🏆🏆🏆🏆

%s, ты тренируешься уже %d дней подряд! 

🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆

🎯 +%d КУБКОВ ЗА ТВОЮ СЕРИЮ %d ДНЕЙ! 🎯

🔥 +%d калорий
🔥 Всего калорий: %d
🏆 +%d кубков
🏆 Всего кубков: %d
🦁 Fat Leopard видит в тебе %s! 
💪 Полсотни дней без пропусков — ты %s в элиту!
🔥 50 дней — это доказательство того, что ты можешь всё!
⭐ Твоя дисциплина на уровне лучших спортсменов мира!
👑 Каждая тренировка — инвестиция в себя, и ты видишь результаты!
🌟 Не останавливайся — впереди ещё больше возможностей!

#fifty_days_olympian #42_cups #training_excellence`,
		username, streakDays, rewardCups, streakDays, caloriesAdded, totalCalories, rewardCups, totalCups, forms.Olympian, forms.Entered)

	reply := tgbotapi.NewMessage(msg.Chat.ID, messageText)
	b.api.Send(reply)
}

func (b *Bot) sendSixtyDayCupsReward(msg *tgbotapi.Message, username string, streakDays int, caloriesAdded int, userGender string) {
	b.sendStreakReward(msg, username, streakDays, caloriesAdded, 420, "🏆 60 дней!", "🔥 Два месяца без провала — легенда растёт.")
}

func (b *Bot) sendQuarterlyCupsReward(msg *tgbotapi.Message, username string, streakDays int, caloriesAdded int, userGender string) {
	b.sendStreakReward(msg, username, streakDays, caloriesAdded, 420, "🏆 90 дней!", "🏁 Квартал дисциплины — элита так и тренируется.")
}

func (b *Bot) sendHundredDayCupsReward(msg *tgbotapi.Message, username string, streakDays int, caloriesAdded int, userGender string) {
	totalCalories, _ := b.db.GetUserCalories(msg.From.ID, msg.Chat.ID)
	totalCups, _ := b.db.GetUserCups(msg.From.ID, msg.Chat.ID)
	rewardCups := 4200
	forms := b.getGenderForms(userGender)

	messageText := fmt.Sprintf(`🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆 БЕССМЕРТНАЯ ЛЕГЕНДА! 🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆

%s, ты тренируешься уже %d дней подряд! 

🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆

🎯 +%d КУБКОВ ЗА ТВОЮ СЕРИЮ %d ДНЕЙ! 🎯

🔥 +%d калорий
🔥 Всего калорий: %d
🏆 +%d кубков
🏆 Всего кубков: %d
🦁 Fat Leopard не может поверить в твою силу воли! 
💪 Ты %s %s дисциплины!
🔥 100 дней абсолютной преданности — это уровень единиц!
⭐ Ты не просто тренируешься — ты создаёшь историю!
👑 Каждый день ты %s лучше, сильнее, увереннее!
🌟 Твоя дисциплина — пример для всех — ты %s!
💎 Ты %s, что можешь достичь ЛЮБОЙ цели — небо не предел!

#hundred_days_immortal #4200_cups #training_perfection`,
		username, streakDays, rewardCups, streakDays, caloriesAdded, totalCalories, rewardCups, totalCups, forms.Invincible, forms.Titan, forms.Became, forms.Champion, forms.Proved)

	reply := tgbotapi.NewMessage(msg.Chat.ID, messageText)
	b.api.Send(reply)
}

func (b *Bot) sendOneHundredEightyDayCupsReward(msg *tgbotapi.Message, username string, streakDays int, caloriesAdded int, userGender string) {
	b.sendStreakReward(msg, username, streakDays, caloriesAdded, 420, "🏆 180 дней!", "🔥 Полгода серии — +420 кубков!")
}

func (b *Bot) sendTwoHundredDayCupsReward(msg *tgbotapi.Message, username string, streakDays int, caloriesAdded int, userGender string) {
	totalCalories, _ := b.db.GetUserCalories(msg.From.ID, msg.Chat.ID)
	totalCups, _ := b.db.GetUserCups(msg.From.ID, msg.Chat.ID)
	rewardCups := 4200
	messageText := fmt.Sprintf(`🌸 БУКЕТ ИЗ КУБКОВ! 🌸

%s, 200 дней подряд!

🏆🌸🏆🌸🏆🌸🏆🌸🏆🌸🏆🌸🏆
🌸🏆🌸🏆🌸🏆🌸🏆🌸🏆🌸🏆🌸
🏆🌸🏆🌸🏆🌸🏆🌸🏆🌸🏆🌸🏆

🎯 +%d кубков — букет из кубков за твою серию %d дней!

🔥 +%d калорий (всего: %d)
🏆 +%d кубков (всего: %d)
🦁 Fat Leopard дарит тебе этот букет — ты легенда!

#two_hundred_days #bouquet_of_cups #4200_cups`,
		username, rewardCups, streakDays, caloriesAdded, totalCalories, rewardCups, totalCups)
	reply := tgbotapi.NewMessage(msg.Chat.ID, messageText)
	b.api.Send(reply)
}

func (b *Bot) sendTwoHundredFortyDayCupsReward(msg *tgbotapi.Message, username string, streakDays int, caloriesAdded int, userGender string) {
	b.sendStreakReward(msg, username, streakDays, caloriesAdded, 4200, "🏆 240 дней!", "🔥 240 дней серии — уровень титана!")
}

func (b *Bot) sendSuperLevelMessage(msg *tgbotapi.Message, username string, totalCups int, userGender string) {
	// Определяем тип чата для адаптации текста
	chatType, err := b.db.GetChatType(msg.Chat.ID)
	if err != nil {
		chatType = "training" // По умолчанию
	}

	forms := b.getGenderForms(userGender)

	var messageText string
	if chatType == "writing" {
		messageText = fmt.Sprintf(`🌟⚡️ СУПЕР-УРОВЕНЬ ДОСТИГНУТ! ⚡️🌟

%s, ты %s %d кубков! 

🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆

🎊 ВСЕ ОЖИДАНИЯ ПРЕВЗОЙДЕНЫ! 🎊

🦁 Fat Leopard в полном восторге! 
💪 Ты не просто %s - ты СУПЕР-%s!
🔥 Твоя сила и мощь безграничны!
⭐️ Ты вдохновляешь всю стаю!
👑 Мотивация не верит, что такое бывает!
🌟 Ты сияешь ярче всех!

🎯 Продолжай писать, супер-леопард!

#super_level #%d_cups #writing_master`,
			username, forms.Accumulated, totalCups, forms.Champion, strings.ToUpper(forms.Champion), totalCups)
	} else {
		messageText = fmt.Sprintf(`🌟⚡️ СУПЕР-УРОВЕНЬ ДОСТИГНУТ! ⚡️🌟

%s, ты %s %d кубков! 

🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆

🎊 ВСЕ ОЖИДАНИЯ ПРЕВЗОЙДЕНЫ! 🎊

🦁 Fat Leopard в полном восторге! 
💪 Ты не просто %s - ты СУПЕР-%s!
🔥 Твоя сила и мощь безграничны!
⭐️ Ты вдохновляешь всю стаю!
👑 Мотивация не верит, что такое бывает!
🌟 Ты сияешь ярче всех!

🎯 Продолжай в том же духе, супер-леопард!

#super_level #%d_cups #motivation_king`,
			username, forms.Accumulated, totalCups, forms.Champion, strings.ToUpper(forms.Champion), totalCups)
	}

	reply := tgbotapi.NewMessage(msg.Chat.ID, messageText)
	if _, err := b.api.Send(reply); err != nil {
		b.logger.Errorf("Failed to send super level message: %v", err)
	}
}

// checkMerchGiveawayCompletion проверяет, завершен ли розыгрыш (3 участника с 420+ кубками)
func (b *Bot) checkMerchGiveawayCompletion(msg *tgbotapi.Message, chatID int64) {
	usersWith420Cups, err := b.db.CountUsersWithCups(chatID, 420)
	if err != nil {
		b.logger.Errorf("Failed to count users with 420+ cups: %v", err)
		return
	}

	if usersWith420Cups == 3 {
		// 3-й участник достиг 420 кубков - розыгрыш завершен
		merchMessage := tgbotapi.NewMessage(chatID, `🎉🎊 РОЗЫГРЫШ ЗАВЕРШЕН! 🎊🎉

Третий участник достиг 420 кубков! 

🏆 Розыгрыш футболки Fat Leopard официально закрыт!

Поздравляем всех участников, которые набрали 420+ кубков! 🦁💪`)
		if _, err := b.api.Send(merchMessage); err != nil {
			b.logger.Errorf("Failed to send merch giveaway completion message: %v", err)
		} else {
			b.logger.Infof("Merch giveaway completed! 3 participants reached 420+ cups in chat %d", chatID)
		}
	}
}
