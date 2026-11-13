import { useCallback, useEffect, useRef, useState } from "react";
import { STREAK_ACHIEVEMENTS, WORKOUT_ACHIEVEMENTS, workoutsWordRu } from "../lib/achievements";
import { inactivityHighlight } from "../lib/inactivityHighlight";
import { detectTimezoneInfo } from "../lib/timezoneSync";
import { inactiveDaysFromRemovalRemaining, removalRemainingUntil } from "../lib/inactivityRemoval";
import { cupsLevelProgressBarPct, formatCupsLevelProgressLabel, miniappCupsLevelProgress, miniappLevelFromCups } from "../lib/miniappLevel";
import { LevelCarousel } from "./LevelCarousel";
import {
  canUseStreakSave,
  effectiveStreakDays,
  streakBurnLabel,
} from "../lib/streakLabel";
import { PACK_WEEKLY_GOAL_DEFAULT } from "../lib/packWeeklyGoal";
import {
  canUseLeopardTheme,
  canUsePackTheme,
  canUseWildTheme,
  getStoredTheme,
  isThemeMode,
  persistThemeToServer,
  setTheme,
  themeAllowedForLevel,
  type ThemeMode,
} from "../lib/theme";
import {
  createCardDonatePayment,
  createStarsDonateInvoice,
  emptyDonateOptions,
  fetchDonateOptions,
  waitForDonationCompleted,
  type DonateOptions,
} from "../lib/donate";
import { DonateThanksToast } from "./DonateThanksToast";
import { InviteFriendsSection } from "./InviteFriendsSection";
import { CupsHistorySheet } from "./CupsHistorySheet";
import "./ProfileScreen.css";

const api = (import.meta.env.VITE_MINIAPP_API_URL as string | undefined)?.replace(/\/$/, "") ?? "";

export type ProfileData = {
  gender: "m" | "f" | "";
  displayName: string;
  timezoneOffset: number;
};

const EMPTY_PROFILE: ProfileData = { gender: "", displayName: "", timezoneOffset: 0 };

const THEME_LABELS: Record<ThemeMode, string> = {
  dark: "🌙 Dark",
  light: "☀️ Light",
  leopard: "🐆 Pink",
  wild: "🐆 Wild",
  pack: "🏆 Pack",
};

export type FriendMember = {
  user_id: number;
  name: string;
  streak_days: number;
  following: boolean;
  notify_workouts: boolean;
};

function normalizeProfileData(profile: ProfileData): ProfileData {
  return {
    gender: profile.gender,
    displayName: profile.displayName.trim(),
    timezoneOffset: Math.max(-12, Math.min(12, Math.trunc(profile.timezoneOffset || 0))),
  };
}

function donateTimesWordRu(n: number): string {
  return n === 1 ? "time" : "times";
}

type Props = {
  name: string;
  streak: number;
  recordStreak: number;
  /** Максимум кубков за одну тренировку / активность. */
  recordCups: number;
  xp: number;
  achievementCount: number;
  achievementsMax: number;
  workouts: number;
  /** Дней в стае с первого входа (включая день входа). 0 — не показываем подпись. */
  daysInPack?: number;
  /** Дней с последней тренировки. -1 — тренировок ещё не было. */
  daysSinceLastTraining: number;
  /** YYYY-MM-DD последней тренировки в локальном TZ пользователя. */
  lastTrainingDate?: string;
  /** RFC3339 — дедлайн кика за неактивность (совпадает с таймером Лео). */
  inactivityRemovalAt?: string;
  initData: string;
  inTelegram: boolean;
  /** Ссылка на аватар из Telegram WebApp (initDataUnsafe.user.photo_url), если бот открыл мини-апп. */
  userPhotoUrl?: string;
  showAlert: (m: string) => void;
  /** Недельная цель стаи с сервера (её может менять админ). */
  packWorkoutsGoal?: number;
  onProfileSaved?: (displayName: string) => void;
  onStreakSaved?: () => void;
  onSupport?: () => void;
  /** Открыть админку мини-аппа (только если isAdmin). */
  onAdmin?: () => void;
  isAdmin?: boolean;
  /** Перезагрузить кубки/уровень/стрик с сервера (из App). */
  onRefreshStats?: () => void;
  /** Вкладка «Профиль» видима (keep-alive). */
  active?: boolean;
};

export function ProfileScreen({
  name,
  streak,
  recordStreak,
  recordCups,
  xp,
  achievementCount,
  achievementsMax,
  workouts,
  daysSinceLastTraining,
  lastTrainingDate,
  inactivityRemovalAt,
  initData,
  inTelegram,
  userPhotoUrl,
  showAlert,
  packWorkoutsGoal = PACK_WEEKLY_GOAL_DEFAULT,
  onProfileSaved,
  onStreakSaved,
  onSupport,
  onAdmin,
  isAdmin = false,
  onRefreshStats,
  active = true,
}: Props) {
  const [cups, setCups] = useState(xp);
  const [theme, setThemeState] = useState<ThemeMode>(() => getStoredTheme());
  const [themeLevelReady, setThemeLevelReady] = useState(false);
  const [packBonusThemeActive, setPackBonusThemeActive] = useState(false);
  const changeTheme = useCallback((mode: ThemeMode) => {
    const next = themeAllowedForLevel(mode, miniappLevelFromCups(cups), {
      streakDays: streak,
      maxStreakDays: recordStreak,
      workoutsTotal: workouts,
      isAdmin,
      packBonusThemeActive,
    });
    setTheme(next);
    setThemeState(next);
    if (initData?.trim()) void persistThemeToServer(initData, next);
  }, [cups, initData, streak, recordStreak, workouts, isAdmin, packBonusThemeActive]);
  const [profile, setProfile] = useState<ProfileData>(EMPTY_PROFILE);
  const [savedProfile, setSavedProfile] = useState<ProfileData>(EMPTY_PROFILE);
  const [profileLoading, setProfileLoading] = useState(true);
  const [profileSaving, setProfileSaving] = useState(false);

  const [onSick, setOnSick] = useState<boolean | null>(null);
  const [healthOpen, setHealthOpen] = useState(false);
  const [sickFormOpen, setSickFormOpen] = useState(false);
  const [sickReason, setSickReason] = useState("");
  const [healthBusy, setHealthBusy] = useState(false);
  const [healthInputFocused, setHealthInputFocused] = useState(false);
  const healthTextareaRef = useRef<HTMLTextAreaElement | null>(null);

  const [saveStreakUsed, setSaveStreakUsed] = useState(0);
  const [saveStreakMax, setSaveStreakMax] = useState(1);
  const [saveStreakBusy, setSaveStreakBusy] = useState(false);
  const saveStreakAvail = Math.max(0, saveStreakMax - saveStreakUsed);

  // Напоминания «внеси тренировку»: вкл/выкл + час по локальному времени пользователя.
  // По умолчанию ВЫКЛ — напоминание приходит, только если пользователь сам включил галочку.
  const [reminderEnabled, setReminderEnabled] = useState(false);
  const [reminderHour, setReminderHour] = useState(19);
  const [reminderLoading, setReminderLoading] = useState(true);
  const [reminderBusy, setReminderBusy] = useState(false);

  // Подписка на «мудрость дня»: только вкл/выкл. Время фиксированное — 04:20 по локальному
  // времени пользователя. По умолчанию ВЫКЛ — мудрость приходит в личку, только если подписался.
  const [wisdomEnabled, setWisdomEnabled] = useState(false);
  const [wisdomHour, setWisdomHour] = useState(4);
  const [wisdomLoading, setWisdomLoading] = useState(true);
  const [wisdomBusy, setWisdomBusy] = useState(false);

  // Уведомления о лайках в ленте: только вкл/выкл. По умолчанию ВЫКЛ — DM о лайке на
  // твоём отчёте или комментарии приходит, только если сам включил галочку.
  const [likeNotifyEnabled, setLikeNotifyEnabled] = useState(false);
  const [likeNotifyLoading, setLikeNotifyLoading] = useState(true);
  const [likeNotifyBusy, setLikeNotifyBusy] = useState(false);
  const [contactJoinEnabled, setContactJoinEnabled] = useState(true);
  const [contactJoinCount, setContactJoinCount] = useState(0);
  const [contactJoinLoading, setContactJoinLoading] = useState(true);
  const [contactJoinBusy, setContactJoinBusy] = useState(false);

  // Донат: добровольная поддержка проекта. Вход в стаю бесплатный, поэтому донат ничего
  // не открывает — номиналы и способы приходят с бэкенда (что настроено, то и показываем).
  const [donateOptions, setDonateOptions] = useState<DonateOptions>(emptyDonateOptions);
  const [donateStars, setDonateStars] = useState<number | null>(null);
  const [donateRub, setDonateRub] = useState<number | null>(null);
  const [donateBusy, setDonateBusy] = useState(false);
  const [donateThanks, setDonateThanks] = useState(false);

  // Друзья по стае: только те, за кем viewer уже подписался в ленте.
  const [friends, setFriends] = useState<FriendMember[]>([]);
  const [friendsLoading, setFriendsLoading] = useState(false);
  const [followingOpen, setFollowingOpen] = useState(false);
  // Уведомления свёрнуты по умолчанию: пять тумблеров подряд занимали пол-экрана
  // профиля, а меняют их редко.
  const [notifOpen, setNotifOpen] = useState(false);
  // Имя и пол спрятаны за шевроном: меняют редко, на экране занимают заметное место.
  const [personalOpen, setPersonalOpen] = useState(false);
  const [donateModalOpen, setDonateModalOpen] = useState(false);
  // Тема свёрнута по умолчанию: четыре кнопки и подсказки занимают место, меняют редко.
  const [themeOpen, setThemeOpen] = useState(false);
  const [cupsHistoryOpen, setCupsHistoryOpen] = useState(false);
  const [friendBusyId, setFriendBusyId] = useState<number | null>(null);
  // Глобальный тумблер «Тренировки друзей» (центр уведомлений): DM о тренировке
  // друга по всем подпискам сразу. Состояние — bool_or по notify_workouts подписок.
  const [friendNotifyBusy, setFriendNotifyBusy] = useState(false);
  const friendNotifyEnabled = friends.some((m) => m.notify_workouts);

  useEffect(() => {
    setCups(xp);
  }, [xp]);

  // Время, чтобы пересчитывать остаток до сгорания стрика. Тикает раз в минуту, пока вкладка видима.
  const [now, setNow] = useState(() => new Date());
  const displayStreak = effectiveStreakDays(streak, daysSinceLastTraining, now, lastTrainingDate);
  const burnLabel = streakBurnLabel(streak, daysSinceLastTraining, now, lastTrainingDate);

  // Сколько ачивок за тренировки уже открыто = число порогов, не превышающих total.
  const workoutAchEarned = WORKOUT_ACHIEVEMENTS.filter(({ count }) => workouts >= count).length;

  const scrollHealthAboveKeyboard = useCallback(() => {
    const ta = healthTextareaRef.current;
    if (!ta) return;
    const gap = 10;
    // Граница, ниже которой поле нельзя опускать, — это РЕАЛЬНЫЙ верх зафиксированной
    // панели с кнопкой «Отправить заявку». Привязка к её позиции даёт одинаковый
    // результат на iOS и Android. Раньше граница считалась по visualViewport, который
    // в Android TG WebView расходится с позицией панели → поле уезжало слишком вверх.
    const bar = document.querySelector<HTMLElement>(".profile__health-actions--keyboard");
    let limitBottom: number;
    if (bar) {
      limitBottom = bar.getBoundingClientRect().top - gap;
    } else {
      const vv = window.visualViewport;
      const visualH = vv?.height ?? window.innerHeight;
      const offsetTop = vv?.offsetTop ?? 0;
      limitBottom = offsetTop + visualH - 44 - gap;
    }
    const taRect = ta.getBoundingClientRect();
    const delta = taRect.bottom - limitBottom;
    // Порог гасит дрожание/петлю обратной связи: прокручиваем только если поле реально
    // перекрыто панелью/клавиатурой. behavior:"auto" — мгновенно, чтобы плавный скролл
    // не плодил новые scroll-события и не запускал прокрутку повторно.
    if (delta > 4) {
      window.scrollBy({ top: delta, behavior: "auto" });
    }
  }, []);

  // Пока вкладка видима и есть что показывать — пересчитываем остаток до сгорания стрика раз в минуту.
  useEffect(() => {
    if (!active || burnLabel == null) return;
    setNow(new Date()); // освежаем при возврате на вкладку, не дожидаясь первого тика
    const id = window.setInterval(() => setNow(new Date()), 60_000);
    return () => window.clearInterval(id);
  }, [active, burnLabel]);

  const cupProgress = miniappCupsLevelProgress(cups);
  const cupProgressLabel = formatCupsLevelProgressLabel(cupProgress);
  const level = miniappLevelFromCups(cups);
  const barPct = cupsLevelProgressBarPct(cupProgress);
  const leopardUnlocked = canUseLeopardTheme(level);
  const wildUnlocked = canUseWildTheme({
    streakDays: displayStreak,
    maxStreakDays: recordStreak,
    workoutsTotal: workouts,
    isAdmin,
  });
  const packUnlocked = canUsePackTheme({ packBonusThemeActive });
  const themeUnlock = {
    streakDays: displayStreak,
    maxStreakDays: recordStreak,
    workoutsTotal: workouts,
    isAdmin,
    packBonusThemeActive,
  };

  useEffect(() => {
    const onTheme = (e: Event) => {
      const mode = (e as CustomEvent<ThemeMode>).detail;
      if (isThemeMode(mode)) setThemeState(mode);
    };
    window.addEventListener("leo-theme", onTheme);
    return () => window.removeEventListener("leo-theme", onTheme);
  }, []);

  useEffect(() => {
    // Пока кубки не приехали с сервера, уровень = 1. Нельзя затирать Розовую.
    // Дикую здесь не трогаем: стрик/админ в пропсах сначала нули и затёрли бы тему.
    if (!themeLevelReady) return;
    const stored = getStoredTheme();
    if (stored !== "leopard") return;
    const next = themeAllowedForLevel(stored, level, themeUnlock);
    if (next !== theme) {
      setTheme(next);
      setThemeState(next);
    }
  }, [level, theme, themeLevelReady, displayStreak, recordStreak, workouts, isAdmin]);

  const load = useCallback(async () => {
    if (!api || !inTelegram || !initData?.trim()) {
      setProfileLoading(false);
      return;
    }
    setProfileLoading(true);
    try {
      const res = await fetch(`${api}/api/miniapp/profile/load`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ init_data: initData }),
      });
      const j = (await res.json().catch(() => ({}))) as {
        ok?: boolean;
        error?: string;
        gender?: string;
        display_name?: string;
        timezone_offset?: number;
        xp?: number;
        theme?: string;
        streak_days?: number;
        max_streak_days?: number;
        workouts_total?: number;
        is_admin?: boolean;
        pack_bonus_theme_active?: boolean;
        streak_save_attempts_used?: number;
        streak_save_attempts_max?: number;
      };
      if (!res.ok) {
        showAlert(j.error ?? `Profile: error ${res.status}`);
        return;
      }
      if (!j.ok) return;
      const g: ProfileData["gender"] = j.gender === "m" ? "m" : j.gender === "f" ? "f" : "";
      const dn = (j.display_name ?? "").trim() || (name && name !== "friend" ? name : "");
      const tz =
        typeof j.timezone_offset === "number" && Number.isFinite(j.timezone_offset)
          ? Math.max(-12, Math.min(12, Math.trunc(j.timezone_offset)))
          : 0;
      const nextProfile = {
        gender: g,
        displayName: dn,
        timezoneOffset: tz,
      };
      setProfile(nextProfile);
      setSavedProfile(normalizeProfileData(nextProfile));
      if (typeof j.xp === "number") {
        setCups(Math.max(0, j.xp));
      }
      const bonusThemeActive = Boolean(j.pack_bonus_theme_active);
      setPackBonusThemeActive(bonusThemeActive);
      if (isThemeMode(j.theme)) {
        const next = themeAllowedForLevel(j.theme, miniappLevelFromCups(typeof j.xp === "number" ? j.xp : cups), {
          streakDays: typeof j.streak_days === "number" ? j.streak_days : streak,
          maxStreakDays: typeof j.max_streak_days === "number" ? j.max_streak_days : recordStreak,
          workoutsTotal: typeof j.workouts_total === "number" ? j.workouts_total : workouts,
          isAdmin: typeof j.is_admin === "boolean" ? j.is_admin : isAdmin,
          packBonusThemeActive: bonusThemeActive,
        });
        setTheme(next);
        setThemeState(next);
      }
      setThemeLevelReady(true);
      if (typeof j.streak_save_attempts_used === "number") {
        setSaveStreakUsed(Math.max(0, j.streak_save_attempts_used));
      }
      if (typeof j.streak_save_attempts_max === "number") {
        setSaveStreakMax(Math.max(1, j.streak_save_attempts_max));
      }
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Network error");
    } finally {
      setProfileLoading(false);
    }
  }, [inTelegram, initData, name, showAlert]);

  useEffect(() => {
    if (!active) return;
    void load();
    onRefreshStats?.();
  }, [load, active, onRefreshStats]);

  const loadReminder = useCallback(async () => {
    if (!api || !inTelegram || !initData?.trim()) {
      setReminderLoading(false);
      return;
    }
    setReminderLoading(true);
    try {
      const res = await fetch(`${api}/api/miniapp/reminders/load`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ init_data: initData }),
      });
      const j = (await res.json().catch(() => ({}))) as { ok?: boolean; enabled?: boolean; remind_hour?: number };
      if (res.ok && j.ok) {
        setReminderEnabled(Boolean(j.enabled));
        if (typeof j.remind_hour === "number" && j.remind_hour >= 0 && j.remind_hour <= 23) {
          setReminderHour(Math.trunc(j.remind_hour));
        }
      }
    } catch {
      // тихо: не критично
    } finally {
      setReminderLoading(false);
    }
  }, [inTelegram, initData]);

  const saveReminder = useCallback(
    async (enabled: boolean, hour: number) => {
      if (!api || !inTelegram || !initData?.trim()) {
        showAlert("Open the miniapp from Telegram (initData required).");
        return;
      }
      // Оптимистично применяем, чтобы UI не «прыгал».
      const prevEnabled = reminderEnabled;
      const prevHour = reminderHour;
      setReminderEnabled(enabled);
      setReminderHour(hour);
      setReminderBusy(true);
      try {
        const res = await fetch(`${api}/api/miniapp/reminders/save`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ init_data: initData, enabled, remind_hour: hour }),
        });
        const j = (await res.json().catch(() => ({}))) as { ok?: boolean; error?: string };
        if (!res.ok || !j.ok) {
          setReminderEnabled(prevEnabled);
          setReminderHour(prevHour);
          showAlert(j.error ?? `Reminders: error ${res.status}`);
        }
      } catch (e) {
        setReminderEnabled(prevEnabled);
        setReminderHour(prevHour);
        showAlert(e instanceof Error ? e.message : "Network error");
      } finally {
        setReminderBusy(false);
      }
    },
    [inTelegram, initData, reminderEnabled, reminderHour, showAlert],
  );

  useEffect(() => {
    if (!active) return;
    void loadReminder();
  }, [loadReminder, active]);

  const loadWisdomSub = useCallback(async () => {
    if (!api || !inTelegram || !initData?.trim()) {
      setWisdomLoading(false);
      return;
    }
    setWisdomLoading(true);
    try {
      const res = await fetch(`${api}/api/miniapp/wisdom-sub/load`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ init_data: initData }),
      });
      const j = (await res.json().catch(() => ({}))) as { ok?: boolean; enabled?: boolean; remind_hour?: number };
      if (res.ok && j.ok) {
        setWisdomEnabled(Boolean(j.enabled));
        if (typeof j.remind_hour === "number" && j.remind_hour >= 0 && j.remind_hour <= 23) {
          setWisdomHour(Math.trunc(j.remind_hour));
        }
      }
    } catch {
      // тихо: не критично
    } finally {
      setWisdomLoading(false);
    }
  }, [inTelegram, initData]);

  const saveWisdomSub = useCallback(
    async (enabled: boolean, hour: number) => {
      if (!api || !inTelegram || !initData?.trim()) {
        showAlert("Open the miniapp from Telegram (initData required).");
        return;
      }
      // Оптимистично применяем, чтобы UI не «прыгал».
      const prevEnabled = wisdomEnabled;
      const prevHour = wisdomHour;
      setWisdomEnabled(enabled);
      setWisdomHour(hour);
      setWisdomBusy(true);
      try {
        const res = await fetch(`${api}/api/miniapp/wisdom-sub/save`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ init_data: initData, enabled, remind_hour: hour }),
        });
        const j = (await res.json().catch(() => ({}))) as { ok?: boolean; error?: string };
        if (!res.ok || !j.ok) {
          setWisdomEnabled(prevEnabled);
          setWisdomHour(prevHour);
          showAlert(j.error ?? `Daily wisdom: error ${res.status}`);
        }
      } catch (e) {
        setWisdomEnabled(prevEnabled);
        setWisdomHour(prevHour);
        showAlert(e instanceof Error ? e.message : "Network error");
      } finally {
        setWisdomBusy(false);
      }
    },
    [inTelegram, initData, wisdomEnabled, wisdomHour, showAlert],
  );

  useEffect(() => {
    if (!active) return;
    void loadWisdomSub();
  }, [loadWisdomSub, active]);

  const loadLikeNotify = useCallback(async () => {
    if (!api || !inTelegram || !initData?.trim()) {
      setLikeNotifyLoading(false);
      return;
    }
    setLikeNotifyLoading(true);
    try {
      const res = await fetch(`${api}/api/miniapp/like-notifications/load`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ init_data: initData }),
      });
      const j = (await res.json().catch(() => ({}))) as { ok?: boolean; enabled?: boolean };
      if (res.ok && j.ok) {
        setLikeNotifyEnabled(Boolean(j.enabled));
      }
    } catch {
      // тихо: не критично
    } finally {
      setLikeNotifyLoading(false);
    }
  }, [inTelegram, initData]);

  const saveLikeNotify = useCallback(
    async (enabled: boolean) => {
      if (!api || !inTelegram || !initData?.trim()) {
        showAlert("Open the miniapp from Telegram (initData required).");
        return;
      }
      // Оптимистично применяем, чтобы UI не «прыгал».
      const prevEnabled = likeNotifyEnabled;
      setLikeNotifyEnabled(enabled);
      setLikeNotifyBusy(true);
      try {
        const res = await fetch(`${api}/api/miniapp/like-notifications/save`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ init_data: initData, enabled }),
        });
        const j = (await res.json().catch(() => ({}))) as { ok?: boolean; error?: string };
        if (!res.ok || !j.ok) {
          setLikeNotifyEnabled(prevEnabled);
          showAlert(j.error ?? `Like notifications: error ${res.status}`);
        }
      } catch (e) {
        setLikeNotifyEnabled(prevEnabled);
        showAlert(e instanceof Error ? e.message : "Network error");
      } finally {
        setLikeNotifyBusy(false);
      }
    },
    [inTelegram, initData, likeNotifyEnabled, showAlert],
  );

  useEffect(() => {
    if (!active) return;
    void loadLikeNotify();
  }, [loadLikeNotify, active]);

  const loadContactJoin = useCallback(async () => {
    if (!api || !inTelegram || !initData?.trim()) {
      setContactJoinLoading(false);
      return;
    }
    setContactJoinLoading(true);
    try {
      const res = await fetch(`${api}/api/miniapp/contact-join-notifications/load`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ init_data: initData }),
      });
      const j = (await res.json().catch(() => ({}))) as {
        ok?: boolean;
        enabled?: boolean;
        contacts_count?: number;
      };
      if (res.ok && j.ok) {
        setContactJoinEnabled(j.enabled !== false);
        setContactJoinCount(typeof j.contacts_count === "number" ? j.contacts_count : 0);
      }
    } catch {
      // тихо: не критично
    } finally {
      setContactJoinLoading(false);
    }
  }, [inTelegram, initData]);

  const saveContactJoin = useCallback(
    async (enabled: boolean) => {
      if (!api || !inTelegram || !initData?.trim()) {
        showAlert("Open the miniapp from Telegram (initData required).");
        return;
      }
      const prevEnabled = contactJoinEnabled;
      setContactJoinEnabled(enabled);
      setContactJoinBusy(true);
      try {
        const res = await fetch(`${api}/api/miniapp/contact-join-notifications/save`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ init_data: initData, enabled }),
        });
        const j = (await res.json().catch(() => ({}))) as { ok?: boolean; error?: string };
        if (!res.ok || !j.ok) {
          setContactJoinEnabled(prevEnabled);
          showAlert(j.error ?? `Pack contacts: error ${res.status}`);
        }
      } catch (e) {
        setContactJoinEnabled(prevEnabled);
        showAlert(e instanceof Error ? e.message : "Network error");
      } finally {
        setContactJoinBusy(false);
      }
    },
    [inTelegram, initData, contactJoinEnabled, showAlert],
  );

  useEffect(() => {
    if (!active) return;
    void loadContactJoin();
  }, [loadContactJoin, active]);

  const loadDonateOptions = useCallback(async () => {
    if (!inTelegram || !initData?.trim()) return;
    const opts = await fetchDonateOptions(initData);
    setDonateOptions(opts);
    setDonateStars((cur) => cur ?? opts.starsTiers[0] ?? null);
    setDonateRub((cur) => cur ?? opts.cardTiersRub[0] ?? null);
  }, [inTelegram, initData]);

  useEffect(() => {
    if (!active) return;
    void loadDonateOptions();
  }, [loadDonateOptions, active]);

  const finishDonate = useCallback(() => {
    setDonateModalOpen(false);
    setDonateThanks(true);
    void window.Telegram?.WebApp?.HapticFeedback?.notificationOccurred?.("success");
    void loadDonateOptions();
  }, [loadDonateOptions]);

  // Звёзды: счёт открывается прямо в мини-аппе (WebApp.openInvoice), «спасибо» в личку шлёт бот.
  const donateWithStars = useCallback(async () => {
    if (!inTelegram || !initData?.trim() || donateStars === null || donateBusy) return;
    const wa = window.Telegram?.WebApp;
    if (!wa?.openInvoice) {
      showAlert("Update Telegram — Stars payments need a recent app version.");
      return;
    }
    setDonateBusy(true);
    try {
      const invoice = await createStarsDonateInvoice(initData, donateStars);
      if (!invoice) {
        showAlert("Couldn't create invoice. Try again later.");
        return;
      }
      wa.openInvoice(invoice.link, (status) => {
        setDonateBusy(false);
        if (status === "paid") finishDonate();
        else if (status === "failed") showAlert("Payment failed. Try again.");
      });
      return; // setDonateBusy(false) вызовет колбэк Telegram
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Network error");
    }
    setDonateBusy(false);
  }, [inTelegram, initData, donateStars, donateBusy, showAlert, finishDonate]);

  // Карта РФ: страница ЮKassa открывается в браузере, поэтому статус доопрашиваем сами.
  const donateWithCard = useCallback(async () => {
    if (!inTelegram || !initData?.trim() || donateRub === null || donateBusy) return;
    setDonateBusy(true);
    try {
      const payment = await createCardDonatePayment(initData, donateRub);
      if (!payment) {
        showAlert("Couldn't create payment. Try again later.");
        return;
      }
      const wa = window.Telegram?.WebApp;
      if (wa?.openLink) wa.openLink(payment.link);
      else window.open(payment.link, "_blank", "noopener");
      if (await waitForDonationCompleted(initData, payment.donationId)) finishDonate();
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Network error");
    } finally {
      setDonateBusy(false);
    }
  }, [inTelegram, initData, donateRub, donateBusy, showAlert, finishDonate]);

  const loadFriends = useCallback(async () => {
    if (!api || !inTelegram || !initData?.trim()) {
      return;
    }
    setFriendsLoading(true);
    try {
      const res = await fetch(`${api}/api/miniapp/friends/list`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ init_data: initData }),
      });
      const j = (await res.json().catch(() => ({}))) as { ok?: boolean; error?: string; members?: FriendMember[] };
      if (!res.ok || !j.ok) {
        showAlert(j.error ?? `Friends: error ${res.status}`);
        return;
      }
      setFriends(Array.isArray(j.members) ? j.members : []);
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Network error");
    } finally {
      setFriendsLoading(false);
    }
  }, [inTelegram, initData, showAlert]);

  const toggleFollow = useCallback(
    async (member: FriendMember) => {
      if (!api || !inTelegram || !initData?.trim() || friendBusyId !== null) return;
      const next = !member.following;
      setFriendBusyId(member.user_id);
      // Оптимистичное обновление.
      setFriends((list) =>
        next
          ? list.map((m) => (m.user_id === member.user_id ? { ...m, following: next } : m))
          : list.filter((m) => m.user_id !== member.user_id),
      );
      try {
        const res = await fetch(`${api}/api/miniapp/friends/${next ? "follow" : "unfollow"}`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ init_data: initData, target_id: member.user_id }),
        });
        const j = (await res.json().catch(() => ({}))) as { ok?: boolean; error?: string };
        if (!res.ok || !j.ok) {
          setFriends((list) => {
            if (next) {
              return list.map((m) => (m.user_id === member.user_id ? { ...m, following: !next } : m));
            }
            return [...list, member].sort((a, b) => b.streak_days - a.streak_days || a.user_id - b.user_id);
          });
          showAlert(j.error ?? `Error ${res.status}`);
        }
      } catch (e) {
        setFriends((list) => {
          if (next) {
            return list.map((m) => (m.user_id === member.user_id ? { ...m, following: !next } : m));
          }
          return [...list, member].sort((a, b) => b.streak_days - a.streak_days || a.user_id - b.user_id);
        });
        showAlert(e instanceof Error ? e.message : "Network error");
      } finally {
        setFriendBusyId(null);
      }
    },
    [inTelegram, initData, friendBusyId, showAlert],
  );

  // Подписки нужны и списку «Слежу за», и тумблеру «Тренировки друзей» в
  // уведомлениях — подгружаем при открытии вкладки профиля.
  useEffect(() => {
    if (active) void loadFriends();
  }, [active, loadFriends]);

  const followingList = friends;

  // Глобальный тумблер «Тренировки друзей»: одним запросом включает/выключает
  // DM по всем подпискам. Оптимистично, с откатом при ошибке.
  const saveFriendNotifyAll = useCallback(
    async (enabled: boolean) => {
      if (!api || !inTelegram || !initData?.trim() || friendNotifyBusy) return;
      const prev = friends;
      setFriends((list) => list.map((m) => ({ ...m, notify_workouts: enabled })));
      setFriendNotifyBusy(true);
      try {
        const res = await fetch(`${api}/api/miniapp/friends/notify-all`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ init_data: initData, enabled }),
        });
        const j = (await res.json().catch(() => ({}))) as { ok?: boolean; error?: string };
        if (!res.ok || !j.ok) {
          setFriends(prev);
          showAlert(j.error ?? `Friend workouts: error ${res.status}`);
        }
      } catch (e) {
        setFriends(prev);
        showAlert(e instanceof Error ? e.message : "Network error");
      } finally {
        setFriendNotifyBusy(false);
      }
    },
    [inTelegram, initData, friendNotifyBusy, friends, showAlert],
  );

  const loadHealth = useCallback(async () => {
    if (!api || !inTelegram || !initData?.trim()) {
      setOnSick(false);
      return;
    }
    try {
      const res = await fetch(`${api}/api/miniapp/health/status`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ init_data: initData }),
      });
      const j = (await res.json().catch(() => ({}))) as { ok?: boolean; on_sick?: boolean };
      if (res.ok && j.ok) {
        setOnSick(Boolean(j.on_sick));
      }
    } catch {
      // тихо: не критично, ниже UI просто не блокируется
    }
  }, [inTelegram, initData]);

  useEffect(() => {
    if (!active) return;
    void loadHealth();
  }, [loadHealth, active]);

  const sendHealthMessage = useCallback(
    async (text: string) => {
      if (!api || !inTelegram || !initData?.trim()) {
        showAlert("Open the miniapp from Telegram (initData required).");
        return false;
      }
      try {
        const res = await fetch(`${api}/api/miniapp/messages`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ init_data: initData, text }),
        });
        const j = (await res.json().catch(() => ({}))) as { ok?: boolean; error?: string };
        if (!res.ok || !j.ok) {
          showAlert(j.error ?? `Error ${res.status}`);
          return false;
        }
        return true;
      } catch (e) {
        showAlert(e instanceof Error ? e.message : "Network error");
        return false;
      }
    },
    [inTelegram, initData, showAlert],
  );

  const submitSickLeave = useCallback(async () => {
    const reason = sickReason.trim();
    if (reason.length < 3) {
      showAlert("Describe what happened — a few words minimum.");
      return;
    }
    setHealthBusy(true);
    const ok = await sendHealthMessage(`#sick_leave ${reason}`);
    setHealthBusy(false);
    if (ok) {
      void window.Telegram?.WebApp?.HapticFeedback?.notificationOccurred?.("success");
      setSickFormOpen(false);
      setSickReason("");
      setOnSick(true);
      showAlert("Request sent. Leo's reply — in the Chat tab.");
      setTimeout(() => {
        void loadHealth();
        void onRefreshStats?.();
      }, 1200);
    }
  }, [sickReason, sendHealthMessage, loadHealth, onRefreshStats, showAlert]);

  const submitHealthy = useCallback(async () => {
    setHealthBusy(true);
    const ok = await sendHealthMessage("#healthy");
    setHealthBusy(false);
    if (ok) {
      void window.Telegram?.WebApp?.HapticFeedback?.notificationOccurred?.("success");
      setOnSick(false);
      showAlert("Get well soon");
      setTimeout(() => {
        void loadHealth();
        void load();
        void onRefreshStats?.();
      }, 1200);
    }
  }, [sendHealthMessage, loadHealth, load, onRefreshStats, showAlert]);

  const useSaveStreak = useCallback(async () => {
    if (!api || !inTelegram || !initData?.trim()) {
      showAlert("Open the miniapp from Telegram (initData required).");
      return;
    }
    if (!canUseStreakSave(daysSinceLastTraining, saveStreakAvail) || saveStreakBusy) return;
    setSaveStreakBusy(true);
    try {
      const res = await fetch(`${api}/api/miniapp/streak/save-use`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ init_data: initData }),
      });
      const j = (await res.json().catch(() => ({}))) as {
        ok?: boolean;
        error?: string;
        streak_days?: number;
        streak_save_attempts_used?: number;
        streak_save_attempts_max?: number;
      };
      if (typeof j.streak_save_attempts_used === "number") {
        setSaveStreakUsed(Math.max(0, j.streak_save_attempts_used));
      }
      if (typeof j.streak_save_attempts_max === "number") {
        setSaveStreakMax(Math.max(1, j.streak_save_attempts_max));
      }
      if (!res.ok || !j.ok) {
        if (j.error === "no_attempts") {
          showAlert("No saves left — level up to earn more.");
        } else if (j.error === "not_needed") {
          showAlert("Streak still alive — save not needed.");
        } else if (j.error === "too_late") {
          showAlert("More than one day missed — one save can't fix that.");
        } else if (j.error === "nothing_to_save") {
          showAlert("Nothing to restore — no streak yet.");
        } else {
          showAlert(j.error ?? `Error ${res.status}`);
        }
        return;
      }
      void window.Telegram?.WebApp?.HapticFeedback?.notificationOccurred?.("success");
      onStreakSaved?.();
      showAlert(
        typeof j.streak_days === "number" && j.streak_days > 0
          ? `Streak restored: ${j.streak_days} ${j.streak_days === 1 ? "day" : "days"}.`
          : "Save used — streak protected.",
      );
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Network error");
    } finally {
      setSaveStreakBusy(false);
    }
  }, [inTelegram, initData, daysSinceLastTraining, saveStreakAvail, saveStreakBusy, showAlert, onStreakSaved]);

  const saveProfile = useCallback(async () => {
    if (!api || !inTelegram || !initData?.trim()) {
      showAlert("Open the miniapp from Telegram (initData required).");
      return;
    }
    setProfileSaving(true);
    try {
      // Пояс берём с устройства, а не из загруженного ранее профиля: иначе сохранение имени
      // после переезда затирало бы автоопределённый пояс старым (напоминания уезжали на часы).
      const normalizedProfile = normalizeProfileData({
        ...profile,
        timezoneOffset: detectTimezoneInfo().offsetFromMoscow,
      });
      const body: Record<string, unknown> = {
        init_data: initData,
        gender: normalizedProfile.gender,
        display_name: normalizedProfile.displayName,
        timezone_offset: normalizedProfile.timezoneOffset,
        timezone_name: detectTimezoneInfo().ianaName,
      };
      const res = await fetch(`${api}/api/miniapp/profile/save`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
      const j = (await res.json().catch(() => ({}))) as { ok?: boolean; error?: string };
      if (!res.ok) {
        showAlert(j.error ?? `Save: error ${res.status}`);
        return;
      }
      if (j.ok) {
        setProfile(normalizedProfile);
        setSavedProfile(normalizedProfile);
        void window.Telegram?.WebApp?.HapticFeedback?.notificationOccurred?.("success");
        onProfileSaved?.(normalizedProfile.displayName);
        showAlert("Saved");
      }
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Network error");
    } finally {
      setProfileSaving(false);
    }
  }, [inTelegram, initData, onProfileSaved, profile, showAlert]);

  const profileDirty = (() => {
    const current = normalizeProfileData(profile);
    return (
      current.gender !== savedProfile.gender ||
      current.displayName !== savedProfile.displayName ||
      current.timezoneOffset !== savedProfile.timezoneOffset
    );
  })();

  const removalRemaining = inactivityRemovalAt ? removalRemainingUntil(inactivityRemovalAt, now) : null;
  const timerInactiveDays =
    removalRemaining != null ? inactiveDaysFromRemovalRemaining(removalRemaining.ms) : null;
  const hasTimerSync = Boolean(inactivityRemovalAt && removalRemaining);
  const daysForKickUi = hasTimerSync
    ? (timerInactiveDays ?? 0)
    : daysSinceLastTraining;
  const inactiveHighlight = onSick ? "none" : inactivityHighlight(daysForKickUi);
  const showDaysWithoutTraining = !onSick && daysForKickUi >= 5;
  const displayedInactiveDays = hasTimerSync && timerInactiveDays != null ? timerInactiveDays : daysSinceLastTraining;
  const showKickBanner = !onSick && daysForKickUi >= 7;
  const streakSaveBlocked = !canUseStreakSave(daysSinceLastTraining, saveStreakAvail);

  const healthActionsKeyboard = sickFormOpen && healthInputFocused;
  const healthCollapsedSummary =
    onSick === true ? " · on sick leave" : onSick === false && sickFormOpen ? " · request" : "";

  const toggleHealthOpen = useCallback(() => {
    setHealthOpen((open) => {
      if (open) {
        setSickFormOpen(false);
        setSickReason("");
        setHealthInputFocused(false);
      }
      return !open;
    });
  }, []);

  useEffect(() => {
    if (!healthInputFocused || !sickFormOpen) return;
    const run = () => scrollHealthAboveKeyboard();
    run();
    const t1 = window.setTimeout(run, 100);
    const t2 = window.setTimeout(run, 280);
    const vv = window.visualViewport;
    // Только resize (показ/скрытие клавиатуры). На scroll НЕ подписываемся: он срабатывает
    // во время самой прокрутки и при ручном скролле пользователя → петля «уезжания» вверх.
    vv?.addEventListener("resize", run);
    return () => {
      window.clearTimeout(t1);
      window.clearTimeout(t2);
      vv?.removeEventListener("resize", run);
    };
  }, [healthInputFocused, sickFormOpen, scrollHealthAboveKeyboard]);

  return (
    <div
      className={`profile${healthActionsKeyboard ? " profile--health-keyboard" : ""}${
        inactiveHighlight !== "none" ? ` profile--inactive-${inactiveHighlight}` : ""
      }`}
    >
      <header className="profile__hero">
        <div
          className={`profile__avatar${
            inactiveHighlight !== "none" ? ` profile__avatar--inactive-${inactiveHighlight}` : ""
          }`}
          aria-hidden
        >
          {userPhotoUrl ? (
            <img src={userPhotoUrl} alt="" className="profile__avatar-img" width={88} height={88} referrerPolicy="no-referrer" />
          ) : (
            "🐆"
          )}
          {onSick ? <span className="profile__avatar-med">🌡️</span> : null}
        </div>
        <div>
          <h1 className="profile__name">{(profile.displayName || name).trim() || "Pack member"}</h1>
          <LevelCarousel currentLevel={level} />
          {showDaysWithoutTraining ? (
            <p
              className={`profile__kick profile__kick--inactive-${inactiveHighlight}`}
              title={`Days without workouts: ${displayedInactiveDays}`}
            >
              Days without workouts: {displayedInactiveDays}
            </p>
          ) : null}
          {showKickBanner ? (
            <p className="profile__danger">WORK OUT BEFORE MIDNIGHT OR LEO WILL EAT YOU</p>
          ) : null}
        </div>
        {onSick && removalRemaining ? (
          <p className="profile__sick-timer muted">
            After sick leave ends, until removal: {removalRemaining.text}
          </p>
        ) : null}
        <button
          type="button"
          className="profile__xp"
          aria-label={`Cups: ${cupProgressLabel}, level ${level}. Earning history`}
          onClick={() => setCupsHistoryOpen(true)}
        >
          <div className="profile__xp-meter">
            <span className="profile__xp-caption" aria-hidden>
              🏆
            </span>
            <div className="profile__xp-bar">
              <div className="profile__xp-fill" style={{ width: `${barPct}%` }} />
            </div>
            <svg className="profile__xp-chevron" viewBox="0 0 8 14" width="8" height="14" aria-hidden focusable="false">
              <path d="M1.5 1.5 6.5 7l-5 5.5" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
          </div>
          <span className="profile__xp-txt">{cupProgressLabel}</span>
        </button>
      </header>

      <div className="profile__grid3">
        <div className="profile__stat-slot">
          <div
            className={`stat-card${
              inactiveHighlight !== "none" ? ` stat-card--inactive-${inactiveHighlight}` : ""
            }`}
          >
            <div className="stat-card__label">Current streak</div>
            <div className="stat-card__val">{displayStreak}</div>
          </div>
          {burnLabel ? (
            <div className="profile__burn" aria-label={burnLabel}>
              <svg className="profile__burn-icon" viewBox="0 0 16 16" aria-hidden focusable="false">
                <circle cx="8" cy="8" r="6.25" fill="none" stroke="currentColor" strokeWidth="1.5" />
                <path
                  d="M8 4.75V8l2.25 1.75"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="1.5"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                />
              </svg>
              <span className="profile__burn-text">{burnLabel}</span>
            </div>
          ) : null}
        </div>
        <div
          className={`stat-card${
            inactiveHighlight !== "none" ? ` stat-card--inactive-${inactiveHighlight}` : ""
          }`}
        >
          <div className="stat-card__label">Streak record</div>
          <div className="stat-card__val">{recordStreak}</div>
        </div>
        <div
          className={`stat-card${
            inactiveHighlight !== "none" ? ` stat-card--inactive-${inactiveHighlight}` : ""
          }`}
        >
          <div className="stat-card__label">Cup record</div>
          <div className="stat-card__val">{recordCups}</div>
        </div>
        <div
          className={`stat-card${
            inactiveHighlight !== "none" ? ` stat-card--inactive-${inactiveHighlight}` : ""
          }`}
        >
          <div className="stat-card__label">Total workouts</div>
          <div className="stat-card__val">{workouts}</div>
        </div>
      </div>

      <section className="profile__achievements" aria-label="Achievements">
        <div className="profile__achievements-group">
          <span>Streak achievements</span>
          <span className="profile__achievements-count">
            {achievementCount}/{achievementsMax}
          </span>
        </div>
        <div className="profile__achievements-strip">
        {STREAK_ACHIEVEMENTS.map(({ days, colorClass, variant }, i) => (
          <div key={days} className={`profile__achievement ${colorClass}${i < achievementCount ? " is-earned" : ""}`}>
            <div className="profile__achievement-badge" aria-hidden>
              {variant === "heart" ? (
                <svg className="profile__achievement-paw profile__achievement-heart" viewBox="0 0 64 64">
                  <circle className="profile__achievement-bg" cx="32" cy="32" r="28" />
                  <path
                    className="profile__achievement-heart-shell"
                    d="M32 56 C18 46 8 38 8 26 C8 18 14 12 22 12 C26 12 30 14 32 18 C34 14 38 12 42 12 C50 12 56 18 56 26 C56 38 46 46 32 56 Z"
                  />
                  <path
                    className="profile__achievement-heart-bean"
                    d="M32 49 C22 41 14 35 14 26 C14 21 18 17 23 17 C27 17 30 19 32 23 C34 19 37 17 41 17 C46 17 50 21 50 26 C50 35 42 41 32 49 Z"
                  />
                </svg>
              ) : variant === "paw-crown" ? (
                <svg className="profile__achievement-paw profile__achievement-paw--crown" viewBox="0 0 64 64">
                  <path
                    className="profile__achievement-crown"
                    d="M11 24 L14.5 11 L18.5 19.5 L26.5 8.5 L32 15.5 L37.5 8.5 L45.5 19.5 L49.5 11 L53 24 Z"
                  />
                  <rect className="profile__achievement-crown-band" x="11" y="21.5" width="42" height="5.5" rx="1.4" />
                  <circle className="profile__achievement-crown-gem" cx="14.5" cy="12" r="1.5" />
                  <circle className="profile__achievement-crown-gem" cx="26.5" cy="9.5" r="1.5" />
                  <circle className="profile__achievement-crown-gem profile__achievement-crown-gem--main" cx="32" cy="8" r="2" />
                  <circle className="profile__achievement-crown-gem" cx="37.5" cy="9.5" r="1.5" />
                  <circle className="profile__achievement-crown-gem" cx="49.5" cy="12" r="1.5" />
                  <path
                    className="profile__achievement-pad-shell"
                    d="M32 34 C22 34 16 41 16 50 C16 57 22 61 32 61 C42 61 48 57 48 50 C48 41 42 34 32 34 Z"
                  />
                  <path
                    className="profile__achievement-pad-bean"
                    d="M32 39 C25 39 21 44 21 50 C21 55 25 58 32 58 C39 58 43 55 43 50 C43 44 39 39 32 39 Z"
                  />
                </svg>
              ) : (
                <svg className="profile__achievement-paw" viewBox="0 0 64 64">
                  <circle className="profile__achievement-bg" cx="32" cy="32" r="28" />
                  <path className="profile__achievement-claw" d="M15 14 L18 6 L21 14 Z" />
                  <path className="profile__achievement-claw" d="M27 10 L30 2 L33 10 Z" />
                  <path className="profile__achievement-claw" d="M39 10 L42 2 L45 10 Z" />
                  <path className="profile__achievement-claw" d="M51 14 L54 6 L57 14 Z" />

                  <ellipse className="profile__achievement-toe-shell" cx="15.5" cy="26" rx="8" ry="9.5" transform="rotate(-16 15.5 26)" />
                  <ellipse className="profile__achievement-toe-shell" cx="27.5" cy="19.5" rx="8.6" ry="10.5" transform="rotate(-6 27.5 19.5)" />
                  <ellipse className="profile__achievement-toe-shell" cx="40.5" cy="19.5" rx="8.6" ry="10.5" transform="rotate(6 40.5 19.5)" />
                  <ellipse className="profile__achievement-toe-shell" cx="52.5" cy="26" rx="8" ry="9.5" transform="rotate(16 52.5 26)" />

                  <ellipse className="profile__achievement-toe-bean" cx="15.5" cy="27" rx="4.7" ry="5.9" transform="rotate(-16 15.5 27)" />
                  <ellipse className="profile__achievement-toe-bean" cx="27.5" cy="20.5" rx="4.9" ry="6.3" transform="rotate(-6 27.5 20.5)" />
                  <ellipse className="profile__achievement-toe-bean" cx="40.5" cy="20.5" rx="4.9" ry="6.3" transform="rotate(6 40.5 20.5)" />
                  <ellipse className="profile__achievement-toe-bean" cx="52.5" cy="27" rx="4.7" ry="5.9" transform="rotate(16 52.5 27)" />

                  <path
                    className="profile__achievement-pad-shell"
                    d="M32 34 C22 34 16 41 16 50 C16 57 22 61 32 61 C42 61 48 57 48 50 C48 41 42 34 32 34 Z"
                  />
                  <path
                    className="profile__achievement-pad-bean"
                    d="M32 39 C25 39 21 44 21 50 C21 55 25 58 32 58 C39 58 43 55 43 50 C43 44 39 39 32 39 Z"
                  />
                </svg>
              )}
              <span
                className={`profile__achievement-days${days >= 100 ? " profile__achievement-days--triple" : days >= 10 ? " profile__achievement-days--double" : ""}${variant === "heart" ? " profile__achievement-days--heart" : ""}`}
              >
                {days}
              </span>
            </div>
            <div className="profile__achievement-label">{days}-day streak</div>
          </div>
        ))}
        </div>
        <div className="profile__achievements-group">
          <span>Workout achievements</span>
          <span className="profile__achievements-count">
            {workoutAchEarned}/{WORKOUT_ACHIEVEMENTS.length}
          </span>
        </div>
        <div className="profile__achievements-strip">
        {WORKOUT_ACHIEVEMENTS.map(({ count, variant }, i) => (
          <div
            key={count}
            className={`profile__achievement profile__wach profile__wach--t${i + 1}${i < workoutAchEarned ? " is-earned" : ""}`}
          >
            <div className="profile__achievement-badge" aria-hidden>
              <svg className="profile__achievement-star-svg" viewBox="0 0 64 64">
                <circle className="profile__achievement-bg" cx="32" cy="32" r="28" />
                {/* пятиконечная звезда — корпус ачивки */}
                <path
                  className="profile__achievement-star"
                  d="M32 2 L40.23 20.67 L60.53 22.73 L45.32 36.33 L49.63 56.27 L32 46 L14.37 56.27 L18.68 36.33 L3.47 22.73 L23.77 20.67 Z"
                />
                {variant === "heart" ? (
                  /* маленькое сердце внутри звезды (как у стрика на 42) */
                  <g className="profile__achievement-miniheart" transform="translate(32 31) scale(0.46) translate(-32 -34)">
                    <path
                      className="profile__achievement-heart-shell"
                      d="M32 56 C18 46 8 38 8 26 C8 18 14 12 22 12 C26 12 30 14 32 18 C34 14 38 12 42 12 C50 12 56 18 56 26 C56 38 46 46 32 56 Z"
                    />
                    <path
                      className="profile__achievement-heart-bean"
                      d="M32 49 C22 41 14 35 14 26 C14 21 18 17 23 17 C27 17 30 19 32 23 C34 19 37 17 41 17 C46 17 50 21 50 26 C50 35 42 41 32 49 Z"
                    />
                  </g>
                ) : variant === "leo" ? (
                  /* медитирующий Лео в позе лотоса — 1000 тренировок */
                  <g className="profile__achievement-leo" transform="translate(32 34) scale(0.6) translate(-32 -34)">
                    {/* основание/тело в позе лотоса */}
                    <path
                      className="profile__achievement-pad-shell"
                      d="M13 53 C13 43 21 38 32 38 C43 38 51 43 51 53 C51 57 47 59 41 59 L23 59 C17 59 13 57 13 53 Z"
                    />
                    {/* лапки сложены по центру (мудра) */}
                    <ellipse className="profile__achievement-toe-bean" cx="26" cy="53" rx="5" ry="3.8" />
                    <ellipse className="profile__achievement-toe-bean" cx="38" cy="53" rx="5" ry="3.8" />
                    {/* уши */}
                    <path className="profile__achievement-pad-shell" d="M20 16 L26 5 L31 18 Z" />
                    <path className="profile__achievement-pad-shell" d="M44 16 L38 5 L33 18 Z" />
                    <path className="profile__achievement-toe-bean" d="M23.5 14 L26 8 L28.5 15 Z" />
                    <path className="profile__achievement-toe-bean" d="M40.5 14 L38 8 L35.5 15 Z" />
                    {/* голова */}
                    <circle className="profile__achievement-pad-shell" cx="32" cy="24" r="12" />
                    {/* закрытые глаза (умиротворение) и улыбка */}
                    <path className="profile__achievement-leo-line" d="M24 24 Q27 27 30 24" />
                    <path className="profile__achievement-leo-line" d="M34 24 Q37 27 40 24" />
                    <path className="profile__achievement-leo-line" d="M29 30 Q32 32.5 35 30" />
                  </g>
                ) : (
                  /* маленькая лапка внутри звезды — число «на подушечке» */
                  <g className="profile__achievement-minipaw" transform="translate(32 31.5) scale(0.5) translate(-32 -34)">
                    <ellipse className="profile__achievement-toe-shell" cx="15.5" cy="26" rx="8" ry="9.5" transform="rotate(-16 15.5 26)" />
                    <ellipse className="profile__achievement-toe-shell" cx="27.5" cy="19.5" rx="8.6" ry="10.5" transform="rotate(-6 27.5 19.5)" />
                    <ellipse className="profile__achievement-toe-shell" cx="40.5" cy="19.5" rx="8.6" ry="10.5" transform="rotate(6 40.5 19.5)" />
                    <ellipse className="profile__achievement-toe-shell" cx="52.5" cy="26" rx="8" ry="9.5" transform="rotate(16 52.5 26)" />

                    <ellipse className="profile__achievement-toe-bean" cx="15.5" cy="27" rx="4.7" ry="5.9" transform="rotate(-16 15.5 27)" />
                    <ellipse className="profile__achievement-toe-bean" cx="27.5" cy="20.5" rx="4.9" ry="6.3" transform="rotate(-6 27.5 20.5)" />
                    <ellipse className="profile__achievement-toe-bean" cx="40.5" cy="20.5" rx="4.9" ry="6.3" transform="rotate(6 40.5 20.5)" />
                    <ellipse className="profile__achievement-toe-bean" cx="52.5" cy="27" rx="4.7" ry="5.9" transform="rotate(16 52.5 27)" />

                    <path
                      className="profile__achievement-pad-shell"
                      d="M32 34 C22 34 16 41 16 50 C16 57 22 61 32 61 C42 61 48 57 48 50 C48 41 42 34 32 34 Z"
                    />
                    <path
                      className="profile__achievement-pad-bean"
                      d="M32 39 C25 39 21 44 21 50 C21 55 25 58 32 58 C39 58 43 55 43 50 C43 44 39 39 32 39 Z"
                    />
                  </g>
                )}
                {variant === "crown" && (
                  /* корона поверх звезды — 420 тренировок */
                  <g className="profile__achievement-wcrown" transform="translate(32 7) scale(0.52) translate(-32 -17)">
                    <path
                      className="profile__achievement-crown"
                      d="M11 24 L14.5 11 L18.5 19.5 L26.5 8.5 L32 15.5 L37.5 8.5 L45.5 19.5 L49.5 11 L53 24 Z"
                    />
                    <rect className="profile__achievement-crown-band" x="11" y="21.5" width="42" height="5.5" rx="1.4" />
                    <circle className="profile__achievement-crown-gem profile__achievement-crown-gem--main" cx="32" cy="8" r="2" />
                  </g>
                )}
              </svg>
              {variant !== "leo" && (
                <span
                  className={`profile__achievement-num${variant === "heart" ? " profile__achievement-num--heart" : ""}${count >= 1000 ? " profile__achievement-num--quad" : count >= 100 ? " profile__achievement-num--triple" : count >= 10 ? " profile__achievement-num--double" : ""}`}
                >
                  {count}
                </span>
              )}
            </div>
            <div className="profile__achievement-label">{count} {workoutsWordRu(count)}</div>
          </div>
        ))}
        </div>
      </section>

      <div className="profile__streak-save">
        <button
          type="button"
          className="profile__save profile__streak-save-btn"
          onClick={() => void useSaveStreak()}
          disabled={saveStreakBusy || streakSaveBlocked}
        >
          {saveStreakBusy
            ? "Sending…"
            : `Streak saves left ${saveStreakAvail}/${saveStreakMax}`}
        </button>
      </div>
      <p className="profile__hint muted profile__streak-save-hint">
        Restore a lost streak if you forgot to log a workout
      </p>

      {onSick === null ? (
        <section className="profile__health-section">
          <button
            type="button"
            className={`section-title profile__health-title profile__notif-toggle${healthOpen ? " is-open" : ""}`}
            aria-expanded={healthOpen}
            onClick={toggleHealthOpen}
          >
            Health
            <span className="profile__notif-chevron" aria-hidden>
              {healthOpen ? "▲" : "▼"}
            </span>
          </button>
          {healthOpen && <p className="profile__hint muted">Loading status…</p>}
        </section>
      ) : (
        <section className="profile__health-section">
          <button
            type="button"
            className={`section-title profile__health-title profile__notif-toggle${healthOpen ? " is-open" : ""}`}
            aria-expanded={healthOpen}
            onClick={toggleHealthOpen}
          >
            Health{!healthOpen ? healthCollapsedSummary : ""}
            <span className="profile__notif-chevron" aria-hidden>
              {healthOpen ? "▲" : "▼"}
            </span>
          </button>
          {healthOpen && (onSick ? (
            <div className="profile__health">
              <p className="profile__hint">🏥 You’re on sick leave — timer paused — come back when you’re better</p>
              <button
                type="button"
                className="profile__save profile__health-btn"
                onClick={() => void submitHealthy()}
                disabled={healthBusy}
              >
                {healthBusy ? "Sending…" : "End sick leave"}
              </button>
            </div>
          ) : sickFormOpen ? (
            <div className="profile__health">
              <p className="profile__hint muted">Describe what happened — Leo will decide on sick leave</p>
              <textarea
                ref={healthTextareaRef}
                className="profile__input profile__health-textarea"
                value={sickReason}
                onChange={(e) => setSickReason(e.target.value.slice(0, 500))}
                placeholder="e.g. fever, cough"
                rows={3}
                maxLength={500}
                disabled={healthBusy}
                onFocus={() => {
                  setHealthInputFocused(true);
                  window.setTimeout(() => scrollHealthAboveKeyboard(), 80);
                }}
                onBlur={() => {
                  window.setTimeout(() => {
                    const el = document.activeElement;
                    if (el instanceof HTMLElement && el.closest(".profile__health-actions")) return;
                    setHealthInputFocused(false);
                  }, 80);
                }}
              />
              {healthActionsKeyboard ? <div className="profile__health-actions-spacer" aria-hidden /> : null}
              <div
                className={`profile__health-actions${healthActionsKeyboard ? " profile__health-actions--keyboard" : ""}`}
              >
                <button
                  type="button"
                  className="profile__save profile__health-btn"
                  onClick={() => void submitSickLeave()}
                  disabled={healthBusy || sickReason.trim().length < 3}
                >
                  {healthBusy ? "Sending…" : "Submit request"}
                </button>
                <button
                  type="button"
                  className="profile__health-cancel"
                  onClick={() => {
                    setSickFormOpen(false);
                    setSickReason("");
                    setHealthInputFocused(false);
                  }}
                  disabled={healthBusy}
                >
                  Cancel
                </button>
              </div>
            </div>
          ) : (
            <div className="profile__health">
              <div className="profile__health-actions">
                <button
                  type="button"
                  className="profile__save profile__health-btn"
                  onClick={() => setSickFormOpen(true)}
                  disabled={healthBusy}
                >
                  Take sick leave
                </button>
              </div>
              <p className="profile__hint muted profile__health-hint">If you’re sick — timer pauses until you recover</p>
            </div>
          ))}
        </section>
      )}

      <section className="profile__notif">
        <button
          type="button"
          className={`section-title profile__notif-title profile__notif-toggle${notifOpen ? " is-open" : ""}`}
          aria-expanded={notifOpen}
          onClick={() => setNotifOpen((open) => !open)}
        >
          🔔 Notifications
          <span className="profile__notif-chevron" aria-hidden>
            {notifOpen ? "▲" : "▼"}
          </span>
        </button>
        {notifOpen && (
        <>
        <div className="profile__reminder">
          <label className="profile__reminder-row">
          <span className="profile__reminder-label">Remind me to log workouts</span>
          <input
            type="checkbox"
            className="profile__reminder-toggle"
            checked={reminderEnabled}
            disabled={reminderLoading || reminderBusy}
            onChange={(e) => void saveReminder(e.target.checked, reminderHour)}
          />
        </label>
        {reminderEnabled && (
          <label className="profile__field">
            <span>Reminder time (your local time)</span>
            <select
              className="profile__input"
              value={reminderHour}
              disabled={reminderLoading || reminderBusy}
              onChange={(e) => void saveReminder(true, Number(e.target.value))}
            >
              {Array.from({ length: 24 }, (_, h) => (
                <option key={h} value={h}>
                  {String(h).padStart(2, "0")}:00
                </option>
              ))}
            </select>
          </label>
        )}
        <p className="profile__hint muted">
          {reminderEnabled
            ? "If no workout is logged by this hour — Leo will nudge you gently"
            : "Reminders off — Leo won't message about missed workouts"}
        </p>

        <label className="profile__reminder-row">
          <span className="profile__reminder-label">Daily wisdom in DM</span>
          <input
            type="checkbox"
            className="profile__reminder-toggle"
            checked={wisdomEnabled}
            disabled={wisdomLoading || wisdomBusy}
            onChange={(e) => void saveWisdomSub(e.target.checked, wisdomHour)}
          />
        </label>
        <p className="profile__hint muted">
          {wisdomEnabled
            ? "Every day at 4:20 your time Leo sends daily wisdom in DM"
            : "Subscription off — daily wisdom only in pack chat"}
        </p>

        <label className="profile__reminder-row">
          <span className="profile__reminder-label">Likes on posts and in feed</span>
          <input
            type="checkbox"
            className="profile__reminder-toggle"
            checked={likeNotifyEnabled}
            disabled={likeNotifyLoading || likeNotifyBusy}
            onChange={(e) => void saveLikeNotify(e.target.checked)}
          />
        </label>
        <p className="profile__hint muted">
          {likeNotifyEnabled
            ? "Leo DMs you when someone likes your workout or comment"
            : "Notifications off — no DMs about likes on your posts"}
        </p>

        <label className="profile__reminder-row">
          <span className="profile__reminder-label">Contacts joined the pack</span>
          <input
            type="checkbox"
            className="profile__reminder-toggle"
            checked={contactJoinEnabled}
            disabled={contactJoinLoading || contactJoinBusy}
            onChange={(e) => void saveContactJoin(e.target.checked)}
          />
        </label>
        <p className="profile__hint muted">
          {!contactJoinEnabled
            ? "Notifications off — no DMs when contacts join the pack"
            : contactJoinCount > 0
              ? `Leo will DM when a saved contact joins the pack (${contactJoinCount} saved). Add more — send the bot a contact via attach → Contact`
              : "Send the bot a contact (attach → Contact) — Leo will DM when they join"}
        </p>

        <label className="profile__reminder-row">
          <span className="profile__reminder-label">Friends’ workouts</span>
          <input
            type="checkbox"
            className="profile__reminder-toggle"
            checked={friendNotifyEnabled}
            disabled={friendsLoading || friendNotifyBusy || friends.length === 0}
            onChange={(e) => void saveFriendNotifyAll(e.target.checked)}
          />
        </label>
        <p className="profile__hint muted">
          {friends.length === 0
            ? "Follow people in the feed — Leo can DM about their workouts"
            : friendNotifyEnabled
              ? "Leo DMs when someone you follow logs a workout"
              : "Notifications off — no friend workout DMs"}
        </p>

        <button
          type="button"
          className={`profile__friends-toggle${followingOpen ? " is-active" : ""}`}
          aria-expanded={followingOpen}
          onClick={() => setFollowingOpen((open) => !open)}
        >
          Following{followingList.length > 0 ? ` · ${followingList.length}` : ""} {followingOpen ? "▲" : "▼"}
        </button>
        {followingOpen && (
          friendsLoading && friends.length === 0 ? (
            <p className="muted">Loading…</p>
          ) : followingList.length === 0 ? (
            <p className="profile__hint muted">
              You’re not following anyone yet — find a post in the feed and tap Follow workouts
            </p>
          ) : (
            <ul className="profile__friends-list">
              {followingList.map((m) => (
                <li key={m.user_id} className="profile__friend-row">
                  <span className="profile__friend-info">
                    <span className="profile__friend-name">{m.name}</span>
                    {m.streak_days > 0 && (
                      <span className="profile__friend-streak muted">🔥 {m.streak_days}</span>
                    )}
                  </span>
                  <button
                    type="button"
                    className="profile__friend-unfollow"
                    disabled={friendBusyId === m.user_id}
                    onClick={() => void toggleFollow(m)}
                  >
                    {friendBusyId === m.user_id ? "…" : "Unfollow"}
                  </button>
                </li>
              ))}
            </ul>
          )
        )}
        </div>
        </>
        )}
      </section>

      <section className="profile__personal">
        <button
          type="button"
          className={`section-title profile__personal-title profile__notif-toggle${personalOpen ? " is-open" : ""}`}
          aria-expanded={personalOpen}
          onClick={() => setPersonalOpen((open) => !open)}
        >
          Personal details
          <span className="profile__notif-chevron" aria-hidden>
            {personalOpen ? "▲" : "▼"}
          </span>
        </button>
        {personalOpen && (
          <>
            {profileLoading && <p className="muted">Loading profile…</p>}
            <div className="profile__form">
              <label className="profile__field">
                <span>Name (what we call you)</span>
                <input
                  type="text"
                  className="profile__input"
                  value={profile.displayName}
                  onChange={(e) => setProfile((p) => ({ ...p, displayName: e.target.value.slice(0, 64) }))}
                  placeholder="e.g. Alex"
                  maxLength={64}
                  autoComplete="name"
                  disabled={profileLoading}
                />
              </label>
              <label className="profile__field">
                <span>Gender</span>
                <select
                  className="profile__input"
                  value={profile.gender}
                  onChange={(e) => setProfile((p) => ({ ...p, gender: e.target.value as "m" | "f" | "" }))}
                  disabled={profileLoading}
                >
                  <option value="">Prefer not to say</option>
                  <option value="m">Male</option>
                  <option value="f">Female</option>
                </select>
              </label>
              {(profileDirty || profileSaving) && (
                <button
                  type="button"
                  className="profile__save"
                  onClick={() => void saveProfile()}
                  disabled={profileLoading || profileSaving || !profileDirty}
                >
                  {profileSaving ? "Saving…" : "Save"}
                </button>
              )}
            </div>
          </>
        )}
      </section>

      <section className="profile__theme">
        <button
          type="button"
          className={`section-title profile__theme-title profile__notif-toggle${themeOpen ? " is-open" : ""}`}
          aria-expanded={themeOpen}
          onClick={() => setThemeOpen((open) => !open)}
        >
          Theme{!themeOpen ? ` · ${THEME_LABELS[theme]}` : ""}
          <span className="profile__notif-chevron" aria-hidden>
            {themeOpen ? "▲" : "▼"}
          </span>
        </button>
        {themeOpen && (
        <>
        <div className="profile__theme-seg" role="group" aria-label="Theme">
          <button
            type="button"
            className={`profile__theme-opt ${theme === "dark" ? "is-active" : ""}`}
            aria-pressed={theme === "dark"}
            onClick={() => changeTheme("dark")}
          >
            {THEME_LABELS.dark}
          </button>
          <button
            type="button"
            className={`profile__theme-opt ${theme === "light" ? "is-active" : ""}`}
            aria-pressed={theme === "light"}
            onClick={() => changeTheme("light")}
          >
            {THEME_LABELS.light}
          </button>
          <button
            type="button"
            className={`profile__theme-opt ${theme === "leopard" ? "is-active" : ""} ${leopardUnlocked ? "" : "is-locked"}`}
            aria-pressed={theme === "leopard"}
            aria-disabled={!leopardUnlocked}
            disabled={!leopardUnlocked}
            title={leopardUnlocked ? "Pink leopard theme" : "From level 5 · Lion"}
            onClick={() => changeTheme("leopard")}
          >
            {THEME_LABELS.leopard}
          </button>
          <button
            type="button"
            className={`profile__theme-opt ${theme === "wild" ? "is-active" : ""} ${wildUnlocked ? "" : "is-locked"}`}
            aria-pressed={theme === "wild"}
            aria-disabled={!wildUnlocked}
            disabled={!wildUnlocked}
            title={wildUnlocked ? "Wild leopard theme" : "365-day streak or 1000 workouts"}
            onClick={() => changeTheme("wild")}
          >
            {THEME_LABELS.wild}
          </button>
          <button
            type="button"
            className={`profile__theme-opt ${theme === "pack" ? "is-active" : ""} ${packUnlocked ? "" : "is-locked"}`}
            aria-pressed={theme === "pack"}
            aria-disabled={!packUnlocked}
            disabled={!packUnlocked}
            title={packUnlocked ? "Pack bonus theme for 24h" : `${packWorkoutsGoal} pack workouts this week`}
            onClick={() => changeTheme("pack")}
          >
            {THEME_LABELS.pack}
          </button>
        </div>
        {!leopardUnlocked ? (
          <p className="profile__theme-lock muted">Pink theme unlocks at level 5 · Lion</p>
        ) : null}
        {!wildUnlocked ? (
          <p className="profile__theme-lock muted">Wild theme — 365-day streak or 1000 workouts</p>
        ) : null}
        {!packUnlocked ? (
          <p className="profile__theme-lock muted">Pack theme — {packWorkoutsGoal} pack workouts this week (resets Mondays 00:00 MSK)</p>
        ) : null}
        </>
        )}
      </section>

      {initData?.trim() ? <InviteFriendsSection initData={initData} /> : null}

      {(donateOptions.starsAvailable || donateOptions.cardAvailable) && (
        <section className="profile__donate">
          <p className="profile__hint muted profile__donate-intro">
            Optional donations don’t stop inactivity removal, but they keep Leo and the project going. You’ve
            supported {donateOptions.completedCount} {donateTimesWordRu(donateOptions.completedCount)}. Thank you!
          </p>
          <button
            type="button"
            className="profile__save profile__donate-btn"
            onClick={() => setDonateModalOpen(true)}
          >
            Support the project
          </button>
        </section>
      )}

      {donateModalOpen ? (
        <div className="profile__donate-modal" role="dialog" aria-modal="true" aria-labelledby="profile-donate-title">
          <button
            type="button"
            className="profile__donate-modal-backdrop"
            aria-label="Close"
            disabled={donateBusy}
            onClick={() => setDonateModalOpen(false)}
          />
          <div className="profile__donate-modal-box">
            <div className="profile__donate-modal-head">
              <h2 id="profile-donate-title" className="section-title profile__donate-modal-title">
                Support the project
              </h2>
              <button
                type="button"
                className="profile__donate-modal-close"
                aria-label="Close"
                disabled={donateBusy}
                onClick={() => setDonateModalOpen(false)}
              >
                ✕
              </button>
            </div>

            {donateOptions.starsAvailable && (
              <div className="profile__donate-method">
                <span className="profile__donate-label">⭐ Telegram Stars — worldwide</span>
                <div className="profile__donate-tiers" role="group" aria-label="Amount in Stars">
                  {donateOptions.starsTiers.map((tier) => (
                    <button
                      key={tier}
                      type="button"
                      className={`profile__donate-tier ${donateStars === tier ? "is-active" : ""}`}
                      aria-pressed={donateStars === tier}
                      disabled={donateBusy}
                      onClick={() => setDonateStars(tier)}
                    >
                      {tier} ⭐
                    </button>
                  ))}
                </div>
                <button
                  type="button"
                  className="profile__save profile__donate-btn"
                  onClick={() => void donateWithStars()}
                  disabled={donateBusy || donateStars === null}
                >
                  {donateBusy ? "Opening invoice…" : `Donate ${donateStars ?? ""} ⭐`}
                </button>
              </div>
            )}

            {donateOptions.cardAvailable && (
              <div className="profile__donate-method">
                <span className="profile__donate-label">💳 Bank card — Russia</span>
                <div className="profile__donate-tiers" role="group" aria-label="Amount in rubles">
                  {donateOptions.cardTiersRub.map((tier) => (
                    <button
                      key={tier}
                      type="button"
                      className={`profile__donate-tier ${donateRub === tier ? "is-active" : ""}`}
                      aria-pressed={donateRub === tier}
                      disabled={donateBusy}
                      onClick={() => setDonateRub(tier)}
                    >
                      {tier} ₽
                    </button>
                  ))}
                </div>
                <button
                  type="button"
                  className="profile__save profile__donate-btn"
                  onClick={() => void donateWithCard()}
                  disabled={donateBusy || donateRub === null}
                >
                  {donateBusy ? "Waiting for payment…" : `Donate ${donateRub ?? ""} ₽`}
                </button>
                <p className="profile__hint muted profile__donate-note">
                  Payment opens in browser — come back here, we’ll wait for confirmation
                </p>
              </div>
            )}
          </div>
        </div>
      ) : null}

      {donateThanks ? (
        <DonateThanksToast onDone={() => setDonateThanks(false)} />
      ) : null}

      {cupsHistoryOpen ? (
        <CupsHistorySheet apiUrl={api} initData={initData} onClose={() => setCupsHistoryOpen(false)} />
      ) : null}

      <div className="profile__support">
        <h2 className="section-title">Support</h2>
        <p className="profile__hint muted">If something broke or you need a human — message here</p>
        <button type="button" className="profile__save profile__support-btn" onClick={onSupport}>
          Report a problem
        </button>
      </div>

      {isAdmin && onAdmin ? (
        <div className="profile__support profile__admin">
          <h2 className="section-title">Admin</h2>
          <p className="profile__hint muted">
            Community, system, data, resources, and task tracker — admins only
          </p>
          <button type="button" className="profile__save profile__support-btn" onClick={onAdmin}>
            Open admin
          </button>
        </div>
      ) : null}

    </div>
  );
}
