import { useCallback, useEffect, useRef, useState } from "react";
import { useAppKeyboardInset } from "./hooks/useAppKeyboardInset";
import { useTelegramWebApp } from "./hooks/useTelegramWebApp";
import { BottomNav } from "./components/BottomNav";
import { ChatScreen } from "./components/ChatScreen";
import { FeedScreen } from "./components/FeedScreen";
import { ProfileScreen } from "./components/ProfileScreen";
import { NewWorkoutScreen } from "./components/NewWorkoutScreen";
import { RulesScreen } from "./components/RulesScreen";
import { TabKeepAlive } from "./components/TabKeepAlive";
import { MiniappRemovedScreen } from "./components/MiniappRemovedScreen";
import { SupportScreen } from "./components/SupportScreen";
import { AdminScreen } from "./components/AdminScreen";
import { AchievementToast } from "./components/AchievementToast";
import { LevelUpToast } from "./components/LevelUpToast";
import { earnedAchievementKeys, freshAchievementKeys, type AchievementKey } from "./lib/achievements";
import { miniappLevelFromCups } from "./lib/miniappLevel";
import { getStoredTheme, hasStoredTheme, hydrateThemeFromCloud, hydrateThemeFromServer, isThemeMode, persistTheme, persistThemeToServer } from "./lib/theme";
import { buildOptimisticTrainingFeedItem, type PackFeedItemDTO } from "./lib/packFeed";
import type { WorkoutCategoryId } from "./lib/workoutCategories";
import { parseSuggestedWorkoutTypes } from "./lib/workoutSuggest";
import { sendMiniappPrivateText, sendMiniappTrainingWithPhoto } from "./lib/miniappPrivateSend";
import { isModerationError, moderationUserMessage } from "./lib/moderationMessages";
import { fetchLeoPendingCount } from "./lib/leoPersonalInbox";
import { fetchFeedThreadUnreadSummary } from "./lib/feedThreadUnread";
import { fetchPackGroupUnreadCount } from "./lib/packGroupUnread";
import { ensureMiniappOnboarding } from "./lib/miniappOnboarding";
import { syncDeviceTimezone } from "./lib/timezoneSync";
import { reportMiniappOpened, reportWorkoutLogStarted, reportNonSportInterest } from "./lib/miniappEvents";
import "./App.css";

type Tab = "chat" | "feed" | "rules" | "profile";
type AccessGateStatus = "checking" | "ok" | "deleted";

function formatTrainingDoneAlert(replyParts: string[]): string {
  const summary = replyParts.filter(Boolean).join("\n\n").trim();
  if (!summary.startsWith("✅ Отчёт принят")) return summary;

  const lines = summary
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);

  const title = lines.find((line) => line.startsWith("✅ Отчёт принят")) ?? "";
  const streakLineRaw = lines.find((line) => /Стрик:|Серия:/i.test(line)) ?? "";
  const cupsLineRaw = lines.find((line) => line.startsWith("🏆")) ?? "";
  const streakLine = streakLineRaw.replace(/^🦁\s*(Стрик|Серия):\s*/i, "Стрик: ");
  const cupsLine = cupsLineRaw.replace(/^🏆\s*/u, "");

  const compact = [title, [streakLine, cupsLine].filter(Boolean).join("\n")].filter(Boolean).join("\n\n").trim();
  return compact || summary;
}

export function App() {
  useAppKeyboardInset();
  const { name, streak: hookStreak, initData, userId, photoUrl, inTelegram, tg } = useTelegramWebApp();
  const showAlert = useCallback((m: string) => {
    if (tg?.showAlert) void tg.showAlert(m);
    else window.alert(m);
  }, [tg]);
  const [tab, setTab] = useState<Tab>("feed");
  const [workoutOpen, setWorkoutOpen] = useState(false);
  const [supportOpen, setSupportOpen] = useState(false);
  const [adminOpen, setAdminOpen] = useState(false);
  const [streak, setStreak] = useState(hookStreak);
  const [recordStreak, setRecordStreak] = useState(hookStreak);
  const [profileDisplayName, setProfileDisplayName] = useState("");
  const [xp, setXP] = useState(0);
  const [achievementCount, setAchievementCount] = useState(0);
  const [achievementsMax, setAchievementsMax] = useState(9);
  const [workouts, setWorkouts] = useState(0);
  const [packDays, setPackDays] = useState(0);
  const [suggestedWorkoutTypes, setSuggestedWorkoutTypes] = useState<WorkoutCategoryId[] | null>(null);
  const [leoPending, setLeoPending] = useState(0);
  const [feedThreadUnread, setFeedThreadUnread] = useState(0);
  const [packGroupUnread, setPackGroupUnread] = useState(0);
  const feedUnread = feedThreadUnread + packGroupUnread;
  const [feedRefreshToken, setFeedRefreshToken] = useState(0);
  const [optimisticFeedItem, setOptimisticFeedItem] = useState<PackFeedItemDTO | null>(null);
  const [daysSinceLastTraining, setDaysSinceLastTraining] = useState<number>(-1);
  const [lastTrainingDate, setLastTrainingDate] = useState("");
  /** RFC3339 — когда возможен кик за неактивность (тот же дедлайн, что в сообщении Лео). */
  const [inactivityRemovalAt, setInactivityRemovalAt] = useState("");
  const [accessGateStatus, setAccessGateStatus] = useState<AccessGateStatus>("checking");
  const [isAdmin, setIsAdmin] = useState(false);
  const [accessPriceRub, setAccessPriceRub] = useState(99);
  const tzSyncedRef = useRef(false);
  // Очередь тостов «Ачивка получена!» — показываем по одному, дедуп по ключам.
  const [achievementQueue, setAchievementQueue] = useState<AchievementKey[]>([]);
  const currentAchievement = achievementQueue[0] ?? null;
  // Очередь поп-апов «Новый уровень!» — по номеру достигнутого уровня.
  const [levelUpQueue, setLevelUpQueue] = useState<number[]>([]);
  const currentLevelUp = levelUpQueue[0] ?? null;

  // §3: miniapp_opened — один раз, как только есть валидный initData в Telegram.
  useEffect(() => {
    if (inTelegram && initData?.trim()) {
      reportMiniappOpened(initData);
    }
  }, [inTelegram, initData]);

  useEffect(() => {
    hydrateThemeFromCloud();
    const flush = () => persistTheme(getStoredTheme());
    const onHide = () => {
      if (document.visibilityState === "hidden") flush();
    };
    window.addEventListener("pagehide", flush);
    document.addEventListener("visibilitychange", onHide);
    return () => {
      window.removeEventListener("pagehide", flush);
      document.removeEventListener("visibilitychange", onHide);
    };
  }, []);

  const refreshAccessStatus = useCallback(async () => {
    if (!inTelegram || !initData?.trim()) {
      setAccessGateStatus("ok");
      return;
    }
    const res = await ensureMiniappOnboarding(initData);
    setAccessGateStatus(res.deleted || res.accessState === "deleted" ? "deleted" : "ok");
  }, [inTelegram, initData]);

  const refreshTabBadges = useCallback(async () => {
    if (accessGateStatus !== "ok" || !inTelegram || !initData?.trim()) {
      setLeoPending(0);
      setFeedThreadUnread(0);
      setPackGroupUnread(0);
      return;
    }
    const [leo, feedThread, packGroup] = await Promise.all([
      fetchLeoPendingCount(initData),
      fetchFeedThreadUnreadSummary(initData),
      fetchPackGroupUnreadCount(initData),
    ]);
    setLeoPending(leo);
    setFeedThreadUnread(feedThread.count);
    setPackGroupUnread(packGroup);
  }, [accessGateStatus, inTelegram, initData]);

  const clearLeoBadge = useCallback(() => setLeoPending(0), []);
  const clearPackGroupBadge = useCallback(() => setPackGroupUnread(0), []);
  const clearFeedThreadBadge = useCallback(() => setFeedThreadUnread(0), []);
  const onOptimisticConsumed = useCallback(() => setOptimisticFeedItem(null), []);

  // Сверяем открытые ачивки с тем, что пользователь уже видел (localStorage по user_id),
  // и ставим новые в очередь тостов. Первый запуск на устройстве — тихая базовая линия,
  // чтобы не сыпать уведомлениями за давно открытые ачивки.
  const notifyNewAchievements = useCallback((uid: number, achCount: number, wk: number) => {
    if (!uid) return;
    const storageKey = `fl_seen_achievements_${uid}`;
    const earned = earnedAchievementKeys(achCount, wk);
    let raw: string | null = null;
    try {
      raw = localStorage.getItem(storageKey);
    } catch {
      return; // приватный режим / нет доступа к storage — не спамим
    }
    if (raw === null) {
      try { localStorage.setItem(storageKey, JSON.stringify(earned)); } catch { /* ignore */ }
      return;
    }
    let seen: string[] = [];
    try {
      const parsed = JSON.parse(raw);
      if (Array.isArray(parsed)) seen = parsed.filter((k): k is string => typeof k === "string");
    } catch {
      seen = [];
    }
    const baseline = JSON.stringify(earned);
    const fresh = freshAchievementKeys(earned, seen);
    if (fresh.length === 0) {
      // Базовая линия могла разъехаться с фактом без всякого повода для тоста: каталог
      // пополнился задним числом (ачивка за первую тренировку) или, наоборот, прогресс
      // обнулился после полного удаления профиля админом. Просто выравниваем.
      if (baseline !== JSON.stringify(seen)) {
        try { localStorage.setItem(storageKey, baseline); } catch { /* ignore */ }
      }
      return;
    }
    setAchievementQueue((q) => [...q, ...fresh.filter((k) => !q.includes(k))]);
    tg?.HapticFeedback?.notificationOccurred?.("success");
    try { localStorage.setItem(storageKey, baseline); } catch { /* ignore */ }
  }, [tg]);

  // Пробитие уровня: сравниваем текущий уровень (по кубкам) с последним увиденным
  // (localStorage по user_id). Первый запуск на устройстве — тихая базовая линия,
  // чтобы не поздравлять задним числом за уже достигнутый уровень.
  const notifyLevelUp = useCallback((uid: number, level: number) => {
    if (!uid || level < 1) return;
    const storageKey = `fl_seen_level_${uid}`;
    let raw: string | null = null;
    try {
      raw = localStorage.getItem(storageKey);
    } catch {
      return; // приватный режим / нет доступа к storage — не спамим
    }
    if (raw === null) {
      try { localStorage.setItem(storageKey, String(level)); } catch { /* ignore */ }
      return;
    }
    const parsed = parseInt(raw, 10);
    const seenLevel = Number.isFinite(parsed) ? parsed : 0;
    if (level > seenLevel) {
      setLevelUpQueue((q) => (q.includes(level) ? q : [...q, level]));
      tg?.HapticFeedback?.notificationOccurred?.("success");
    }
    if (level !== seenLevel) {
      try { localStorage.setItem(storageKey, String(level)); } catch { /* ignore */ }
    }
  }, [tg]);

  const refreshProfileStats = useCallback(async () => {
    const apiBase = (import.meta.env.VITE_MINIAPP_API_URL as string | undefined)?.replace(/\/$/, "") ?? "";
    if (accessGateStatus !== "ok" || !inTelegram || !initData?.trim() || !apiBase) return;
    try {
      const res = await fetch(`${apiBase}/api/miniapp/profile/load`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ init_data: initData }),
      });
      const j = (await res.json().catch(() => ({}))) as {
        ok?: boolean;
        display_name?: string;
        streak_days?: number;
        max_streak_days?: number;
        xp?: number;
        achievement_count?: number;
        achievements_max?: number;
        workouts_total?: number;
        workouts_week?: number;
        pack_days?: number;
        inactivity_removal_at?: string;
        timezone_offset?: number;
        days_since_last_training?: number;
        last_training_date?: string;
        is_admin?: boolean;
        access_price_rub?: number;
        theme?: string;
        suggested_workout_types?: unknown;
      };
      if (!res.ok || !j.ok) return;
      setIsAdmin(Boolean(j.is_admin));
      if (typeof j.access_price_rub === "number" && j.access_price_rub > 0) {
        setAccessPriceRub(j.access_price_rub);
      }
      setProfileDisplayName((j.display_name ?? "").trim());
      setDaysSinceLastTraining(typeof j.days_since_last_training === "number" ? j.days_since_last_training : -1);
      setLastTrainingDate(typeof j.last_training_date === "string" ? j.last_training_date.trim() : "");
      setInactivityRemovalAt(typeof j.inactivity_removal_at === "string" ? j.inactivity_removal_at.trim() : "");
      setStreak(typeof j.streak_days === "number" ? j.streak_days : 0);
      setRecordStreak(typeof j.max_streak_days === "number" ? j.max_streak_days : 0);
      const xpNow = typeof j.xp === "number" ? j.xp : 0;
      setXP(xpNow);
      const levelNow = miniappLevelFromCups(xpNow);
      const themeUnlock = {
        streakDays: typeof j.streak_days === "number" ? j.streak_days : 0,
        maxStreakDays: typeof j.max_streak_days === "number" ? j.max_streak_days : 0,
        workoutsTotal: typeof j.workouts_total === "number" ? j.workouts_total : 0,
        isAdmin: Boolean(j.is_admin),
      };
      hydrateThemeFromServer(j.theme, levelNow, themeUnlock);
      if (!isThemeMode(j.theme) && hasStoredTheme()) {
        void persistThemeToServer(initData, getStoredTheme());
      }
      notifyLevelUp(userId, levelNow);
      const achCount = typeof j.achievement_count === "number" ? j.achievement_count : 0;
      const workoutsTotal = typeof j.workouts_total === "number" ? j.workouts_total : 0;
      setAchievementCount(achCount);
      setAchievementsMax(typeof j.achievements_max === "number" ? j.achievements_max : 9);
      setWorkouts(workoutsTotal);
      setPackDays(typeof j.pack_days === "number" && j.pack_days > 0 ? j.pack_days : 0);
      setSuggestedWorkoutTypes(parseSuggestedWorkoutTypes(j.suggested_workout_types));
      notifyNewAchievements(userId, achCount, workoutsTotal);
      // Автоопределение часового пояса из устройства: приводим хранимое смещение к зоне телефона.
      if (!tzSyncedRef.current) {
        tzSyncedRef.current = true;
        const currentOffset = typeof j.timezone_offset === "number" ? j.timezone_offset : 0;
        void syncDeviceTimezone(initData, currentOffset);
      }
    } catch {
      return;
    }
  }, [accessGateStatus, inTelegram, initData, userId, notifyNewAchievements, notifyLevelUp]);

  useEffect(() => {
    void refreshAccessStatus();
  }, [refreshAccessStatus]);

  useEffect(() => {
    const t = window.setInterval(() => {
      void refreshAccessStatus();
    }, 30_000);
    const onVis = () => {
      if (document.visibilityState === "visible") void refreshAccessStatus();
    };
    document.addEventListener("visibilitychange", onVis);
    return () => {
      window.clearInterval(t);
      document.removeEventListener("visibilitychange", onVis);
    };
  }, [refreshAccessStatus]);

  useEffect(() => {
    if (accessGateStatus !== "ok") return;
    void refreshTabBadges();
    const t = window.setInterval(() => {
      void refreshTabBadges();
    }, 30_000);
    const onVis = () => {
      if (document.visibilityState === "visible") void refreshTabBadges();
    };
    document.addEventListener("visibilitychange", onVis);
    return () => {
      window.clearInterval(t);
      document.removeEventListener("visibilitychange", onVis);
    };
  }, [accessGateStatus, refreshTabBadges]);

  // Перепроверяем непрочитанные при каждой смене вкладки, чтобы бейдж появлялся
  // сразу при навигации, а не ждал 30-секундный тик. Вход в личный чат с Лео
  // пропускаем: там бейдж гасится оптимистично (clearLeoBadge), и повторный
  // запрос мог бы вернуть его обратно, пока бэкенд не пометил входящие прочитанными.
  useEffect(() => {
    if (accessGateStatus !== "ok") return;
    if (tab === "chat") return;
    void refreshTabBadges();
  }, [tab, accessGateStatus, refreshTabBadges]);

  useEffect(() => {
    if (accessGateStatus !== "ok") return;
    void refreshProfileStats();
  }, [accessGateStatus, refreshProfileStats]);

  const onLeoInboxDrained = useCallback(() => {
    void refreshTabBadges();
  }, [refreshTabBadges]);

  const onLeoChatOpened = useCallback(() => {
    clearLeoBadge();
  }, [clearLeoBadge]);

  // Отправка текстового сообщения в общую ленту из компоуз-поля таббара.
  // Пишется в чат стаи (miniapp_pack_group_chat) и появляется карточкой в ленте.
  const sendFeedMessage = useCallback(
    async (text: string, photo?: File | null): Promise<boolean> => {
      const t = text.trim();
      if (!t && !photo) return false;
      if (!inTelegram || !initData) {
        showAlert("Открой мини-апп из Telegram.");
        return false;
      }
      const apiBase = (import.meta.env.VITE_MINIAPP_API_URL as string | undefined)?.replace(/\/$/, "") ?? "";
      if (!apiBase) {
        showAlert("Сборка без VITE_MINIAPP_API_URL.");
        return false;
      }
      try {
        let res: Response;
        if (photo) {
          // С фото — multipart на тот же эндпоинт, что и комментарии-фото (reply_to_id опущен → верхнеуровневое сообщение).
          const fd = new FormData();
          fd.append("init_data", initData);
          fd.append("text", t);
          fd.append("photo", photo, photo.name || "photo.jpg");
          res = await fetch(`${apiBase}/api/miniapp/pack-group/messages/photo`, { method: "POST", body: fd });
        } else {
          res = await fetch(`${apiBase}/api/miniapp/pack-group/messages`, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ init_data: initData, text: t }),
          });
        }
        const j = (await res.json().catch(() => ({}))) as { error?: string; message?: string };
        if (!res.ok) {
          if (isModerationError(j.error)) {
            showAlert(moderationUserMessage(j.error, j.message));
            return false;
          }
          const errMap: Record<string, string> = {
            empty_text: "Пустое сообщение",
            text_too_long: "Слишком длинное сообщение",
            chat_mismatch: "Открой мини-апп из чата стаи",
            forbidden: "Нет доступа",
          };
          showAlert(errMap[j.error ?? ""] ?? j.error ?? `Ошибка ${res.status}`);
          return false;
        }
        tg?.HapticFeedback?.impactOccurred?.("light");
        setTab("feed");
        setFeedRefreshToken((v) => v + 1);
        void refreshTabBadges();
        return true;
      } catch (e) {
        showAlert(e instanceof Error ? e.message : "Сеть");
        return false;
      }
    },
    [inTelegram, initData, showAlert, tg, refreshTabBadges],
  );

  const effectiveName = profileDisplayName.trim() || name.trim() || "друг";
  // Админка — шторка поверх приложения, профиль остаётся виден в зазоре сверху.
  const tabsVisible = !supportOpen && !workoutOpen;

  if (accessGateStatus === "checking") {
    return <div className="app" />;
  }

  if (accessGateStatus === "deleted") {
    return (
      <div className="app">
        <MiniappRemovedScreen />
      </div>
    );
  }

  return (
    <div className="app">
      <div className="app__tabs">
        <TabKeepAlive active={tab === "feed"} hidden={!tabsVisible}>
          <FeedScreen
            active={tab === "feed" && tabsVisible}
            streak={streak}
            userId={userId}
            initData={initData}
            inTelegram={inTelegram}
            showAlert={showAlert}
            refreshToken={feedRefreshToken}
            optimisticFeedItem={optimisticFeedItem}
            onOptimisticConsumed={onOptimisticConsumed}
            feedThreadUnreadCount={feedThreadUnread}
            onFeedThreadRead={clearFeedThreadBadge}
            onRefreshAll={async () => {
              await Promise.all([refreshProfileStats(), refreshTabBadges()]);
            }}
            onRefreshTabBadges={refreshTabBadges}
            isAdmin={isAdmin}
          />
        </TabKeepAlive>
        <TabKeepAlive active={tab === "chat"} hidden={!tabsVisible}>
          <ChatScreen
            active={tab === "chat" && tabsVisible}
            name={effectiveName}
            initData={initData}
            inTelegram={inTelegram}
            showAlert={showAlert}
            onInboxOpened={onLeoChatOpened}
            onInboxDrained={onLeoInboxDrained}
          />
        </TabKeepAlive>
        <TabKeepAlive active={tab === "rules"} hidden={!tabsVisible}>
          <RulesScreen accessPriceRub={accessPriceRub} />
        </TabKeepAlive>
        <TabKeepAlive active={tab === "profile"} hidden={!tabsVisible}>
          <ProfileScreen
            active={tab === "profile" && tabsVisible}
            name={effectiveName}
            streak={streak}
            workouts={workouts}
            packDays={packDays}
            initData={initData}
            inTelegram={inTelegram}
            userPhotoUrl={photoUrl}
            xp={xp}
            recordStreak={recordStreak}
            achievementCount={achievementCount}
            achievementsMax={achievementsMax}
            daysSinceLastTraining={daysSinceLastTraining}
            lastTrainingDate={lastTrainingDate || undefined}
            inactivityRemovalAt={inactivityRemovalAt || undefined}
            showAlert={showAlert}
            onProfileSaved={(displayName) => {
              setProfileDisplayName(displayName.trim());
            }}
            onStreakSaved={() => {
              void refreshProfileStats();
            }}
            onRefreshStats={refreshProfileStats}
            isAdmin={isAdmin}
            onSupport={() => {
              setWorkoutOpen(false);
              setAdminOpen(false);
              setSupportOpen(true);
            }}
            onAdmin={() => {
              setWorkoutOpen(false);
              setSupportOpen(false);
              setAdminOpen(true);
            }}
          />
        </TabKeepAlive>
      </div>

      {supportOpen ? (
        <SupportScreen
          initData={initData}
          inTelegram={inTelegram}
          showAlert={showAlert}
          onClose={() => setSupportOpen(false)}
        />
      ) : null}

      {adminOpen && isAdmin ? (
        <AdminScreen
          initData={initData}
          inTelegram={inTelegram}
          showAlert={showAlert}
          onClose={() => setAdminOpen(false)}
        />
      ) : null}

      {/* Празднования показываем по одному, чтобы оверлеи не накладывались:
          сначала «Новый уровень!», затем очередь ачивок. */}
      {currentLevelUp != null ? (
        <LevelUpToast
          key={`level-${currentLevelUp}`}
          level={currentLevelUp}
          onDone={() => setLevelUpQueue((q) => q.slice(1))}
        />
      ) : currentAchievement ? (
        <AchievementToast
          key={currentAchievement}
          achievementKey={currentAchievement}
          onDone={() => setAchievementQueue((q) => q.slice(1))}
        />
      ) : null}

      {/* В админке свой таббар внутри шторки: пользовательские вкладки там лишние. */}
      {adminOpen ? null : (
      <BottomNav
        active={tab}
        leoBadgeCount={leoPending}
        feedBadgeCount={feedUnread}
        showCompose={tabsVisible && tab === "feed"}
        onChat={() => {
          setWorkoutOpen(false);
          setSupportOpen(false);
          setAdminOpen(false);
          clearLeoBadge();
          setTab("chat");
        }}
        onFeed={() => {
          setWorkoutOpen(false);
          setSupportOpen(false);
          setAdminOpen(false);
          clearPackGroupBadge();
          setTab("feed");
        }}
        onSendMessage={sendFeedMessage}
        onAddWorkout={() => {
          setSupportOpen(false);
          setAdminOpen(false);
          void refreshProfileStats();
          setWorkoutOpen(true);
          reportWorkoutLogStarted(initData); // §4: открыл форму логирования
        }}
        onRules={() => {
          setWorkoutOpen(false);
          setSupportOpen(false);
          setAdminOpen(false);
          setTab("rules");
        }}
        onProfile={() => {
          setWorkoutOpen(false);
          setSupportOpen(false);
          setAdminOpen(false);
          setTab("profile");
        }}
      />
      )}

      {workoutOpen && (
        <NewWorkoutScreen
          suggestedWorkoutTypes={suggestedWorkoutTypes ?? undefined}
          showAlert={showAlert}
          onNonSportInterest={() => {
            if (inTelegram && initData?.trim()) {
              reportNonSportInterest(initData);
              return true;
            }
            return false; // нет initData — заявка не ушла, экран покажет ретрай
          }}
          onClose={() => setWorkoutOpen(false)}
          onSave={async ({ types, min, intensity, note, photo, otherLabel }) => {
            if (!inTelegram || !initData) {
              showAlert("Открой мини-апп из Telegram (нужен initData).");
              return false;
            }
            const labels: Record<string, string> = {
              run: "бег",
              walk: "ходьба",
              bike: "велосипед",
              swim: "плавание",
              yoga: "йога",
              rowing: "гребля",
              workout: "воркаут",
              crossfit: "кроссфит",
              stretch: "растяжка",
              dance: "танцы",
              hiit: "hiit",
              cardio: "кардио",
              kettlebell: "гиря",
              strength: "силовая",
              jump_rope: "скакалка",
              pole: "пилон",
              rollerblade: "ролики",
              basketball: "баскетбол",
              football: "футбол",
              volleyball: "волейбол",
              tennis: "теннис",
              padel: "падел",
              gymnastics: "гимнастика",
              other: "другое",
            };
            // Несколько видов объединяем через « + » — бэкенд начислит кубки за самый эффективный.
            const kind = types
              .map((t) => (t === "other" && otherLabel?.trim() ? otherLabel.trim() : labels[t] ?? t))
              .join(" + ");
            const base = `${kind}, ${min} мин, инт. ${intensity}/5`;
            const line = note ? `${base}\n\n${note}` : base;
            tg?.HapticFeedback?.impactOccurred?.("medium");
            // Отчёт: сервер отдаёт reply_text сразу (стрик, кубки, ачивки).
            const result = photo
              ? await sendMiniappTrainingWithPhoto(initData, line, photo)
              : await sendMiniappPrivateText(initData, line);
            if (!result.ok) {
              showAlert(result.error);
              return false;
            }
            setOptimisticFeedItem(
              buildOptimisticTrainingFeedItem({
                userId: userId || 0,
                username: effectiveName,
                text: line,
                streakDays: streak,
                trainingPhotoUrl: result.trainingPhotoUrl,
                authorPhotoUrl: photoUrl,
              }),
            );
            void refreshTabBadges();
            void refreshProfileStats();
            setTab("feed");
            setFeedRefreshToken((v) => v + 1);
            window.setTimeout(() => void refreshTabBadges(), 6000);
            const summary = formatTrainingDoneAlert(result.replyParts);
            const fallback =
              "Отчёт принят. Комментарий Лео скоро появится в ленте.";
            const msg = summary.length > 0 ? summary : fallback;
            showAlert(msg.length > 400 ? `${msg.slice(0, 397)}…` : msg);
            return true;
          }}
        />
      )}
    </div>
  );
}
