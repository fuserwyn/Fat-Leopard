package bot

import (
	"errors"
	"fmt"
	"strings"

	"leo-bot/internal/domain"
	"leo-bot/internal/game/leopardmoney"
	"leo-bot/internal/usecase/sickleave"
	"leo-bot/internal/utils"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Разбор входящих сообщений: отчёты о тренировках, больничный, таймер.

func (b *Bot) handleMessage(msg *tgbotapi.Message, personalReplyCh chan<- string, trainingPhotoURLOverride string) {
	// Проверяем наличие хештегов в тексте или подписи
	text := msg.Text
	if text == "" && msg.Caption != "" {
		text = msg.Caption
	}

	b.tryHandleSickApprovalReply(msg, text)

	if text != "" && strings.Contains(strings.ToLower(text), "#change") {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "⚠️ #change больше не работает: обмен калорий убран. Сейчас в игре кубки и стрик — отмечай тренировки в мини-аппе («+»).")
		if _, err := b.api.Send(reply); err != nil {
			b.logger.Errorf("send #change deprecation reply: %v", err)
		}
		return
	}

	// Хештеги команд (#sick_leave, #healthy, …). Отчёт о тренировке — только формат мини-аппа, не #training_done.
	hasTrainingReport := leopardmoney.IsTrainingReportLine(text)
	hasSickLeave := strings.Contains(strings.ToLower(text), "#sick_leave")
	hasHealthy := strings.Contains(strings.ToLower(text), "#healthy")
	hasCommand := hasTrainingReport || hasSickLeave || hasHealthy

	// Если есть команда, обрабатываем её и НЕ обрабатываем через ИИ
	if hasCommand {
		// Получаем никнейм пользователя
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

		var trainingPhotoURL string
		alreadyOnSickLeave := false
		if hasSickLeave {
			// Стейт больничного храним на pack-row; если больничный уже активен —
			// повторный #sick_leave не пишем в ленту второй раз.
			if kickLog, kerr := b.db.GetMessageLog(msg.From.ID, b.kickChatIDForMessage(msg)); kerr == nil && kickLog != nil && kickLog.HasSickLeave {
				alreadyOnSickLeave = true
			}
		}
		stateChatID := b.packTrainingStateChatID(msg)

		if hasTrainingReport {
			if trainingPhotoURLOverride != "" {
				trainingPhotoURL = trainingPhotoURLOverride
			} else {
				trainingPhotoURL = b.takeMiniappTrainingPhotoURL(msg.From.ID)
			}
		}
		var trainingDoneFeedMsgID int64
		if text != "" && !(hasSickLeave && alreadyOnSickLeave) {
			messageType := "general"
			if hasTrainingReport {
				messageType = "training_done"
			} else if hasSickLeave {
				messageType = "sick_leave"
			} else if hasHealthy {
				messageType = "healthy"
			}

			userMsg := &domain.UserMessage{
				UserID:           msg.From.ID,
				ChatID:           msg.Chat.ID,
				Username:         username,
				MessageText:      text,
				MessageType:      messageType,
				TrainingPhotoURL: trainingPhotoURL,
			}
			if hasTrainingReport {
				id, err := b.db.SaveUserMessageReturningID(userMsg)
				if err != nil {
					b.logger.Errorf("Failed to save user message: %v", err)
				} else {
					trainingDoneFeedMsgID = id
					// Лента мини-аппа и реакции/треды читают user_messages с chat_id = стая.
					// Отчёт из лички / мини-аппа пишется с chat_id = private (как у Telegram); дублируем строку для ленты.
					if b.config.MonetizedChatID != 0 && msg.Chat != nil && msg.Chat.Type == "private" {
						mirror := &domain.UserMessage{
							UserID:           userMsg.UserID,
							ChatID:           b.config.MonetizedChatID,
							Username:         userMsg.Username,
							MessageText:      userMsg.MessageText,
							MessageType:      userMsg.MessageType,
							TrainingPhotoURL: trainingPhotoURL,
						}
						feedID, errM := b.db.SaveUserMessageReturningID(mirror)
						if errM != nil {
							b.logger.Warnf("mirror training_done to pack feed user_messages: %v", errM)
						} else {
							trainingDoneFeedMsgID = feedID
						}
					}
				}
			} else {
				// #sick_leave / #healthy — приватные события, в ленту стаи их не дублируем
				// (только #training_done зеркалится в pack feed выше).
				if err := b.db.SaveUserMessage(userMsg); err != nil {
					b.logger.Errorf("Failed to save user message: %v", err)
				}
			}
		}

		// Получаем существующие данные пользователя
		existingLog, err := b.db.GetMessageLog(msg.From.ID, stateChatID)
		if err != nil {
			// Если пользователя нет в БД, создаем новую запись
			timerStartTime := utils.FormatMoscowTime(utils.GetMoscowTime())
			messageLog := &domain.MessageLog{
				UserID:          msg.From.ID,
				ChatID:          stateChatID,
				Username:        username,
				StreakDays:      0,
				CupsEarned:      0,
				LastMessage:     timerStartTime,
				HasTrainingDone: hasTrainingReport,
				HasSickLeave:    false,
				HasHealthy:      false,
				IsDeleted:       false,
				TimerStartTime:  &timerStartTime,
			}

			if err := b.db.SaveMessageLog(messageLog); err != nil {
				b.logger.Errorf("Failed to save message log: %v", err)
			} else {
				b.logger.Infof("Initialized timer state for new user %d (%s) from message", msg.From.ID, username)
				b.startTimer(msg.From.ID, stateChatID, username)
			}
		} else {
			// Обновляем только необходимые поля, сохраняя streak данные
			existingLog.Username = username
			existingLog.LastMessage = utils.FormatMoscowTime(utils.GetMoscowTime())
			existingLog.HasTrainingDone = hasTrainingReport
			existingLog.IsDeleted = false

			if err := b.db.SaveMessageLog(existingLog); err != nil {
				b.logger.Errorf("Failed to update message log: %v", err)
			}
		}

		// Обрабатываем хештеги
		if hasTrainingReport {
			b.handleTrainingDone(msg, personalReplyCh, trainingDoneFeedMsgID)
		} else if hasSickLeave {
			b.handleSickLeave(msg)
		} else if hasHealthy {
			b.handleHealthy(msg)
		}
		return // Выходим, не обрабатывая через ИИ
	}

	// В режиме поддержки в личке — только в поддержку, не в Лео.
	if msg.Chat != nil && msg.Chat.IsPrivate() && msg.From != nil && b.userInSupportSession(msg.From.ID) {
		if text != "" || msg.Caption != "" {
			_ = b.handleUserSupportFlowMessage(msg)
			return
		}
	}

	// Если нет команд — вопросы к ИИ: в личке с ботом — любой текст; в группах — @ или ответ на бота.
	shouldHandleAI := false
	// Личка: Type == "private" или (иногда) пустой Type — в личке chat_id совпадает с id отправителя.
	if msg.Chat != nil && text != "" && msg.From != nil &&
		(msg.Chat.IsPrivate() || msg.Chat.ID == msg.From.ID) {
		shouldHandleAI = true
	} else {
		// Проверяем упоминание через @ в тексте
		if msg.Entities != nil && text != "" {
			for _, entity := range msg.Entities {
				if entity.Type == "mention" {
					mentionText := ""
					if entity.Offset+entity.Length <= len(text) {
						mentionText = text[entity.Offset : entity.Offset+entity.Length]
					}

					botUsername := b.api.Self.UserName
					if botUsername == "" {
						botUsername = strings.TrimPrefix(mentionText, "@")
					}

					if strings.EqualFold(mentionText, "@"+botUsername) ||
						strings.EqualFold(mentionText, botUsername) ||
						strings.Contains(strings.ToLower(text), "@"+strings.ToLower(botUsername)) ||
						strings.Contains(strings.ToLower(text), strings.ToLower(botUsername)+" ") {
						shouldHandleAI = true
						b.logger.Infof("Bot mention detected: %s in message: %s", mentionText, text)
						break
					}
				}
			}
		}

		if !shouldHandleAI && msg.ReplyToMessage != nil {
			if msg.ReplyToMessage.From != nil && msg.ReplyToMessage.From.IsBot &&
				msg.ReplyToMessage.From.ID == b.api.Self.ID {
				shouldHandleAI = true
				b.logger.Infof("Reply to bot message detected")
			}
		}
	}

	// Реплика из мини-аппа пришла в TG с префиксом — не запускаем ИИ повторно (ответ уже в приложении или не было @).
	if msg.Chat != nil && msg.Chat.ID == b.config.MonetizedChatID &&
		(msg.Chat.Type == "supergroup" || msg.Chat.Type == "group") &&
		text != "" && strings.HasPrefix(strings.TrimSpace(text), "💬 Мини-апп") {
		shouldHandleAI = false
	}

	// Если обращение к боту обнаружено и есть текст вопроса
	if shouldHandleAI && text != "" {
		isPrivateLeo := msg.Chat != nil && msg.From != nil &&
			(msg.Chat.IsPrivate() || msg.Chat.ID == msg.From.ID)
		if isPrivateLeo {
			if _, err := b.enforceLeoChat(text, msg.From.ID); err != nil {
				var mod *ModerationBlockedError
				reply := "⚠️ Сообщение не отправлено."
				if errors.As(err, &mod) && mod != nil && strings.TrimSpace(mod.Message) != "" {
					reply = "⚠️ " + mod.Message
				}
				miniReply := func(s string) {
					if personalReplyCh == nil || s == "" {
						return
					}
					select {
					case personalReplyCh <- s:
					default:
					}
				}
				miniReply(reply)
				if personalReplyCh == nil && b.api != nil {
					b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, reply))
				}
				return
			}
		}

		// Сохраняем вопрос в БД перед обработкой
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

		userMsg := &domain.UserMessage{
			UserID:      msg.From.ID,
			ChatID:      msg.Chat.ID,
			Username:    username,
			MessageText: text,
			MessageType: "question", // Отмечаем как вопрос к ИИ
		}
		if err := b.db.SaveUserMessage(userMsg); err != nil {
			b.logger.Errorf("Failed to save user question: %v", err)
		}

		// Нативная личка Telegram: дублируем в miniapp_personal_chat, чтобы вкладка «Лео» в мини-аппе
		// совпадала с перепиской в TG (пишем в БД и при personalReplyCh!=nil — уже сделано в processMiniAppPrivateCore).
		if personalReplyCh == nil && msg.Chat != nil && msg.From != nil &&
			(msg.Chat.IsPrivate() || msg.Chat.ID == msg.From.ID) {
			b.savePersonalChatMessage(msg.From.ID, "user", text)
		}

		compactCtx := personalReplyCh != nil
		if !compactCtx && msg.Chat != nil {
			if msg.Chat.IsPrivate() || (msg.From != nil && msg.Chat.ID == msg.From.ID) {
				compactCtx = true // личка: меньше контекста — быстрее ответ без потери смысла
			}
		}

		b.handleAIQuestion(msg, text, personalReplyCh, personalReplyCh != nil, compactCtx)
		return
	}

	// Если дошли сюда, значит нет ни команд, ни обращения к боту - сохраняем обычное сообщение в БД
	if text != "" {
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

		// Сохраняем в user_messages для контекста
		userMsg := &domain.UserMessage{
			UserID:      msg.From.ID,
			ChatID:      msg.Chat.ID,
			Username:    username,
			MessageText: text,
			MessageType: "general", // Обычное сообщение
		}
		if err := b.db.SaveUserMessage(userMsg); err != nil {
			b.logger.Errorf("Failed to save user message: %v", err)
		}

		// Обновляем LastMessage в training_state
		rowChat := b.packTrainingStateChatID(msg)
		messageLog, err := b.db.GetMessageLog(msg.From.ID, rowChat)
		if err == nil {
			messageLog.Username = username
			messageLog.LastMessage = text
			messageLog.IsDeleted = false
			if err := b.db.SaveMessageLog(messageLog); err != nil {
				b.logger.Errorf("Failed to update message log: %v", err)
			}
		}
	}
}

func (b *Bot) handleTrainingDone(msg *tgbotapi.Message, personalReplyCh chan<- string, trainingUserMessageID int64) {
	b.handleLeopardMoneyTrainingDone(msg, personalReplyCh, trainingUserMessageID)
}

func (b *Bot) evaluateSickLeaveJustification(text string, messageLog *domain.MessageLog) bool {
	clean := strings.TrimSpace(strings.ToLower(text))
	clean = strings.ReplaceAll(clean, "#sick_leave", "")
	clean = strings.ReplaceAll(clean, "#sickleave", "")
	clean = strings.ReplaceAll(clean, "#healthy", "")
	clean = strings.ReplaceAll(clean, "#здоров", "")

	heuristicsApprove, hasNegative := sickleave.EvaluateHeuristics(clean)

	if heuristicsApprove {
		return true
	}
	if hasNegative {
		return false
	}
	if b.aiClient == nil || clean == "" {
		return false
	}

	var ctxBuilder strings.Builder
	ctxBuilder.WriteString("Оцени убедительность больничного запроса.\n")
	if messageLog != nil {
		ctxBuilder.WriteString(fmt.Sprintf("Пользователь: %s\n", messageLog.Username))
		ctxBuilder.WriteString(fmt.Sprintf("StreakDays: %d\n", messageLog.StreakDays))
		ctxBuilder.WriteString(fmt.Sprintf("HasSickLeave: %t\n", messageLog.HasSickLeave))
		ctxBuilder.WriteString(fmt.Sprintf("HasHealthy: %t\n", messageLog.HasHealthy))
	}
	ctxBuilder.WriteString(fmt.Sprintf("Текст запроса: \"%s\"\n", clean))
	ctxBuilder.WriteString("Эвристика не нашла явных признаков ни болезни, ни обмана.\n")

	question := "Если сообщение описывает реальную болезнь, ответь строго словом APPROVE. " +
		"Если это похоже на отговорку (работа, дела, лень и т.п.), ответь строго словом REJECT. " +
		"Никаких других слов или пояснений."

	answer, err := b.aiClient.AnswerUserQuestion(question, ctxBuilder.String())
	if err != nil {
		b.logger.Errorf("AI sick leave evaluation failed: %v", err)
		return false
	}

	normalized := strings.ToUpper(strings.TrimSpace(answer))
	if strings.Contains(normalized, "APPROVE") {
		return true
	}
	if strings.Contains(normalized, "REJECT") {
		return false
	}

	return false
}

func (b *Bot) handleStartTimer(msg *tgbotapi.Message) {
	// Проверяем права администратора
	if !b.isAdmin(msg.Chat.ID, msg.From.ID) {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Только администраторы или владелец могут использовать эту команду!")
		b.api.Send(reply)
		return
	}

	packScope := b.packTrainingStateChatID(msg)

	// Получаем всех пользователей в чате
	users, err := b.db.GetUsersByChatID(packScope)
	if err != nil {
		b.logger.Errorf("Failed to get users: %v", err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Ошибка при получении пользователей")
		b.api.Send(reply)
		return
	}

	// Запускаем таймеры для всех пользователей
	startedCount := 0
	for _, user := range users {
		if b.isUserInChat(msg.Chat.ID, user.UserID) {
			b.startTimer(user.UserID, packScope, "")
			startedCount++
		}
	}

	// Отправляем отчет
	reply := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("🐆 Fat Leopard активирован!\n\n⏱️ Запущено таймеров: %d\n⏰ Время: 7 дней\n💪 Действие: отметь тренировку в мини-аппе", startedCount))

	b.logger.Infof("Sending start timer message to chat %d", msg.Chat.ID)
	_, err = b.api.Send(reply)
	if err != nil {
		b.logger.Errorf("Failed to send start timer message: %v", err)
	} else {
		b.logger.Infof("Successfully sent start timer message to chat %d", msg.Chat.ID)
	}
}
