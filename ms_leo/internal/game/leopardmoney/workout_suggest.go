package leopardmoney

import (
	"math"
	"strings"
	"time"
)

// WorkoutSessionHint — одна прошлая тренировка для ранжирования типов в форме отчёта.
type WorkoutSessionHint struct {
	MessageText string
	CreatedAt   time.Time
}

// DefaultWorkoutTypeOrder — порядок id как в miniapp workoutCategories (fallback для новичков).
var DefaultWorkoutTypeOrder = []string{
	"run", "walk", "bike", "swim", "yoga", "rowing", "workout", "crossfit", "stretch", "dance",
	"hiit", "cardio", "kettlebell", "strength", "jump_rope", "pole", "rollerblade",
	"basketball", "football", "volleyball", "tennis", "padel", "gymnastics", "other",
}

var defaultWorkoutTypeIndex = func() map[string]int {
	m := make(map[string]int, len(DefaultWorkoutTypeOrder))
	for i, id := range DefaultWorkoutTypeOrder {
		m[id] = i
	}
	return m
}()

// SuggestWorkoutTypeOrder — персональный порядок типов: история (частота + свежесть),
// совпадение дня недели и времени с прошлыми отчётами, лёгкий контекст «цели» —
// восстановление после перерыва и типичный ритм дня.
func SuggestWorkoutTypeOrder(sessions []WorkoutSessionHint, now time.Time, tzOffsetHours int, daysSinceLastTraining int) []string {
	scores := make(map[string]float64, len(DefaultWorkoutTypeOrder))
	localNow := now.In(userLocalLocFromMoscowOffset(tzOffsetHours))
	curWeekday := localNow.Weekday()
	curHour := localNow.Hour()

	for i, s := range sessions {
		text := strings.TrimSpace(s.MessageText)
		if text == "" {
			continue
		}
		_, _, cats, ok := parseTrainingDoneReport(text)
		if !ok || len(cats) == 0 {
			continue
		}
		recency := math.Exp(-float64(i) * 0.08)
		sLocal := s.CreatedAt.In(userLocalLocFromMoscowOffset(tzOffsetHours))
		sameWeekday := sLocal.Weekday() == curWeekday
		sameTimeSlot := hourDistance(sLocal.Hour(), curHour) <= 2

		for _, cat := range cats {
			id := strings.ToLower(strings.TrimSpace(cat))
			if id == "" {
				id = "other"
			}
			scores[id] += recency
			if sameWeekday {
				scores[id] += recency * 0.35
			}
			if sameTimeSlot {
				scores[id] += recency * 0.45
			}
		}
	}

	applyContextBoosts(scores, curHour, daysSinceLastTraining)

	out := append([]string(nil), DefaultWorkoutTypeOrder...)
	if len(scores) == 0 {
		return out
	}

	// «Цель» — удержать привычный ритм: чуть поднимаем топ-3 из истории.
	type scored struct {
		id    string
		score float64
	}
	top := make([]scored, 0, len(scores))
	for id, sc := range scores {
		top = append(top, scored{id: id, score: sc})
	}
	for i := 0; i < len(top)-1; i++ {
		for j := i + 1; j < len(top); j++ {
			if top[j].score > top[i].score {
				top[i], top[j] = top[j], top[i]
			}
		}
	}
	for i := 0; i < len(top) && i < 3; i++ {
		scores[top[i].id] += 0.25 * float64(3-i)
	}

	for i := 0; i < len(out)-1; i++ {
		for j := i + 1; j < len(out); j++ {
			si := scores[out[i]]
			sj := scores[out[j]]
			if sj > si || (sj == si && defaultWorkoutTypeIndex[out[j]] < defaultWorkoutTypeIndex[out[i]]) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func applyContextBoosts(scores map[string]float64, localHour int, daysSinceLastTraining int) {
	// После перерыва — восстановительные виды в начале списка.
	if daysSinceLastTraining >= 5 {
		for _, id := range []string{"stretch", "yoga", "walk"} {
			scores[id] += 2.0
		}
	} else if daysSinceLastTraining >= 3 {
		for _, id := range []string{"stretch", "yoga", "walk"} {
			scores[id] += 1.0
		}
	} else if daysSinceLastTraining == 2 {
		for _, id := range []string{"walk", "yoga", "stretch"} {
			scores[id] += 0.25
		}
	}

	switch {
	case localHour >= 5 && localHour < 11:
		for _, id := range []string{"run", "walk", "bike", "swim"} {
			scores[id] += 0.2
		}
	case localHour >= 17 && localHour < 23:
		for _, id := range []string{"strength", "workout", "yoga", "crossfit"} {
			scores[id] += 0.2
		}
	}
}

func hourDistance(a, b int) int {
	d := a - b
	if d < 0 {
		d = -d
	}
	if d > 12 {
		d = 24 - d
	}
	return d
}

// userLocalLocFromMoscowOffset — как miniapp TZ: часы относительно МСК (UTC+3).
func userLocalLocFromMoscowOffset(offsetFromMoscow int) *time.Location {
	sec := (3 + offsetFromMoscow) * 3600
	return time.FixedZone("user_local", sec)
}
