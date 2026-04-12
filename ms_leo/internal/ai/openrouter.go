package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"leo-bot/internal/logger"
	"leo-bot/internal/prompts"
)

type OpenRouterClient struct {
	apiKey        string
	baseURL       string
	model         string // Модель по умолчанию
	logger        logger.Logger
	httpClient    *http.Client
	promptsBundle prompts.Bundle
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream,omitempty"`
}

type ChatResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Message      ChatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type UserTrainingData struct {
	UserID          int64
	Username        string
	HasTraining     bool
	HasSickLeave    bool
	HasHealthy      bool
	StreakDays      int
	Calories        int
	Cups            int
	TrainingMessage string
}

func NewOpenRouterClient(apiKey string, defaultModel string, p prompts.Bundle, log logger.Logger) *OpenRouterClient {
	if defaultModel == "" {
		defaultModel = "deepseek/deepseek-r1-0528" // Fallback
	}
	return &OpenRouterClient{
		apiKey:        apiKey,
		baseURL:       "https://openrouter.ai/api/v1/chat/completions",
		model:         defaultModel,
		logger:        log,
		promptsBundle: p,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// Chat отправляет запрос к OpenRouter API и возвращает ответ
func (c *OpenRouterClient) Chat(messages []ChatMessage, model string) (string, error) {
	if model == "" {
		model = c.model // Используем модель из конфига
	}

	request := ChatRequest{
		Model:    model,
		Messages: messages,
		Stream:   false,
	}

	jsonData, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", c.baseURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))
	req.Header.Set("HTTP-Referer", "https://github.com/LeoPoacherBot")
	req.Header.Set("X-Title", "LeoPoacherBot")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		errorMsg := string(bodyBytes)

		// Специальная обработка ошибки политики данных для бесплатных моделей
		if resp.StatusCode == 404 && strings.Contains(errorMsg, "data policy") {
			return "", fmt.Errorf("OpenRouter API требует настройки политики данных. Перейди на https://openrouter.ai/settings/privacy и включи 'Model Training' для бесплатных моделей. Ошибка: %s", errorMsg)
		}

		return "", fmt.Errorf("API error (status %d): %s", resp.StatusCode, errorMsg)
	}

	var response ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if len(response.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}

	return response.Choices[0].Message.Content, nil
}

// GenerateDailySummary генерирует ежедневную сводку о тренировках
func (c *OpenRouterClient) GenerateDailySummary(usersData []UserTrainingData) (string, error) {
	systemPrompt := c.promptsBundle.DailySummary

	var userReports strings.Builder
	userReports.WriteString("Отчеты о тренировках за прошедшие сутки:\n\n")

	for _, user := range usersData {
		userReports.WriteString(fmt.Sprintf("Пользователь: %s (ID: %d)\n", user.Username, user.UserID))
		if user.HasTraining {
			userReports.WriteString(fmt.Sprintf("  - Отправил отчет о тренировке: %s\n", user.TrainingMessage))
		}
		if user.HasSickLeave {
			userReports.WriteString("  - Был на больничном\n")
		}
		if user.HasHealthy {
			userReports.WriteString("  - Выздоровел\n")
		}
		userReports.WriteString(fmt.Sprintf("  - Серия тренировок: %d дней\n", user.StreakDays))
		userReports.WriteString(fmt.Sprintf("  - Всего калорий: %d\n", user.Calories))
		userReports.WriteString(fmt.Sprintf("  - Всего кубков: %d\n\n", user.Cups))
	}

	messages := []ChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userReports.String()},
	}

	return c.Chat(messages, "")
}

// GenerateMonthlySummary генерирует ежемесячную сводку о тренировках
func (c *OpenRouterClient) GenerateMonthlySummary(usersData []UserTrainingData) (string, error) {
	systemPrompt := c.promptsBundle.MonthlySummary

	var userReports strings.Builder
	userReports.WriteString("Отчеты о тренировках за прошедший месяц:\n\n")

	for _, user := range usersData {
		userReports.WriteString(fmt.Sprintf("Пользователь: %s (ID: %d)\n", user.Username, user.UserID))
		if user.HasTraining {
			userReports.WriteString(fmt.Sprintf("  - Отправлял отчеты о тренировке: %s\n", user.TrainingMessage))
		}
		if user.HasSickLeave {
			userReports.WriteString("  - Был на больничном\n")
		}
		if user.HasHealthy {
			userReports.WriteString("  - Выздоровел\n")
		}
		userReports.WriteString(fmt.Sprintf("  - Текущая серия тренировок: %d дней\n", user.StreakDays))
		userReports.WriteString(fmt.Sprintf("  - Всего калорий: %d\n", user.Calories))
		userReports.WriteString(fmt.Sprintf("  - Всего кубков: %d\n\n", user.Cups))
	}

	messages := []ChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userReports.String()},
	}

	return c.Chat(messages, "")
}

// AnswerUserQuestion отвечает на вопрос пользователя на основе его истории тренировок
func (c *OpenRouterClient) AnswerUserQuestion(question string, userContext string) (string, error) {
	systemPrompt := c.promptsBundle.AnswerUserQuestion

	prompt := fmt.Sprintf("Вопрос пользователя: %s\n\n=== ПОЛНЫЙ КОНТЕКСТ ПОЛЬЗОВАТЕЛЯ ===\n%s", question, userContext)

	messages := []ChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: prompt},
	}

	return c.Chat(messages, "")
}

// GenerateDailyWisdom генерирует короткую «мудрость дня» о тренировках и дисциплине
func (c *OpenRouterClient) GenerateDailyWisdom() (string, error) {
	systemPrompt := c.promptsBundle.DailyWisdomTraining

	systemPrompt += "\n\n" + c.promptsBundle.DailyWisdomLangRule

	// Добавляем ежедневное семя (дата + тема дня), чтобы повысить вариативность
	today := time.Now().Format("2006-01-02")
	weekday := time.Now().Weekday()
	theme := "дисциплина"
	switch weekday {
	case time.Monday:
		theme = "старт и дисциплина"
	case time.Tuesday:
		theme = "выносливость"
	case time.Wednesday:
		theme = "равновесие и техника"
	case time.Thursday:
		theme = "терпение и прогресс"
	case time.Friday:
		theme = "радость движения без спешки"
	case time.Saturday:
		theme = "осознанность и восстановление"
	case time.Sunday:
		theme = "тихая сила духа"
	}

	userPrompt := fmt.Sprintf(c.promptsBundle.DailyWisdomUserTemplate, today, theme)

	messages := []ChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}

	return c.Chat(messages, "")
}
