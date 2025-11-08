package models

import (
	"time"
)

// MessageLog представляет запись о сообщении пользователя
type MessageLog struct {
	UserID                int64      `json:"user_id" db:"user_id"`
	ChatID                int64      `json:"chat_id" db:"chat_id"`
	Username              string     `json:"username" db:"username"`
	Calories              int        `json:"calories" db:"calories"`
	StreakDays            int        `json:"streak_days" db:"streak_days"`
	CalorieStreakDays     int        `json:"calorie_streak_days" db:"calorie_streak_days"`
	CupsEarned            int        `json:"cups_earned" db:"cups_earned"`
	LastTrainingDate      *string    `json:"last_training_date" db:"last_training_date"`
	LastMessage           string     `json:"last_message" db:"last_message"`
	HasTrainingDone       bool       `json:"has_training_done" db:"has_training_done"`
	HasSickLeave          bool       `json:"has_sick_leave" db:"has_sick_leave"`
	HasHealthy            bool       `json:"has_healthy" db:"has_healthy"`
	IsDeleted             bool       `json:"is_deleted" db:"is_deleted"`
	IsExemptFromDeletion  bool       `json:"is_exempt_from_deletion" db:"is_exempt_from_deletion"`
	TimerStartTime        *string    `json:"timer_start_time" db:"timer_start_time"`
	SickLeaveStartTime    *string    `json:"sick_leave_start_time" db:"sick_leave_start_time"`
	SickLeaveEndTime      *string    `json:"sick_leave_end_time" db:"sick_leave_end_time"`
	SickTime              *string    `json:"sick_time" db:"sick_time"`
	RestTimeTillDel       *string    `json:"rest_time_till_del" db:"rest_time_till_del"`
	Gender                string     `json:"gender" db:"gender"` // "m" (male), "f" (female), or empty
	SickApprovalPending   bool       `json:"sick_approval_pending" db:"sick_approval_pending"`
	SickApprovalDeadline  *time.Time `json:"sick_approval_deadline" db:"sick_approval_deadline"`
	SickApprovalMessageID *int64     `json:"sick_approval_message_id" db:"sick_approval_message_id"`
	CreatedAt             time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at" db:"updated_at"`
}

// TrainingLog представляет отчет о тренировке
type TrainingLog struct {
	UserID     int64     `json:"user_id" db:"user_id"`
	Username   string    `json:"username" db:"username"`
	LastReport string    `json:"last_report" db:"last_report"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
	UpdatedAt  time.Time `json:"updated_at" db:"updated_at"`
}

// ChatMember представляет участника чата
type ChatMember struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	Status   string `json:"status"`
}

// TimerInfo представляет информацию о таймере
type TimerInfo struct {
	UserID         int64
	ChatID         int64
	Username       string
	WarningTask    chan bool
	RemovalTask    chan bool
	TimerStartTime string
}

// UserMessage представляет сообщение пользователя для RAG контекста
type UserMessage struct {
	ID          int64     `json:"id" db:"id"`
	UserID      int64     `json:"user_id" db:"user_id"`
	ChatID      int64     `json:"chat_id" db:"chat_id"`
	Username    string    `json:"username" db:"username"`
	MessageText string    `json:"message_text" db:"message_text"`
	MessageType string    `json:"message_type" db:"message_type"` // general, training_done, sick_leave, healthy
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
}
