// Package i18n — язык UI пользователя (miniapp + бот + Лео).
package i18n

import (
	"fmt"
	"strings"
)

type Lang string

const (
	EN Lang = "en"
	RU Lang = "ru"
)

// Normalize — только ru|en; пустое → "".
func Normalize(raw string) Lang {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "ru", "ru-ru", "ru_ru":
		return RU
	case "en", "en-us", "en_gb", "en-gb":
		return EN
	default:
		return ""
	}
}

// FromTelegram — language_code Telegram → ru|en.
func FromTelegram(languageCode string) Lang {
	code := strings.ToLower(strings.TrimSpace(languageCode))
	if code == "" {
		return EN
	}
	if strings.HasPrefix(code, "ru") {
		return RU
	}
	return EN
}

// Or — если a пуст, вернуть b (или EN).
func Or(a, b Lang) Lang {
	if a == RU || a == EN {
		return a
	}
	if b == RU || b == EN {
		return b
	}
	return EN
}

// T — строка по ключу; fallback EN, затем сам ключ.
func T(lang Lang, key string, args ...any) string {
	lang = Or(lang, EN)
	s := lookup(lang, key)
	if s == "" && lang != EN {
		s = lookup(EN, key)
	}
	if s == "" {
		s = key
	}
	if len(args) == 0 {
		return s
	}
	return fmt.Sprintf(s, args...)
}

func lookup(lang Lang, key string) string {
	m := catalogEN
	if lang == RU {
		m = catalogRU
	}
	if m == nil {
		return ""
	}
	return m[key]
}
