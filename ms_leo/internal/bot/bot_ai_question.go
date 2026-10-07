package bot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"leo-bot/internal/ai"
	"leo-bot/internal/domain"
	"leo-bot/internal/prompts"
	"leo-bot/internal/rag"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Ответ Лео на вопрос пользователя.

// handleAIQuestion обрабатывает вопрос пользователя к ИИ.
// personalReplyCh — дублирует доставленный в Telegram текст в Mini App (HTTP reply_text), если задан.
// skipTelegram — не слать в Telegram (общий чат мини-апpa: ответ только в БД/HTTP).
// compactContext — меньше истории/без RAG-примеров: быстрее и меньше токенов (мини-апп HTTP и pack-чат).
func (b *Bot) handleAIQuestion(msg *tgbotapi.Message, questionText string, personalReplyCh chan<- string, skipTelegram bool, compactContext bool) {
	b.logger.Infof("handleAIQuestion called for user %d with text: %s", msg.From.ID, questionText)
	miniReply := func(s string) {
		if personalReplyCh == nil || s == "" {
			return
		}
		select {
		case personalReplyCh <- s:
		default:
			b.logger.Warnf("miniapp reply channel full, drop duplicate fragment user_id=%d", msg.From.ID)
		}
	}

	if b.aiClient == nil {
		b.logger.Warn("AI client is nil, cannot process question")
		help := "❌ ИИ функции недоступны. Проверьте настройки OpenRouter API."
		miniReply(help)
		if !skipTelegram {
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, help))
		}
		return
	}

	// Удаляем упоминание бота из вопроса
	botUsername := b.api.Self.UserName
	if botUsername != "" {
		questionText = strings.ReplaceAll(questionText, "@"+botUsername, "")
		questionText = strings.ReplaceAll(questionText, botUsername, "")
	}
	// Удаляем все упоминания в формате @username
	questionText = strings.ReplaceAll(questionText, "@", "")
	questionText = strings.TrimSpace(questionText)

	if questionText == "" {
		b.logger.Infof("Question text is empty after cleaning")
		hint := "💬 Привет! 👋 Задай мне вопрос!\n\nНапример:\n• Что я делал вчера?\n• Как мой прогресс?\n• Что улучшить в тренировках?\n• Как лечиться?"
		miniReply(hint)
		if !skipTelegram {
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, hint))
		}
		return
	}

	b.logger.Infof("Processing AI question: %s", questionText)

	stateChat := b.packTrainingStateChatID(msg)
	ctxChannel := b.aiContextChannel(msg, skipTelegram, compactContext)
	ragCtx, ragCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer ragCancel()

	histLimit := 50
	if compactContext {
		histLimit = 18
	}
	// Получаем историю тренировок пользователя
	history, err := b.db.GetUserTrainingHistory(msg.From.ID, stateChat, histLimit)
	if err != nil {
		b.logger.Errorf("Failed to get user training history: %v", err)
		t := "❌ Ошибка при получении истории тренировок"
		miniReply(t)
		if !skipTelegram {
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, t))
		}
		return
	}

	var aiSec aiQuestionSections
	aiSec.initRules()

	interlocutorName := strings.TrimSpace(msg.From.UserName)
	if interlocutorName == "" {
		interlocutorName = fmt.Sprintf("user%d", msg.From.ID)
	}

	if len(history) > 0 {
		for _, hm := range history {
			aiSec.facts.WriteString(formatTrainingFactLine(
				hm.CreatedAt.Format("2006-01-02 15:04"),
				hm.Username,
				msg.From.ID,
				hm.MessageType,
				hm.MessageText,
			))
			aiSec.facts.WriteString("\n")
		}
	}

	// Добавляем предыдущее сообщение бота только если пользователь отвечает на него
	lastBotMessageText := ""
	if msg.ReplyToMessage != nil && msg.ReplyToMessage.From != nil && msg.ReplyToMessage.From.IsBot && msg.ReplyToMessage.From.ID == b.api.Self.ID {
		replyText := strings.TrimSpace(msg.ReplyToMessage.Text)
		if replyText == "" && msg.ReplyToMessage.Caption != "" {
			replyText = strings.TrimSpace(msg.ReplyToMessage.Caption)
		}
		if replyText != "" {
			aiSec.thread.WriteString("• [бот] Лео: ")
			aiSec.thread.WriteString(replyText)
			aiSec.thread.WriteString("\n")
			lastBotMessageText = replyText
		}
	}

	userLog, err := b.db.GetMessageLog(msg.From.ID, stateChat)
	if err == nil && userLog != nil {
		if strings.TrimSpace(userLog.Username) != "" {
			interlocutorName = userLog.Username
		}
		cups, _ := b.db.GetUserCups(msg.From.ID, stateChat)
		remaining := ""
		if userLog.TimerStartTime != nil {
			if rt := b.calculateRemainingTime(userLog); rt > 0 {
				remaining = "до удаления: " + b.formatDurationToDays(rt)
				if userLog.HasSickLeave {
					remaining = "после #healthy: " + b.formatDurationToDays(rt)
				}
			} else {
				remaining = "таймер истёк"
			}
		}
		aiSec.users.WriteString(formatUserEntityLine(userLog, cups, remaining))
		aiSec.users.WriteString("\n")
	}

	// Недавний контекст беседы: общий чат — только pack_group; личка — user_messages / личный чат.
	if ctxChannel == rag.ChannelPackGroup {
		b.appendPackGroupSQLContext(stateChat, &aiSec.thread, 12)
	} else {
		recentLimit := 10
		if compactContext {
			recentLimit = 5
		}
		end := time.Now()
		start := end.Add(-2 * time.Hour)
		recentChat, err := b.db.GetMessagesInRange(msg.Chat.ID, start, end)
		if err == nil && len(recentChat) > 0 {
			count := 0
			for i := len(recentChat) - 1; i >= 0 && count < recentLimit; i-- {
				text := strings.TrimSpace(recentChat[i].MessageText)
				if text == "" {
					continue
				}
				if len(text) > 300 {
					text = text[:300] + "…"
				}
				ts := recentChat[i].CreatedAt.In(time.FixedZone("MSK", 3*3600)).Format("2006-01-02 15:04")
				aiSec.thread.WriteString("• [" + ts + "] ")
				aiSec.thread.WriteString(text)
				aiSec.thread.WriteString("\n")
				count++
			}
		}
	}

	// Добавляем анти‑повторы: последние ответы ИИ для этого пользователя
	{
		maxReplies := 5
		maxSnippet := 400
		lookbackDays := 30
		if compactContext {
			maxReplies = 2
			maxSnippet = 220
			lookbackDays = 7
		}
		end := time.Now()
		start := end.AddDate(0, 0, -lookbackDays)
		recent, err := b.db.GetUserMessagesAcrossTrainingScope(msg.From.ID, stateChat, start, end)
		if err == nil {
			var lastReplies []string
			for i := len(recent) - 1; i >= 0 && len(lastReplies) < maxReplies; i-- {
				if strings.ToLower(recent[i].MessageType) == "ai_reply" {
					lastReplies = append(lastReplies, recent[i].MessageText)
				}
			}
			if len(lastReplies) > 0 {
				for _, r := range lastReplies {
					txt := r
					if len(txt) > maxSnippet {
						txt = txt[:maxSnippet] + "…"
					}
					aiSec.thread.WriteString("• [бот] Лео: ")
					aiSec.thread.WriteString(txt)
					aiSec.thread.WriteString("\n")
				}
			}
		}
	}

	// Легкий RAG по чату — тяжёлый по токенам; для HTTP мини-аппа и pack-чата опускаем, чтобы быстрее ответить.
	if !compactContext {
		end := time.Now()
		start := end.AddDate(0, 0, -14)
		examples, err := b.db.GetMessagesInRange(msg.Chat.ID, start, end)
		if err == nil {
			var picked []string
			for i := len(examples) - 1; i >= 0 && len(picked) < 3; i-- {
				if examples[i].MessageType == "training_done" {
					text := examples[i].MessageText
					if len(text) > 200 {
						text = text[:200] + "…"
					}
					picked = append(picked, text)
				}
			}
			if len(picked) > 0 {
				for _, p := range picked {
					aiSec.facts.WriteString("• [пример отчёта] ")
					aiSec.facts.WriteString(p)
					aiSec.facts.WriteString("\n")
				}
			}
		}
	}

	// «Печатает…» только в Telegram, не в режиме «только мини-апп».
	var typingDone chan struct{}
	if !skipTelegram {
		b.api.Send(tgbotapi.NewChatAction(msg.Chat.ID, tgbotapi.ChatTyping))
		typingDone = make(chan struct{})
		go func(chatID int64, done <-chan struct{}) {
			ticker := time.NewTicker(4 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-done:
					return
				case <-ticker.C:
					b.api.Send(tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping))
				}
			}
		}(msg.Chat.ID, typingDone)
	}

	// Пытаемся определить пол из сообщения или имени
	detectedGender := b.detectGenderFromMessage(questionText)
	if detectedGender == "" && msg.From.FirstName != "" {
		detectedGender = b.detectGenderFromName(msg.From.FirstName)
	}

	// Обновляем пол в базе данных, если он определен
	if detectedGender != "" {
		if err := b.updateUserGender(msg.From.ID, stateChat, detectedGender); err != nil {
			b.logger.Warnf("Failed to update user gender: %v", err)
		}
	}

	// Проверяем, есть ли в вопросе упоминание другого пользователя
	// Извлекаем упоминания (@username) и ищем информацию о них в БД
	words := strings.Fields(questionText)
	var mentionedUsernames []string

	for _, word := range words {
		word = strings.Trim(word, ".,!?;:")
		// Ищем упоминания (@username)
		if strings.HasPrefix(word, "@") {
			searchUsername := strings.TrimPrefix(word, "@")
			if len(searchUsername) >= 2 {
				mentionedUsernames = append(mentionedUsernames, searchUsername)
			}
		}
	}

	// Если упоминаний нет, ищем по словам после ключевых фраз (например, "какого пола Tester" или "про Tester")
	questionLower := strings.ToLower(questionText)
	if len(mentionedUsernames) == 0 && (strings.Contains(questionLower, "пол") || strings.Contains(questionLower, "статистик") || strings.Contains(questionLower, "сколько") || strings.Contains(questionLower, "кубк") || strings.Contains(questionLower, "про ") || strings.Contains(questionLower, "расскажи") || strings.Contains(questionLower, "достижен") || strings.Contains(questionLower, "у него") || strings.Contains(questionLower, "у неё") || strings.Contains(questionLower, "его") || strings.Contains(questionLower, "её")) {
		// Ищем потенциальные имена пользователей (слова с заглавной буквы или после ключевых фраз)
		for _, word := range words {
			word = strings.Trim(word, ".,!?;:")
			// Пропускаем слишком короткие слова и служебные
			if len(word) < 2 || word == "какого" || word == "пола" || word == "какой" || word == "про" || word == "о" || word == "расскажи" || word == "у" || word == "него" || word == "неё" || word == "его" || word == "её" || word == "какие" {
				continue
			}
			// Если слово начинается с заглавной буквы, возможно это имя
			if len(word) > 0 && word[0] >= 'A' && word[0] <= 'Z' {
				mentionedUsernames = append(mentionedUsernames, word)
			}
		}
	}

	// Если упоминаний всё ещё нет, но есть местоимения "он", "его", "у него" - ищем в недавнем контексте
	if len(mentionedUsernames) == 0 && (strings.Contains(questionLower, "у него") || strings.Contains(questionLower, "у неё") || strings.Contains(questionLower, "его") || strings.Contains(questionLower, "её")) {
		// Ищем в недавнем контексте беседы (последние 2 часа) упоминания пользователей
		end := time.Now()
		start := end.Add(-2 * time.Hour)
		recentChat, err := b.db.GetMessagesInRange(msg.Chat.ID, start, end)
		if err == nil {
			// Ищем в последних сообщениях упоминания пользователей или имена с заглавной буквы
			for i := len(recentChat) - 1; i >= 0 && i >= len(recentChat)-5; i-- {
				text := recentChat[i].MessageText
				// Ищем @username
				if strings.Contains(text, "@") {
					parts := strings.Fields(text)
					for _, part := range parts {
						if strings.HasPrefix(part, "@") {
							username := strings.TrimPrefix(part, "@")
							username = strings.Trim(username, ".,!?;:")
							if len(username) >= 2 {
								mentionedUsernames = append(mentionedUsernames, username)
								break
							}
						}
					}
				}
				// Ищем слова с заглавной буквы (имена)
				if len(mentionedUsernames) == 0 {
					nameParts := strings.Fields(text)
					for _, namePart := range nameParts {
						namePart = strings.Trim(namePart, ".,!?;:")
						if len(namePart) >= 2 && namePart[0] >= 'A' && namePart[0] <= 'Z' {
							// Проверяем, не является ли это именем пользователя в БД
							mentionedUsernames = append(mentionedUsernames, namePart)
							break
						}
					}
				}
				if len(mentionedUsernames) > 0 {
					break
				}
			}
		}
	}

	// Ищем информацию о найденных пользователях в БД
	for _, searchUsername := range mentionedUsernames {
		userID, err := b.db.GetUserIDByUsername(searchUsername, stateChat)
		if err == nil && userID != msg.From.ID {
			// Нашли другого пользователя, получаем всю информацию о нём
			otherUserLog, err := b.db.GetMessageLog(userID, stateChat)
			if err == nil {
				cups, _ := b.db.GetUserCups(userID, stateChat)
				remaining := ""
				if otherUserLog.TimerStartTime != nil {
					if rt := b.calculateRemainingTime(otherUserLog); rt > 0 {
						remaining = "до удаления: " + b.formatDurationToDays(rt)
					}
				}
				aiSec.users.WriteString(formatUserEntityLine(otherUserLog, cups, remaining))
				aiSec.users.WriteString("\n")
			}
			break // Нашли одного пользователя, достаточно
		}
	}

	// Если спрашивают про список участников ("какие участники", "кто есть", "список участников", "какого пола участники")
	questionLower = strings.ToLower(questionText)
	if strings.Contains(questionLower, "участник") || strings.Contains(questionLower, "кто есть") || strings.Contains(questionLower, "список") {
		users, err := b.db.GetUsersByChatID(stateChat)
		if err == nil && len(users) > 0 {
			for i, user := range users {
				if i >= 15 {
					aiSec.users.WriteString(fmt.Sprintf("… и ещё %d участников\n", len(users)-15))
					break
				}
				cups, _ := b.db.GetUserCups(user.UserID, stateChat)
				remaining := ""
				if user.TimerStartTime != nil {
					if rt := b.calculateRemainingTime(user); rt > 0 {
						remaining = "до удаления: " + b.formatDurationToDays(rt)
					}
				}
				aiSec.users.WriteString(formatUserEntityLine(user, cups, remaining))
				aiSec.users.WriteString("\n")
			}
		}
	}

	// RAG + история диалога: изолированные сессии (личка vs общий чат).
	b.appendRAGContext(ragCtx, ctxChannel, msg.From.ID, stateChat, questionText, &aiSec.facts)
	if ctxChannel == rag.ChannelPersonalLeo && b.config.MonetizedChatID != 0 && b.db != nil {
		if b.ragStore == nil || !b.ragStore.Enabled() {
			personalHist, histErr := b.db.ListMiniappPersonalChat(msg.From.ID, b.config.MonetizedChatID, 10, 0)
			if histErr == nil && len(personalHist) > 0 {
				for _, h := range personalHist {
					role := "пользователь"
					if h.Role == "leo" {
						role = "бот"
					}
					ts := strings.TrimSpace(h.CreatedAt)
					if ts == "" {
						ts = "—"
					}
					aiSec.thread.WriteString(fmt.Sprintf("• [%s] %s: %s\n", ts, role, h.Text))
				}
			}
		}
	}

	// Генерируем ответ с помощью ИИ
	finalQuestion := questionText
	if lastBotMessageText != "" {
		finalQuestion = fmt.Sprintf(
			"МОЁ ПРЕДЫДУЩЕЕ СООБЩЕНИЕ:\n%s\n\nПОЛЬЗОВАТЕЛЬ ОТВЕТИЛ ТАК: %s\n\nСОХРАНИ ЛОГИКУ ПРЕДЫДУЩЕГО СООБЩЕНИЯ. ЕСЛИ ЕГО ОСПАРИВАЮТ ИЛИ ПРОСЛЕЖИВАЕТСЯ ХИТРОСТЬ, ПРОДОЛЖАЙ СТРОГО НАСТАИВАТЬ, ТРЕБУЙ ДОКАЗАТЕЛЬСТВ И НЕ СМЕНЯЙ ТОН НА ПОДДЕРЖИВАЮЩИЙ БЕЗ НОВЫХ ФАКТОВ.",
			lastBotMessageText,
			questionText,
		)
	}

	finalQuestion += b.livePrompts().CombinedChatInstructionSuffix()

	userPrompt := prompts.FormatAIQuestionUserMessage(prompts.AIQuestionUserPayload{
		InterlocutorName: interlocutorName,
		InterlocutorID:   msg.From.ID,
		UsersBlock:       aiSec.users.String(),
		RulesBlock:       aiSec.rules.String(),
		FactsBlock:       aiSec.facts.String(),
		ChatThread:       aiSec.thread.String(),
		RouterHint:       prompts.RouterHintForQuestion(questionText),
		Question:         finalQuestion,
	})

	answer, err := b.aiClient.AnswerUserQuestion("", userPrompt)
	if err != nil {
		b.logger.Errorf("Failed to generate AI answer: %v", err)

		// Проверяем, является ли это ошибкой настройки политики данных
		errorMsg := err.Error()
		if strings.Contains(errorMsg, "data policy") || strings.Contains(errorMsg, "Model Training") {
			help := "❌ ИИ функции требуют настройки OpenRouter API.\n\nДля бесплатных моделей нужно:\n1. Перейди на https://openrouter.ai/settings/privacy\n2. Включи опцию 'Model Training'\n\nПосле этого ИИ заработает!"
			miniReply(help)
			if !skipTelegram {
				b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, help))
			}
			if typingDone != nil {
				close(typingDone)
			}
			return
		}

		var et string
		if strings.Contains(err.Error(), "context deadline exceeded") || strings.Contains(err.Error(), "Client.Timeout") {
			et = "❌ ИИ не успел ответить вовремя. Попробуй ещё раз или сформулируй вопрос короче."
		} else {
			et = fmt.Sprintf("❌ Ошибка при генерации ответа ИИ: %v", err)
		}
		miniReply(et)
		if !skipTelegram {
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, et))
		}
		if typingDone != nil {
			close(typingDone)
		}
		return
	}

	answer = ai.SanitizeTextForUser(answer)
	if answer == "" {
		answer = "Сформулируй, пожалуйста, вопрос короче — отвечу по сути."
	}

	miniReply(answer)
	// Отправляем ответ с реплаем на исходное сообщение
	if !skipTelegram {
		reply := tgbotapi.NewMessage(msg.Chat.ID, answer)
		if msg.MessageID != 0 {
			reply.ReplyToMessageID = msg.MessageID
		}
		b.logger.Infof("Sending AI answer to user %d in chat %d (replying to message %d)", msg.From.ID, msg.Chat.ID, msg.MessageID)
		_, err = b.api.Send(reply)
		if err != nil {
			b.logger.Errorf("Failed to send AI answer: %v", err)
		} else if msg.From != nil && strings.TrimSpace(answer) != "" {
			b.savePersonalChatMessage(msg.From.ID, "leo", answer)
		}
	}
	if typingDone != nil {
		close(typingDone)
	}

	// Сохраняем ответ ИИ для анти‑повторов (тип ai_reply)
	_ = b.db.SaveUserMessage(&domain.UserMessage{
		UserID:      msg.From.ID,
		ChatID:      msg.Chat.ID,
		Username:    b.api.Self.UserName,
		MessageText: answer,
		MessageType: "ai_reply",
		CreatedAt:   time.Now(),
	})
}
