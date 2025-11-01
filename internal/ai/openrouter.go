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
		model = "deepseek/deepseek-chat-v3.1:free" // Бесплатная модель DeepSeek по умолчанию
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

ВАЖНО: Начинай сводку обязательно с фразы "Привет, стая! 🦁" или похожей приветственной фразы.

Ты должен:
1. Начать с приветствия "Привет, стая! 🦁" или похожей дружелюбной фразы
2. Поздравить всех, кто отправил отчеты о тренировках, отметив их тегами (@username)
3. Отметить успехи и прогресс каждого пользователя
4. Подбадривать тех, кто активно тренируется
5. Упомянуть тех, кто был на больничном - пожелать выздоровления
6. Быть позитивным, мотивирующим и дружелюбным
7. Использовать эмодзи и теги (@username) для упоминания пользователей
8. Написать на русском языке
9. Сохранять стиль леопарда-тренера - быть немного строгим, но справедливым и мотивирующим
10. В конце добавить мотивирующую фразу для продолжения тренировок

Пример начала: "Привет, стая! 🦁 Вот как прошли тренировки за прошедшие сутки..." или "🦁 Привет, стая! Сегодняшняя сводка о ваших тренировках..."`

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
func (c *OpenRouterClient) AnswerUserQuestion(question string, userContext string) (string, error) {
	systemPrompt := `Ты - опытный тренер-леопард по имени Fat Leopard, который помогает пользователям с тренировками.
Ты имеешь полный доступ к истории тренировок, статистике и данным пользователя.

Твоя задача:
1. Анализировать всю историю тренировок пользователя (все сообщения с тегами и без)
2. Учитывать текущую статистику: кубки, калории, серии тренировок
3. Давать персональные советы на основе истории пользователя
4. Отвечать на вопросы о прогрессе, тренировках, восстановлении, лечении
5. Общаться дружелюбно, мотивировать, но быть профессиональным
6. Отвечать на русском языке
7. Если пользователь спрашивает о конкретном дне - ищи в истории сообщения за этот день
8. Если спрашивает о прогрессе - сравнивай текущую статистику с историей
9. Если спрашивает о лечении/восстановлении - учитывай информацию о больничных
10. Всегда быть позитивным и подбадривающим, но честным

Ты знаешь ВСЮ историю пользователя, все его сообщения, все тренировки, все события. Используй эту информацию для максимально персонального ответа.`

	prompt := fmt.Sprintf("Вопрос пользователя: %s\n\n=== ПОЛНЫЙ КОНТЕКСТ ПОЛЬЗОВАТЕЛЯ ===\n%s", question, userContext)

	messages := []ChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: prompt},
	}

	return c.Chat(messages, "")
}
