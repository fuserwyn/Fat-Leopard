package leopardmoney

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Пороги накопленных кубков (нижняя граница уровня): L1 с 0, L2 с 420, … L6 Слон с 13 020 (§2.5 спеки).
var LevelStartCups = []int{0, 420, 1260, 2940, 6300, 13020}

// MaxLevel — максимальный уровень (6 — Слон).
const MaxLevel = 6

// StreakAchievementMilestones — пороги стрика (дней подряд) для получения ачивок в мини-аппе.
var StreakAchievementMilestones = []int{7, 14, 30, 42, 60, 90, 180, 365, 420}

// StreakAchievementIndex — 0-based индекс ачивки для данного стрика; -1 если не совпадает ни с одним порогом.
func StreakAchievementIndex(streak int) int {
	for i, m := range StreakAchievementMilestones {
		if streak == m {
			return i
		}
	}
	return -1
}

// AchievementsCountForStreak — сколько ачивок должно быть открыто при данном стрике (все пороги ≤ streak).
func AchievementsCountForStreak(streak int) int {
	if streak < 0 {
		streak = 0
	}
	n := 0
	for _, m := range StreakAchievementMilestones {
		if streak >= m {
			n++
		}
	}
	if n > MaxAchievements {
		n = MaxAchievements
	}
	return n
}

// LastAchievementMilestoneForStreak — последний порог стрика, достигнутый при данном стрике (0 если ни один).
func LastAchievementMilestoneForStreak(streak int) int {
	last := 0
	for _, m := range StreakAchievementMilestones {
		if streak >= m && m > last {
			last = m
		}
	}
	return last
}

// LevelName — имя уровня по его номеру (1-based). Возвращает пустую строку для неизвестного уровня.
func LevelName(level int) string {
	if level < 1 || level >= len(LevelNames) {
		return ""
	}
	return LevelNames[level]
}

// LevelFromTotalCups — уровень 1…6 по накопленным кубкам.
func LevelFromTotalCups(total int) int {
	if total < 0 {
		total = 0
	}
	lvl := 1
	for i := 1; i < len(LevelStartCups); i++ {
		if total >= LevelStartCups[i] {
			lvl = i + 1
		} else {
			break
		}
	}
	if lvl > MaxLevel {
		lvl = MaxLevel
	}
	return lvl
}

// ActivityCoeff — тип-коэффициент по id активности (как в miniapp workoutCategories).
func ActivityCoeff(categoryID string) float64 {
	switch strings.ToLower(strings.TrimSpace(categoryID)) {
	case "yoga", "stretch":
		return 0.8
	case "walk":
		return 0.8
	case "rowing", "workout", "strength", "kettlebell", "dance", "pole", "gymnastics", "other":
		return 1.0
	case "swim", "bike", "run", "cardio", "jump_rope", "rollerblade", "basketball", "football", "volleyball", "tennis", "padel":
		return 1.2
	case "crossfit", "hiit":
		return 1.5
	default:
		return 1.0
	}
}

var (
	// Мини-апп: «бег, 15 мин, инт. 3/5». Старые записи в ленте могли начинаться с #training_done — префикс опционален при разборе.
	reTrainingHeader = regexp.MustCompile(`(?i)^(?:#training_done\s*[—–\-]\s*)?([^,]+),\s*(\d+)\s*мин`)
	reIntensity      = regexp.MustCompile(`(?i)инт\.?\s*(\d+)`)
	// Несколько видов в одном отчёте: «бег + плавание». Разделитель «+» (или «/») между видами.
	reKindSplit = regexp.MustCompile(`\s*[+/]\s*`)
)

// labelToCategoryID — русские подписи из UI (нижний регистр) → id.
var labelToCategoryID = map[string]string{
	"бег":       "run",
	"ходьба":    "walk",
	"велосипед": "bike",
	"плавание":  "swim",
	"йога":      "yoga",
	"гребля":    "rowing",
	"воркаут":   "workout",
	"кроссфит":  "crossfit",
	"растяжка":  "stretch",
	"танцы":     "dance",
	"hiit":      "hiit",
	"кардио":    "cardio",
	"гиря":      "kettlebell",
	"силовая":   "strength",
	"скакалка":  "jump_rope",
	"пилон":     "pole",
	"ролики":    "rollerblade",
	"баскетбол": "basketball",
	"футбол":    "football",
	"волейбол":  "volleyball",
	"теннис":    "tennis",
	"падел":       "padel",
	"гимнастика":  "gymnastics",
	"другое":      "other",
	"отжимания": "other",
	"отжимание": "other",
}

// IsTrainingReportLine — первая строка похожа на отчёт о тренировке из мини-аппа.
func IsTrainingReportLine(text string) bool {
	_, _, _, ok := ParseTrainingDoneReport(text)
	return ok
}

// parseCategoryIDs — один или несколько видов из «сырой» подписи заголовка.
// «бег + плавание» → ["run","swim"]; неизвестная подпись или пусто → ["other"]. Дубли схлопываются.
func parseCategoryIDs(rawLabel string) []string {
	parts := reKindSplit.Split(strings.TrimSpace(strings.ToLower(rawLabel)), -1)
	ids := make([]string, 0, len(parts))
	seen := make(map[string]bool, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		cat := labelToCategoryID[p]
		if cat == "" {
			cat = "other"
		}
		if !seen[cat] {
			seen[cat] = true
			ids = append(ids, cat)
		}
	}
	if len(ids) == 0 {
		return []string{"other"}
	}
	return ids
}

// bestCategoryID — вид с максимальным коэффициентом (за него дают больше кубков). При равенстве — первый.
func bestCategoryID(categoryIDs []string) string {
	best := "other"
	bestCoef := -1.0
	for _, id := range categoryIDs {
		if c := ActivityCoeff(id); c > bestCoef {
			bestCoef = c
			best = id
		}
	}
	return best
}

// parseTrainingDoneReport — общий разбор первой строки отчёта: длительность, интенсивность, список видов.
func parseTrainingDoneReport(text string) (durationMin, intensity int, categoryIDs []string, ok bool) {
	line := strings.TrimSpace(text)
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	m := reTrainingHeader.FindStringSubmatch(line)
	if len(m) < 3 {
		return 0, 0, []string{"other"}, false
	}
	dur, err := strconv.Atoi(m[2])
	if err != nil || dur <= 0 {
		return 0, 0, []string{"other"}, false
	}
	intensity = 1
	if im := reIntensity.FindStringSubmatch(line); len(im) >= 2 {
		if v, e := strconv.Atoi(im[1]); e == nil && v > 0 {
			intensity = v
		}
	}
	return dur, intensity, parseCategoryIDs(m[1]), true
}

// ParseTrainingDoneReportCategories — все виды из отчёта (порядок — как ввёл пользователь).
// Для мультивыбора «бег + плавание» вернёт ["run","swim"].
func ParseTrainingDoneReportCategories(text string) (durationMin, intensity int, categoryIDs []string, ok bool) {
	return parseTrainingDoneReport(text)
}

// ParseTrainingDoneReport — первая строка отчёта мини-аппа, например «бег, 15 мин, инт. 3/5».
// При нескольких видах возвращает самый «дорогой» (с максимальным коэффициентом) — кубки начисляются за него.
// Возвращает ok=false, если нет распознанного заголовка (тогда начисление — минимум 1 кубок снаружи).
func ParseTrainingDoneReport(text string) (durationMin, intensity int, categoryID string, ok bool) {
	dur, inten, cats, ok := parseTrainingDoneReport(text)
	if !ok {
		return 0, 0, "other", false
	}
	return dur, inten, bestCategoryID(cats), true
}

// TrainingCupsFromParts — кубки по формуле §2.2: (длительность×интенсивность×тип)/5, округление до целого,
// минимум 1. Длительность 1–480 мин, интенсивность 1–5.
func TrainingCupsFromParts(durationMin, intensity int, categoryID string) int {
	d := durationMin
	if d < 1 {
		d = 1
	}
	if d > 480 {
		d = 480
	}
	in := intensity
	if in < 1 {
		in = 1
	}
	if in > 5 {
		in = 5
	}
	coef := ActivityCoeff(categoryID)
	raw := float64(d*in) * coef / 5.0
	n := int(math.Floor(raw + 0.5))
	if n < 1 {
		n = 1
	}
	return n
}

// TrainingCupsFromReportText — разбор + расчёт; при неразобранном заголовке возвращает 1.
func TrainingCupsFromReportText(text string) int {
	dur, inten, cat, ok := ParseTrainingDoneReport(text)
	if !ok {
		return 1
	}
	return TrainingCupsFromParts(dur, inten, cat)
}
