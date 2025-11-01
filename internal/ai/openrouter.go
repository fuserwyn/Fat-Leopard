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
)

type OpenRouterClient struct {
	apiKey     string
	baseURL    string
	logger     logger.Logger
	httpClient *http.Client
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

func NewOpenRouterClient(apiKey string, log logger.Logger) *OpenRouterClient {
	return &OpenRouterClient{
		apiKey:  apiKey,
		baseURL: "https://openrouter.ai/api/v1/chat/completions",
		logger:  log,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// Chat отправляет запрос к OpenRouter API и возвращает ответ
func (c *OpenRouterClient) Chat(messages []ChatMessage, model string) (string, error) {
	if model == "" {
		model = "meta-llama/llama-3.1-8b-instruct:free" // Бесплатная модель по умолчанию
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
		return "", fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(bodyBytes))
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
	systemPrompt := `Ты - мотивирующий тренер-леопард по имени Fat Leopard, который следит за тренировками команды.
Твоя задача - составить ежедневную сводку о тренировках за прошедшие сутки.
Ты должен:
1. Поздравить всех, кто отправил отчеты о тренировках
2. Отметить успехи и прогресс каждого
3. Подбадривать тех, кто активно тренируется
4. Быть позитивным, мотивирующим и дружелюбным
5. Использовать эмодзи и теги для упоминания пользователей
6. Написать на русском языке`

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

// AnswerUserQuestion отвечает на вопрос пользователя на основе его истории тренировок
func (c *OpenRouterClient) AnswerUserQuestion(question string, userHistory string) (string, error) {
	systemPrompt := `Ты - опытный тренер-леопард по имени Fat Leopard, который помогает пользователям с тренировками.
Ты имеешь доступ к истории тренировок пользователя.
Твоя задача:
1. Анализировать историю тренировок пользователя
2. Давать полезные советы и рекомендации
3. Отвечать на вопросы о прогрессе, тренировках, восстановлении
4. Быть дружелюбным, мотивирующим и профессиональным
5. Отвечать на русском языке
6. Если пользователь спрашивает о конкретном дне - анализируй данные из истории`

	prompt := fmt.Sprintf("Вопрос пользователя: %s\n\nИстория тренировок пользователя:\n%s", question, userHistory)

	messages := []ChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: prompt},
	}

	return c.Chat(messages, "")
}
