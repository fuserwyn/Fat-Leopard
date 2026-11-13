const STR = {
  ru: {
    title: "Fat Leopard",
    sub: "Стая, тренировки и Лео — на компьютере",
    checking: "Проверяю сессию…",
    loading: "загрузка",
    idleHint: "Вход подтверждается в чате с ботом — пароль не нужен.",
    login: "Войти через Telegram",
    idleFoot: "Откроется Telegram. Нажми «Войти» в чате, и приложение продолжит само.",
    waitingTitle: "Подтверди вход в Telegram",
    waitingHint: "Жду подтверждения…",
    reopen: "Открыть Telegram снова",
    cancel: "Отмена",
    doneOpen: "Открываю Fat Leopard…",
    doneFallback: "Готово",
    hello: (name) => (name ? `Привет, ${name}!` : "Готово"),
    serverRequired: "Укажи адрес сервера Fat Leopard — тот же URL, что у кнопки «Открыть IDE» в боте.",
    expired: "Ссылка на вход устарела. Попробуй ещё раз.",
    timeout: "Не дождался подтверждения. Нажми «Войти» и подтверди в Telegram.",
    tauriMissing: "Не поднялся мост к Tauri — запусти приложение через npm run tauri dev.",
    settings: "Настройки подключения",
    serverLabel: "Адрес сервера",
    botLabel: "Бот",
    save: "Сохранить",
    logout: "Выйти из аккаунта",
    settingsHint: "Адрес — тот же, что у кнопки «Открыть IDE» в боте (WEBAPP_URL).",
    statTotal: "Всего",
    statWorking: "В работе",
    statAttention: "Внимание",
    statReview: "Ревью",
    projects: "Проекты",
    create: "Создать",
    guide: "Гайд",
    profile: "Профиль",
    pageTitle: "Fat Leopard — вход",
  },
  en: {
    title: "Fat Leopard",
    sub: "Your pack on desktop",
    checking: "Checking session…",
    loading: "loading",
    idleHint: "Sign-in is confirmed in the bot chat — no password needed.",
    login: "Sign in with Telegram",
    idleFoot: "Telegram will open. Tap «Sign in» in chat and the app continues automatically.",
    waitingTitle: "Confirm sign-in in Telegram",
    waitingHint: "Waiting for confirmation…",
    reopen: "Open Telegram again",
    cancel: "Cancel",
    doneOpen: "Opening Fat Leopard…",
    doneFallback: "Done",
    hello: (name) => (name ? `Hi, ${name}!` : "Done"),
    serverRequired: "Set the Fat Leopard server URL — the same as the «Open IDE» button in the bot.",
    expired: "Sign-in link expired. Try again.",
    timeout: "Confirmation timed out. Tap Sign in and confirm in Telegram.",
    tauriMissing: "Tauri bridge failed — run the app via npm run tauri dev.",
    settings: "Connection settings",
    serverLabel: "Server URL",
    botLabel: "Bot",
    save: "Save",
    logout: "Sign out",
    settingsHint: "Same URL as the «Open IDE» button in the bot (WEBAPP_URL).",
    statTotal: "Total",
    statWorking: "In progress",
    statAttention: "Attention",
    statReview: "Review",
    projects: "Projects",
    create: "Create",
    guide: "Guide",
    profile: "Profile",
    pageTitle: "Fat Leopard — sign in",
  },
};

export function pickLang() {
  try {
    const saved = localStorage.getItem("mvl-lang");
    if (saved === "en" || saved === "ru") return saved;
  } catch (_) {}
  const nav = (navigator.language || "en").toLowerCase();
  return nav.startsWith("ru") ? "ru" : "en";
}

export function strings(lang = pickLang()) {
  return STR[lang] || STR.ru;
}

export function applyDom(lang = pickLang()) {
  const s = strings(lang);
  document.documentElement.lang = lang;
  document.title = s.pageTitle;
  const map = [
    [".title", s.title],
    [".page-sub", s.sub],
    ["#viewChecking .muted", s.checking],
    ["#viewIdle .muted", s.idleHint],
    ["#loginBtn", s.login],
    ["#viewIdle .hint", s.idleFoot],
    ["#viewWaiting .strong", s.waitingTitle],
    ["#viewWaiting .muted", s.waitingHint],
    ["#viewDone .muted", s.doneOpen],
    ["#reopenBtn", s.reopen],
    ["#cancelBtn", s.cancel],
    ["#saveSettingsBtn", s.save],
    ["#logoutBtn", s.logout],
    ["#settings summary", s.settings],
    ['label[for="baseUrl"]', s.serverLabel],
    ['label[for="botName"]', s.botLabel],
    ["#settings .hint", s.settingsHint],
    [".grid-label:nth-of-type(1)", s.statTotal],
  ];
  map.forEach(([sel, text]) => {
    const el = document.querySelector(sel);
    if (el) el.textContent = text;
  });
  const stats = document.querySelectorAll(".grid-label");
  const statTexts = [s.statTotal, s.statWorking, s.statAttention, s.statReview];
  stats.forEach((el, i) => {
    if (statTexts[i]) el.textContent = statTexts[i];
  });
  const navLabels = document.querySelectorAll(".bottom-nav__label");
  [s.projects, s.create, s.guide, s.profile].forEach((t, i) => {
    if (navLabels[i]) navLabels[i].textContent = t;
  });
  const ws = document.querySelector(".ws-panel .section-title");
  if (ws) ws.textContent = s.projects;
  document.querySelectorAll(".spinner").forEach((el) => {
    el.setAttribute("aria-label", s.loading);
  });
  return s;
}
