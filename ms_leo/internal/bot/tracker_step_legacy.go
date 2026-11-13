package bot

import "strings"

// trackerLoHas checks English step/label text and legacy Russian stored in DB.
func trackerLoHas(s, en, ru string) bool {
	low := strings.ToLower(strings.TrimSpace(s))
	if en != "" && strings.Contains(low, strings.ToLower(en)) {
		return true
	}
	if ru != "" && strings.Contains(low, strings.ToLower(ru)) {
		return true
	}
	return false
}

func trackerLoEq(s, en, ru string) bool {
	low := strings.ToLower(strings.TrimSpace(s))
	return low == strings.ToLower(en) || (ru != "" && low == strings.ToLower(ru))
}
