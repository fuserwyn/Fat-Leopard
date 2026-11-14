package bot

import (
	"fmt"

	"leo-bot/internal/i18n"
)

// Админка всегда по-русски, но уведомления, которые админ шлёт участнику
// (больничный, UGC-мьют), уходят на языке самого участника.

func adminSickSetNotice(lang i18n.Lang, withChatHint bool) string {
	if i18n.Or(lang, i18n.EN) == i18n.RU {
		if withChatHint {
			return "🏥 Администратор оформил тебе больничный. Таймер неактивности приостановлен — выздоравливай! Когда поправишься — «Выйти с больничного» в профиле или #healthy в чате."
		}
		return "🏥 Администратор оформил тебе больничный. Таймер неактивности приостановлен — выздоравливай! Когда поправишься — «Выйти с больничного» в профиле."
	}
	if withChatHint {
		return "🏥 An admin set sick leave for you. Inactivity timer paused — get well! When recovered — End sick leave in profile or #healthy in chat."
	}
	return "🏥 An admin set sick leave for you. Inactivity timer paused — get well! When recovered — «End sick leave» in profile."
}

func adminSickCancelNotice(lang i18n.Lang) string {
	if i18n.Or(lang, i18n.EN) == i18n.RU {
		return "🏥 Больничный завершён администратором. Таймер неактивности снова активен — не забудь про тренировку!"
	}
	return "🏥 Sick leave ended by an admin. Inactivity timer is on again — don't miss your workout!"
}

func adminUGCMuteNotice(lang i18n.Lang, hours int) string {
	if i18n.Or(lang, i18n.EN) == i18n.RU {
		return fmt.Sprintf("🔇 Публикации в Стае ограничены на %d ч. по решению модератора.", hours)
	}
	return fmt.Sprintf("🔇 Pack publishing restricted for %d h by moderator.", hours)
}

func adminUGCUnmuteNotice(lang i18n.Lang) string {
	if i18n.Or(lang, i18n.EN) == i18n.RU {
		return "🔊 Ограничения на публикации в Стае сняты. Можно снова писать в ленте, чате и заметках."
	}
	return "🔊 Pack publishing restrictions lifted. You can post in feed, chat and notes again."
}
