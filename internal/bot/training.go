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

	caloriesToAdd, newStreakDays, newCalorieStreakDays, weeklyAchievement, twoWeekAchievement, threeWeekAchievement, monthlyAchievement, fortyTwoDayAchievement, fiftyDayAchievement, sixtyDayAchievement, quarterlyAchievement, hundredDayAchievement := b.calculateCalories(messageLog)

	b.logger.Infof("DEBUG handleTrainingDone: caloriesToAdd=%d, newStreakDays=%d, newCalorieStreakDays=%d, weeklyAchievement=%t, twoWeekAchievement=%t, threeWeekAchievement=%t, monthlyAchievement=%t, fortyTwoDayAchievement=%t, fiftyDayAchievement=%t, sixtyDayAchievement=%t, quarterlyAchievement=%t, hundredDayAchievement=%t",
		caloriesToAdd, newStreakDays, newCalorieStreakDays, weeklyAchievement, twoWeekAchievement, threeWeekAchievement, monthlyAchievement, fortyTwoDayAchievement, fiftyDayAchievement, sixtyDayAchievement, quarterlyAchievement, hundredDayAchievement)

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
		today := utils.GetMoscowDate()

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
			if err := b.db.AddCups(msg.From.ID, msg.Chat.ID, 84); err != nil {
				b.logger.Errorf("Failed to add two-week cups: %v", err)
			}
		}

		if threeWeekAchievement {
			if err := b.db.AddCups(msg.From.ID, msg.Chat.ID, 126); err != nil {
				b.logger.Errorf("Failed to add three-week cups: %v", err)
			}
		}

		if monthlyAchievement {
			if err := b.db.AddCups(msg.From.ID, msg.Chat.ID, 200); err != nil {
				b.logger.Errorf("Failed to add monthly cups: %v", err)
			}
		}

		if fortyTwoDayAchievement {
			if err := b.db.AddCups(msg.From.ID, msg.Chat.ID, 300); err != nil {
				b.logger.Errorf("Failed to add 42-day cups: %v", err)
			}
		}

		if fiftyDayAchievement {
			if err := b.db.AddCups(msg.From.ID, msg.Chat.ID, 360); err != nil {
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
		b.sendWeeklyCupsReward(msg, username, newStreakDays, caloriesToAdd)
	}
	if twoWeekAchievement {
		b.sendTwoWeekCupsReward(msg, username, newStreakDays, caloriesToAdd)
	}
	if threeWeekAchievement {
		b.sendThreeWeekCupsReward(msg, username, newStreakDays, caloriesToAdd)
	}
	if monthlyAchievement {
		b.sendMonthlyCupsReward(msg, username, newStreakDays, caloriesToAdd)
	}
	if fortyTwoDayAchievement {
		b.sendFortyTwoDayCupsReward(msg, username, newStreakDays, caloriesToAdd)
	}
	if fiftyDayAchievement {
		b.sendFiftyDayCupsReward(msg, username, newStreakDays, caloriesToAdd)
	}
	if sixtyDayAchievement {
		b.sendSixtyDayCupsReward(msg, username, newStreakDays, caloriesToAdd)
	}
	if quarterlyAchievement {
		b.sendQuarterlyCupsReward(msg, username, newStreakDays, caloriesToAdd)
	}
	if hundredDayAchievement {
		b.sendHundredDayCupsReward(msg, username, newStreakDays, caloriesToAdd)
	}

	if caloriesToAdd > 0 {
		totalCups, _ := b.db.GetUserCups(msg.From.ID, msg.Chat.ID)
		if totalCups >= 10000 && totalCups-caloriesToAdd < 10000 {
			b.sendSuperLevelMessage(msg, username, totalCups)
		}
	}
}

func (b *Bot) calculateCalories(messageLog *domain.MessageLog) (int, int, int, bool, bool, bool, bool, bool, bool, bool, bool, bool) {
	today := utils.GetMoscowDate()

	if messageLog.LastTrainingDate != nil && *messageLog.LastTrainingDate == today {
		return 0, messageLog.StreakDays, messageLog.CalorieStreakDays, false, false, false, false, false, false, false, false, false
	}

	newStreakDays := 1
	if messageLog.LastTrainingDate != nil {
		yesterday := utils.GetMoscowTime().AddDate(0, 0, -1)
		yesterdayStr := utils.GetMoscowDateFromTime(yesterday)
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
		yesterday := utils.GetMoscowTime().AddDate(0, 0, -1)
		yesterdayStr := utils.GetMoscowDateFromTime(yesterday)
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

	return caloriesToAdd, newStreakDays, newCalorieStreakDays, weeklyAchievement, twoWeekAchievement, threeWeekAchievement, monthlyAchievement, fortyTwoDayAchievement, fiftyDayAchievement, sixtyDayAchievement, quarterlyAchievement, hundredDayAchievement
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

func (b *Bot) sendWeeklyCupsReward(msg *tgbotapi.Message, username string, streakDays int, caloriesAdded int) {
	b.sendStreakReward(msg, username, streakDays, caloriesAdded, 42, "🏆 7 дней!", "🎯 Недельная планка покорена, держи темп!")
}

func (b *Bot) sendTwoWeekCupsReward(msg *tgbotapi.Message, username string, streakDays int, caloriesAdded int) {
	b.sendStreakReward(msg, username, streakDays, caloriesAdded, 84, "🏆 14 дней!", "🔥 Две недели подряд — мощный рывок!")
}

func (b *Bot) sendThreeWeekCupsReward(msg *tgbotapi.Message, username string, streakDays int, caloriesAdded int) {
	b.sendStreakReward(msg, username, streakDays, caloriesAdded, 126, "🏆 21 день!", "⚡️ Три недели дисциплины подряд.")
}

func (b *Bot) sendMonthlyCupsReward(msg *tgbotapi.Message, username string, streakDays int, caloriesAdded int) {
	b.sendStreakReward(msg, username, streakDays, caloriesAdded, 200, "🏆 30 дней!", "💥 Месяц без пауз — ты держишь линию, продолжай.")
}

func (b *Bot) sendFortyTwoDayCupsReward(msg *tgbotapi.Message, username string, streakDays int, caloriesAdded int) {
	b.sendStreakReward(msg, username, streakDays, caloriesAdded, 300, "🏆 42 дня!", "🦁 Символ стаи — 42. Ты доказал, что достоин её поддержки.")
}

func (b *Bot) sendFiftyDayCupsReward(msg *tgbotapi.Message, username string, streakDays int, caloriesAdded int) {
	b.sendStreakReward(msg, username, streakDays, caloriesAdded, 360, "🏆 50 дней!", "🚀 Полсотни подряд — дисциплина на уровне пилота-истребителя.")
}

func (b *Bot) sendSixtyDayCupsReward(msg *tgbotapi.Message, username string, streakDays int, caloriesAdded int) {
	b.sendStreakReward(msg, username, streakDays, caloriesAdded, 420, "🏆 60 дней!", "🔥 Два месяца без провала — легенда растёт.")
}

func (b *Bot) sendQuarterlyCupsReward(msg *tgbotapi.Message, username string, streakDays int, caloriesAdded int) {
	b.sendStreakReward(msg, username, streakDays, caloriesAdded, 420, "🏆 90 дней!", "🏁 Квартал дисциплины — элита так и тренируется.")
}

func (b *Bot) sendHundredDayCupsReward(msg *tgbotapi.Message, username string, streakDays int, caloriesAdded int) {
	b.sendStreakReward(msg, username, streakDays, caloriesAdded, 4200, "🏆 100 дней!", "💎 Столетняя серия — высший уровень контроля.")
}

func (b *Bot) sendSuperLevelMessage(msg *tgbotapi.Message, username string, totalCups int) {
	messageText := fmt.Sprintf(`🔥 Суперуровень открыт!

%s, ты набрал %d кубков. Это верхняя планка стаи.
Дальше — только ещё больше дисциплины и собственных рекордов.`,
		username,
		totalCups,
	)

	reply := tgbotapi.NewMessage(msg.Chat.ID, messageText)
	if _, err := b.api.Send(reply); err != nil {
		b.logger.Errorf("Failed to send super level message: %v", err)
	}
}
