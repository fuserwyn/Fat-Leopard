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
