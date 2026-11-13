package bot

import (
	"strings"
	"time"

	"leo-bot/internal/game/leopardmoney"
	"leo-bot/internal/utils"
)

// Определение пола по имени и тексту, сборка промптов для тренировок и мудрости.

// detectGenderFromName пытается определить пол по русскому имени
func (b *Bot) detectGenderFromName(firstName string) string {
	if firstName == "" {
		return ""
	}
	firstName = strings.ToLower(strings.TrimSpace(firstName))

	// Женские окончания русских имен
	femaleEndings := []string{"а", "я", "ь", "ия", "ина", "ая"}
	for _, ending := range femaleEndings {
		if strings.HasSuffix(firstName, ending) {
			return "f"
		}
	}

	// Мужские имена без окончаний (обычно оканчиваются на согласную, кроме ь)
	// Также имена с окончаниями: ов, ев, ин, ой, ий
	maleEndings := []string{"ов", "ев", "ин", "ой", "ий"}
	for _, ending := range maleEndings {
		if strings.HasSuffix(firstName, ending) {
			return "m"
		}
	}

	// Если имя не оканчивается на характерные окончания, возвращаем пустую строку
	return ""
}

// detectGenderFromMessage пытается определить пол из сообщения пользователя
func (b *Bot) detectGenderFromMessage(text string) string {
	text = strings.ToLower(text)

	// Паттерны, указывающие на женский пол
	femalePatterns := []string{"я девочка", "я девушка", "я женщина", "я девочка", "полина", "ирина", "анна", "мария", "елена", "ольга", "татьяна", "наталья", "светлана"}
	for _, pattern := range femalePatterns {
		if strings.Contains(text, pattern) {
			return "f"
		}
	}

	// Паттерны, указывающие на мужской пол
	malePatterns := []string{"я мальчик", "я парень", "я мужчина", "я парень", "александр", "дмитрий", "иван", "михаил", "сергей", "алексей", "андрей", "максим"}
	for _, pattern := range malePatterns {
		if strings.Contains(text, pattern) {
			return "m"
		}
	}

	// Проверка упоминания рода в обратной связи
	if strings.Contains(text, "род") || strings.Contains(text, "пол") {
		if strings.Contains(text, "женск") || strings.Contains(text, "девочк") || strings.Contains(text, "девушк") {
			return "f"
		}
		if strings.Contains(text, "мужск") || strings.Contains(text, "мальчик") || strings.Contains(text, "парень") {
			return "m"
		}
	}

	return ""
}

// getUnifiedTrainingPrompt генерирует единый промпт для AI после #training_done
func (b *Bot) getUnifiedTrainingPrompt(streakDays, _ int, achievementCount int, wasOnSickLeave bool) string {
	now := utils.GetMoscowTime()
	hour := now.Hour()
	weekday := now.Weekday()
	formatHint := " If the report has a line like \"type, N min, int. X/5\", then `int.` means workout intensity on a 1..5 scale, not intervals."

	prompts := []string{
		"You are Fat Leopard — a strict coach who loves to eat. Write 2-3 sentences after the training report: first — a specific comment on exercises from the message, second — a short note or tip. A light hint that you \"won't eat\" someone who trains is fine. Use ONLY exercises from the message. Don't repeat numbers. No Markdown.",

		"You are Fat Leopard — lazy but fair. Reply to the training report (2-3 sentences): first — note the exercises, second — a short tip. Tone: slightly jealous of the discipline but you acknowledge the result. Use ONLY this report. No Markdown.",

		"Reply as a coach after a workout (2-3 sentences): first — sincere comment on exercises from the message (no templates), second — one practical tip. Write in a lively voice. Use ONLY this report. Don't repeat numbers. No Markdown.",

		"You are Fat Leopard. Write 2-3 sentences after the report: first — assess the workout mentioning exercises, second — a concrete note. Light humor like \"I'd be asleep already while you're still at it\" is fine. Use ONLY exercises from the message. No Markdown.",

		"Reply as a mentor after a workout (2-3 sentences): first — what you liked in the report, second — one concrete tip. Avoid generic 'spirit', 'willpower', 'remember' phrases. Use ONLY this report. No Markdown.",

		"Write 2-3 sentences after the training report: first — note specific exercises, second — a short observation on technique or approach. Tone: supportive with light strictness. Use ONLY this report. Don't repeat numbers. No Markdown.",

		"Fat Leopard approves. Write 2-3 sentences: first — comment on exercises from the message, second — a short note. Tone: friendly-strict. Use ONLY this report. No Markdown.",

		"Reply to the training report (2-3 sentences): first — concrete take on exercises, second — practical tip. Be specific, no abstract phrases. Use ONLY this report. No Markdown.",
	}

	if wasOnSickLeave {
		prompts = append(prompts, "Write one cohesive message (2-3 sentences) after the workout: the user recently returned after sick leave. First — praise the return and note exercises, second — a short practical tip. Do NOT use 'remember', 'spirit', 'willpower'. Use ONLY exercises from the message. No Markdown.")
	}

	if streakDays >= 7 && streakDays < 14 {
		prompts = append(prompts, "Write one cohesive message (2-3 sentences): the user has trained a full week in a row — a key milestone! First — note that and the exercises, second — a short observation. Avoid 'remember', 'spirit', 'willpower'. Use exercises from the message. No Markdown.")
	}

	if streakDays >= 21 {
		prompts = append(prompts, "Write one cohesive message (2-3 sentences): the user shows great discipline with a long streak. First — acknowledge that and note exercises, second — a concrete workout observation. No abstract 'spirit', 'willpower', 'remember'. Use exercises from the message. No Markdown.")
	}

	if hour >= 17 && hour < 22 {
		prompts = append(prompts,
			"End-of-day workout — they closed the day right. Write 2-3 sentences: first — note exercises from the message, second — a short note (e.g. recovery or sleep). Supportive tone, no pomposity. Use ONLY this report. No Markdown.",
			"Evening workout — not everyone can do that. Write 2-3 sentences: first — comment on exercises, second — a light hint that tomorrow may feel easier. Fat Leopard approves. Use ONLY this report. No Markdown.",
			"Late in the day and still training. Write 2-3 sentences: first — concrete comment on the report, second — short tip (recovery, sleep, tomorrow). Use ONLY this report. No Markdown.")
	}

	if hour >= 22 || hour < 6 {
		prompts = append(prompts,
			"End-of-day workout — they didn't quit. Write 2-3 sentences: first — note exercises, second — short comment. Light humor: Fat Leopard's already asleep while they're still at it. Use ONLY this report. No Markdown.",
			"Late workout — special grit. Write 2-3 sentences: first — comment on exercises, second — sleep and recovery tip. Use ONLY this report. No Markdown.")
	}

	if weekday == time.Saturday || weekday == time.Sunday {
		prompts = append(prompts, "Write one cohesive message (2-3 sentences): a weekend workout shows real commitment! First — praise and note exercises, second — practical observation. Do NOT use 'remember', 'spirit', 'willpower'. Use exercises from the message. No Markdown.")
	}

	if achievementCount >= leopardmoney.MaxAchievements {
		prompts = append(prompts, "Write one cohesive message (2-3 sentences): the user has max streak achievements — a veteran. First — address them as a veteran, note exercises, second — concrete observation. No abstract 'spirit', 'willpower', 'remember'. Use exercises from the message. No Markdown.")
	}

	return prompts[now.Unix()%int64(len(prompts))] + formatHint
}

// getVariedTrainingPrompt генерирует разнообразные промпты для AI в зависимости от контекста (оставлено для совместимости)
func (b *Bot) getVariedTrainingPrompt(streakDays, _, totalCups int, wasOnSickLeave bool) string {
	now := utils.GetMoscowTime()
	hour := now.Hour()
	weekday := now.Weekday()

	// Базовые стили промптов
	prompts := []string{
		"Write a very short (1–2 sentences) friendly but strict add-on after the training report. Don't repeat numbers from the message, don't list rules. CRITICAL: use ONLY exercises and details from the user's message. Do NOT invent missing details. No Markdown.",

		"Write a short (1–2 sentences) motivating add-on as strict but fair Fat Leopard after the training report. Be specific about exercises from the message but don't repeat numbers. No Markdown.",

		"Write a short (1–2 sentences) post-workout comment: supportive with light strictness. Mention specific exercises from the user's message, not numbers. No Markdown.",

		"Write a short (1–2 sentences) add-on after the report in wise-mentor style: friendly but demanding. Use ONLY exercises from the user's message. No Markdown.",

		"Write a very short (1–2 sentences) add-on after the workout: energetic and motivating with a note of strictness. Be specific about exercises, no numbers. No Markdown.",
	}

	// Специальные промпты в зависимости от контекста
	if wasOnSickLeave {
		prompts = append(prompts, "Write a short (1–2 sentences) add-on after the workout: the user recently returned after sick leave — praise the return but remind them about consistency. Use ONLY exercises from the message. No Markdown.")
	}

	if streakDays >= 7 && streakDays < 14 {
		prompts = append(prompts, "Write a short (1–2 sentences) add-on: the user has trained a full week in a row — a key milestone! Praise but urge them not to stop. Use exercises from the message. No Markdown.")
	}

	if streakDays >= 21 {
		prompts = append(prompts, "Write a short (1–2 sentences) add-on: the user shows great discipline with a long streak. Acknowledge it but stay strict. Use exercises from the message. No Markdown.")
	}

	if hour >= 22 || hour < 6 {
		prompts = append(prompts, "Write a short (1–2 sentences) add-on: a late-evening or night workout shows special grit! Note that, but use ONLY exercises from the message. No Markdown.")
	}

	if weekday == time.Saturday || weekday == time.Sunday {
		prompts = append(prompts, "Write a short (1–2 sentences) add-on: a weekend workout shows real dedication! Praise but stay demanding. Use exercises from the message. No Markdown.")
	}

	if totalCups >= 1000 {
		prompts = append(prompts, "Write a short (1–2 sentences) add-on: the user has many cups — a veteran. Address them as a veteran but don't lower the bar. Use exercises from the message. No Markdown.")
	}

	// Выбираем случайный промпт
	selectedPrompt := prompts[now.Unix()%int64(len(prompts))]
	return selectedPrompt
}

// getVariedWisdomPrompt генерирует разнообразные промпты для мудрости
func (b *Bot) getVariedWisdomPrompt(streakDays, _, totalCups int) string {
	now := utils.GetMoscowTime()

	// Базовые стили мудрости
	prompts := []string{
		"Give one very short wise thought (1 sentence) for a member after a successful workout: calm, respectful, mentor-like; no pomposity and no repeating numbers. CRITICAL: use ONLY exercises and details from the user's message. Do NOT invent missing details. No Markdown.",

		"Write one short philosophical thought (1 sentence) about the workout: deep but simple. Use ONLY exercises from the user's message. No Markdown.",

		"Give one short motivating thought (1 sentence) in wise-coach style after the workout. Use exercises from the message, no numbers. No Markdown.",

		"Write one short inspiring thought (1 sentence) on discipline in training. Be specific about exercises from the message. No Markdown.",

		"Give one short wise thought (1 sentence) on how each workout moves toward the goal. Use ONLY exercises from the user's message. No Markdown.",
	}

	// Специальные промпты
	if streakDays >= 30 {
		prompts = append(prompts, "Write one short thought (1 sentence) on how a long training streak changes a person. Use exercises from the message. No Markdown.")
	}

	if totalCups >= 500 {
		prompts = append(prompts, "Give one short wise thought (1 sentence) on accumulated experience and discipline. Use exercises from the message. No Markdown.")
	}

	// Выбираем случайный промпт
	selectedPrompt := prompts[now.Unix()%int64(len(prompts))]
	return selectedPrompt
}

// updateUserGender обновляет пол пользователя в базе данных
func (b *Bot) updateUserGender(userID, chatID int64, gender string) error {
	if gender == "" {
		return nil
	}

	userLog, err := b.db.GetMessageLog(userID, chatID)
	if err != nil {
		return err
	}

	// Обновляем только если пол еще не установлен
	if userLog.Gender == "" {
		userLog.Gender = gender
		return b.db.SaveMessageLog(userLog)
	}

	return nil
}
