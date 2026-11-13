export type UILang = "en" | "ru";

type Dict = Record<string, string>;

const en: Dict = {
  "nav.menu": "Main menu",
  "nav.pack": "Pack",
  "nav.packUnread": "Pack, unread: {n}",
  "nav.leo": "Leo",
  "nav.leoUnread": "Leo, unread: {n}",
  "nav.rules": "Rules",
  "nav.profile": "Profile",
  "nav.addWorkout": "Log workout",
  "nav.composePlaceholder": "Message the pack…",
  "nav.composeAria": "Message to pack feed",
  "nav.attachPhoto": "Attach photo from gallery",
  "nav.replacePhoto": "Replace photo from gallery",
  "nav.removePhoto": "Remove photo",
  "nav.sendFeed": "Send message to feed",
  "nav.newFeedReplies": "New replies in pack feed",
  "nav.newFromLeo": "New from Leo",

  "lang.label": "Language",
  "lang.en": "English",
  "lang.ru": "Русский",
  "lang.group": "Interface language",

  "theme.label": "Theme",

  "common.loading": "Loading…",
  "common.save": "Save",
  "common.cancel": "Cancel",
  "common.error": "Something went wrong",
  "common.packMember": "Pack member",

  "level.1": "Meerkat",
  "level.2": "Gazelle",
  "level.3": "Zebra",
  "level.4": "Cheetah",
  "level.5": "Lion",
  "level.6": "Elephant",

  "profile.kickBanner": "WORK OUT BEFORE MIDNIGHT OR LEO WILL EAT YOU",
  "profile.language": "Language",

  "leo.translateEn": "Translate to English",
  "leo.showOriginal": "Show original",
  "leo.translateFail": "Couldn't translate",

  "feed.filterAll": "All",
  "feed.filterMine": "Mine",
  "feed.filterFriends": "Friends",
  "feed.filterWorkouts": "Workouts",
  "feed.filterMessages": "Messages",
  "feed.allTypes": "All types",
  "feed.allTypesAria": "All workout types",
  "feed.packWeek": "Pack week",
  "feed.packWeekAria": "Pack: {n} workouts this week",
  "feed.packWeekHintAria": "Pack weekly goal hint",
  "feed.emptyFriends": "Friend reports show up here. Follow members in profile → Pack friends.",
  "feed.emptyFilters": "Nothing matches filters — pick another type or All types / All.",
  "feed.showMore": "Show more",
  "feed.streak": "Streak",
  "feed.loadingHistory": "Loading history…",
  "feed.historyStart": "This is the start of pack history",

  "card.comments": "Comments",
  "card.commentsCount": "Comments · {n}",
  "card.collapseComments": "Collapse comments",
  "card.showMore": "Show more",
  "card.collapse": "Collapse",
  "card.streak": "Streak",
  "card.edited": "edited",
  "card.unreadReplies": "Unread replies",
  "card.openPhoto": "Open photo",
  "card.photoFailed": "Photo failed to load — tap to retry",
  "card.poll": "Poll",
  "card.votes": "{n} votes",
  "card.you": "You",

  "meta.sickLeave": "Sick leave",
  "meta.healthy": "Recovery",
  "meta.packJoin": "Leo · greeting",
  "meta.dailyWisdom": "Daily wisdom",
  "meta.packRoar": "Pack roar",
  "meta.packRemoved": "Leo · pack",
  "meta.packRemovedDetails": "Removed for inactivity",
  "meta.adminPost": "Admin · announcement",
  "meta.adminPostDetails": "Custom post",
  "meta.adminPoll": "Admin · poll",
  "meta.adminPollDetails": "Miniapp poll",
  "meta.inactiveNotice": "Leo · pack nudge",
  "meta.inactiveNoticeDetails": "Inactivity timer (context duplicate)",
  "meta.message": "Message",
  "meta.announcement": "Announcement",
};

const ru: Dict = {
  "nav.menu": "Основное меню",
  "nav.pack": "Стая",
  "nav.packUnread": "Стая, непрочитанных: {n}",
  "nav.leo": "Лео",
  "nav.leoUnread": "Лео, непрочитанных: {n}",
  "nav.rules": "Правила",
  "nav.profile": "Профиль",
  "nav.addWorkout": "Отметить тренировку",
  "nav.composePlaceholder": "Сообщение в стаю…",
  "nav.composeAria": "Сообщение в ленту стаи",
  "nav.attachPhoto": "Прикрепить фото из галереи",
  "nav.replacePhoto": "Заменить фото из галереи",
  "nav.removePhoto": "Убрать фото",
  "nav.sendFeed": "Отправить сообщение в ленту",
  "nav.newFeedReplies": "Новые ответы в ленте стаи",
  "nav.newFromLeo": "Новое от Лео",

  "lang.label": "Язык",
  "lang.en": "English",
  "lang.ru": "Русский",
  "lang.group": "Язык интерфейса",

  "theme.label": "Тема",

  "common.loading": "Загрузка…",
  "common.save": "Сохранить",
  "common.cancel": "Отмена",
  "common.error": "Что-то пошло не так",
  "common.packMember": "Участник стаи",

  "level.1": "Сурикат",
  "level.2": "Газель",
  "level.3": "Зебра",
  "level.4": "Гепард",
  "level.5": "Лев",
  "level.6": "Слон",

  "profile.kickBanner": "ТРЕНИРУЙСЯ ДО ПОЛНОЧИ ИЛИ ЛЕО ТЕБЯ СЪЕСТ",
  "profile.language": "Язык",

  "leo.translateEn": "Перевести на English",
  "leo.showOriginal": "Показать оригинал",
  "leo.translateFail": "Не удалось перевести",

  "feed.filterAll": "Все",
  "feed.filterMine": "Мои",
  "feed.filterFriends": "Друзья",
  "feed.filterWorkouts": "Тренировки",
  "feed.filterMessages": "Сообщения",
  "feed.allTypes": "Все типы",
  "feed.allTypesAria": "Все типы тренировок",
  "feed.packWeek": "Неделя стаи",
  "feed.packWeekAria": "Стая: {n} тренировок за неделю",
  "feed.packWeekHintAria": "Подсказка о недельной цели стаи",
  "feed.emptyFriends": "Здесь отчёты друзей. Подпишись на участников в профиле → «Друзья по стае».",
  "feed.emptyFilters": "Ничего не подходит под фильтры — выбери другой тип или «Все типы» / «Все».",
  "feed.showMore": "Показать ещё",
  "feed.streak": "Стрик",
  "feed.loadingHistory": "Загрузка истории…",
  "feed.historyStart": "Это начало истории стаи",

  "card.comments": "Комментарии",
  "card.commentsCount": "Комментарии · {n}",
  "card.collapseComments": "Свернуть комментарии",
  "card.showMore": "Показать полностью",
  "card.collapse": "Свернуть",
  "card.streak": "Стрик",
  "card.edited": "изменено",
  "card.unreadReplies": "Непрочитанные ответы",
  "card.openPhoto": "Открыть фото",
  "card.photoFailed": "Фото не загрузилось — нажми, чтобы повторить",
  "card.poll": "Опрос",
  "card.votes": "Голосов: {n}",
  "card.you": "Вы",

  "meta.sickLeave": "Больничный",
  "meta.healthy": "Выздоровление",
  "meta.packJoin": "Лео · приветствие",
  "meta.dailyWisdom": "Мудрость дня",
  "meta.packRoar": "Рык стаи",
  "meta.packRemoved": "Лео · стая",
  "meta.packRemovedDetails": "Выбыл за неактивность",
  "meta.adminPost": "Админ · объявление",
  "meta.adminPostDetails": "Кастомный пост",
  "meta.adminPoll": "Админ · опрос",
  "meta.adminPollDetails": "Голосование в miniapp",
  "meta.inactiveNotice": "Лео · напоминание стае",
  "meta.inactiveNoticeDetails": "Таймер неактивности (дубль контекста)",
  "meta.message": "Сообщение",
  "meta.announcement": "Объявление",
};

const catalogs: Record<UILang, Dict> = { en, ru };

export function isUILang(raw: unknown): raw is UILang {
  return raw === "en" || raw === "ru";
}

export function langFromTelegram(languageCode: string | undefined | null): UILang {
  const code = (languageCode || "").trim().toLowerCase();
  if (code.startsWith("ru")) return "ru";
  return "en";
}

export function t(lang: UILang, key: string, vars?: Record<string, string | number>): string {
  const dict = catalogs[lang] || catalogs.en;
  let s = dict[key] ?? catalogs.en[key] ?? key;
  if (vars) {
    for (const [k, v] of Object.entries(vars)) {
      s = s.split(`{${k}}`).join(String(v));
    }
  }
  return s;
}

export function levelName(lang: UILang, level: number): string {
  if (level < 1) return "";
  const capped = Math.min(level, 6);
  return t(lang, `level.${capped}`);
}
