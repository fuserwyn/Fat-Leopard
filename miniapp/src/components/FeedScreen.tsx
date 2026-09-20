import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type CSSProperties } from "react";
import { usePullToRefresh } from "../hooks/usePullToRefresh";
import { tgConfirm } from "../lib/tgConfirm";
import { ActivityCard, type ActivityCardProps } from "./ActivityCard";
import { LEO_AVATAR_URL } from "../lib/leoAvatar";
import {
  dtoToCard,
  feedHasMatchingTrainingReport,
  mergePackFeedIncremental,
  mergeFeedReactionsForType,
  optimisticTogglePackFeedReaction,
  optimisticToggleThreadReplyLike,
  reconcilePinnedFeed,
  resolveFeedAvatarUrl,
  resolveTrainingPhotoUrl,
  sortPackFeedItemsDesc,
  extractEditableFeedPostText,
  buildFeedPostTextForSave,
  feedPostEditable,
  feedSupportsThread,
  feedItemKey,
  isFeedMessage,
  packMessageCommentReplyToId,
  mapFeedThreadQuote,
  type PackFeedItemDTO,
  type PackFeedThreadReplyDTO,
} from "../lib/packFeed";
import { moderationUserMessage, isModerationError } from "../lib/moderationMessages";
import { clearFeedThreadUnread, fetchFeedThreadUnreadSummary } from "../lib/feedThreadUnread";
import { clearPackGroupUnread } from "../lib/packGroupUnread";
import { reportLeoCommentDisplayed } from "../lib/leoCommentDiag";
import { formatLocalDateTime } from "../lib/timeAgo";
import { streakStreakAriaLabel } from "../lib/streakLabel";
import {
  PACK_WEEKLY_GOAL_DEFAULT,
  PACK_WEEKLY_GOAL_HINT,
  packWeeklyProgressLabel,
  packWeeklyStepFillPcts,
} from "../lib/packWeeklyGoal";
import { applyScrollY, feedFilterEpoch } from "../lib/tabScrollRestore";
import {
  sortWorkoutCategoryIds,
  sortWorkoutCategoriesByCountDesc,
  trainingDoneMatchesAnyCategory,
  WORKOUT_CATEGORY_OPTIONS,
  type WorkoutCategoryId,
} from "../lib/workoutCategories";
import "./FeedScreen.css";

const apiBase = (import.meta.env.VITE_MINIAPP_API_URL as string | undefined)?.replace(/\/$/, "") ?? "";
const FEED_POLL_MS = 8000;

function applyOptimisticFeedItem(items: PackFeedItemDTO[], optimistic: PackFeedItemDTO | null | undefined): PackFeedItemDTO[] {
  if (!optimistic) return items;
  if (feedHasMatchingTrainingReport(items, optimistic.text, optimistic.user_id)) return items;
  if (items.some((i) => i.id === optimistic.id)) return items;
  return sortPackFeedItemsDesc([optimistic, ...items]);
}

const tsMs = (s: string): number => Date.parse(s) || 0;

/** Самое свежее created_at среди реальных записей (id>0) — курсор «новее» (since_ts). */
function newestFeedTs(items: PackFeedItemDTO[]): string {
  let best = "";
  for (const it of items) {
    if (it.id <= 0) continue;
    if (best === "" || tsMs(it.created_at) > tsMs(best)) best = it.created_at;
  }
  return best;
}

/**
 * Самое старое created_at обычной (не закреплённой) записи — курсор «загрузить ещё старее»
 * (before_ts). Закрепы и оптимистичные (id<0) исключаем: они не задают границу окна.
 */
function minRegularFeedTs(items: PackFeedItemDTO[]): string {
  let oldest = "";
  for (const it of items) {
    if (it.id <= 0 || it.is_pinned) continue;
    if (oldest === "" || tsMs(it.created_at) < tsMs(oldest)) oldest = it.created_at;
  }
  return oldest;
}

type Props = {
  streak: number;
  userId: number;
  initData: string;
  inTelegram: boolean;
  showAlert: (m: string) => void;
  refreshToken?: number;
  /** Временная карточка сразу после отчёта (до ответа polling). */
  optimisticFeedItem?: PackFeedItemDTO | null;
  onOptimisticConsumed?: () => void;
  /** Перезагрузить общие данные (стрик, уровень, кубки) — вызывается при pull-to-refresh. */
  onRefreshAll?: () => Promise<void> | void;
  /** Вкладка «Стая» видима (keep-alive). */
  active?: boolean;
  /** Обновить бейджи таббара. */
  onRefreshTabBadges?: () => void;
  /** Непрочитанные комментарии к твоим отчётам (бейдж на вкладке «Стая»). */
  feedThreadUnreadCount?: number;
  /** Мгновенно убрать бейдж «Стая» из UI (после просмотра ленты). */
  onFeedThreadRead?: () => void;
  /** Админ может удалять любой пост ленты. */
  isAdmin?: boolean;
  /** Недельный прогресс стаи (сброс каждый понедельник 00:00 МСК). */
  packWorkoutsWeek?: number;
  packWorkoutsGoal?: number;
  packGoalReached?: boolean;
  packBonusThemeActive?: boolean;
};

// Единый поток — подвкладок больше нет; тип оставлен для совместимости эффектов.
type Sub = "activity";

type FeedViewportStyle = CSSProperties & {
  "--feed-vvh"?: string;
  "--feed-bottom-nav-h"?: string;
};

/** Почему лента не грузится — у каждой причины свой текст, чтобы не путать API URL с клиентом. */
type MockFeedReason = "no-api" | "no-telegram" | "no-initdata";

function mockFallback(streak: number, reason: MockFeedReason): ActivityCardProps[] {
  const card = (emoji: string, activity: string, details: string, comment: string): ActivityCardProps => ({
    avatar: "💬",
    name: "Стая",
    streak: Math.max(streak, 0),
    timeAgo: "сейчас",
    emoji,
    activity,
    details,
    comment,
  });
  switch (reason) {
    case "no-api":
      // Локальный предпросмотр (без бэкенда): несколько демо-записей, чтобы видеть тему.
      return [
        {
          avatar: "🌸",
          name: "Анна",
          streak: 12,
          timeAgo: "5 мин",
          emoji: "🚴",
          activity: "Велосипед",
          details: "90 мин · интенсивность 3/5 · 20 км",
          comment: "Доехала до фонтана и обратно, ноги горят 🔥",
          aiText: "Анна, 20 км — серьёзный заезд. Ритм у тебя уже свой.",
          trainingPhotoUrl: "https://picsum.photos/seed/leobike/640/420",
          reactions: [
            { emoji: "🔥", count: 4, me: false },
            { emoji: "💪", count: 2, me: true },
          ],
        },
        {
          avatar: "🐯",
          name: "Олег",
          streak: 5,
          timeAgo: "1 ч",
          emoji: "🏃",
          activity: "Бег",
          details: "30 мин · интенсивность 4/5 · 5 км",
          comment: "Утренняя пробежка перед работой.",
          reactions: [{ emoji: "❤️", count: 7, me: false }],
        },
        {
          avatar: "🧘‍♀️",
          name: "Лена",
          streak: 30,
          timeAgo: "3 ч",
          emoji: "🧘",
          activity: "Йога",
          details: "45 мин · интенсивность 2/5",
          comment: "Растянулась, спина сказала спасибо.",
          aiText: "Лена, 30 дней подряд — это характер. Спокойно и верно.",
          reactions: [
            { emoji: "🧘", count: 3, me: false },
            { emoji: "❤️", count: 5, me: true },
          ],
        },
        {
          avatar: "🏀",
          name: "Макс",
          streak: 8,
          timeAgo: "вчера",
          emoji: "🏀",
          activity: "Баскетбол",
          details: "60 мин · интенсивность 4/5",
          comment: "Дворовый матч 3х3, выложились полностью.",
          reactions: [{ emoji: "🔥", count: 6, me: false }],
        },
        {
          avatar: "🛼",
          name: "Катя",
          streak: 3,
          timeAgo: "вчера",
          emoji: "🛼",
          activity: "Ролики",
          details: "40 мин · интенсивность 3/5",
          comment: "Накаталась по набережной на закате.",
          reactions: [
            { emoji: "🛼", count: 9, me: true },
            { emoji: "🔥", count: 2, me: false },
          ],
        },
      ];
    case "no-telegram":
      return [
        card(
          "📲",
          "Открой через Telegram",
          "Мини-апп вне Telegram",
          "Запусти мини-апп кнопкой меню бота. В браузере или старом клиенте лента недоступна.",
        ),
      ];
    case "no-initdata":
    default:
      return [
        card(
          "🔄",
          "Не получили данные Telegram",
          "Обнови приложение",
          "Telegram не передал данные входа. Обнови Telegram до последней версии и переоткрой мини-апп.",
        ),
      ];
  }
}

export function FeedScreen({
  streak,
  userId,
  initData,
  inTelegram,
  showAlert,
  refreshToken = 0,
  optimisticFeedItem = null,
  onOptimisticConsumed,
  onRefreshAll,
  active = true,
  onRefreshTabBadges,
  feedThreadUnreadCount = 0,
  onFeedThreadRead,
  isAdmin = false,
  packWorkoutsWeek = 0,
  packWorkoutsGoal = PACK_WEEKLY_GOAL_DEFAULT,
  packGoalReached = false,
  packBonusThemeActive = false,
}: Props) {
  const [sub] = useState<Sub>("activity");
  const [unreadFeedCardIds, setUnreadFeedCardIds] = useState<Set<number>>(() => new Set());

  const [feedItems, setFeedItems] = useState<PackFeedItemDTO[]>([]);
  const [useMockFeed, setUseMockFeed] = useState(false);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState<string | null>(null);
  const [threadDrafts, setThreadDrafts] = useState<Record<number, string>>({});
  const [threadPosting, setThreadPosting] = useState<Record<number, boolean>>({});
  const [threadReplyDeleting, setThreadReplyDeleting] = useState<Record<number, boolean>>({});
  const [feedReportPosting, setFeedReportPosting] = useState<Record<number, boolean>>({});
  const [feedDeletePosting, setFeedDeletePosting] = useState<Record<number, boolean>>({});
  const [feedPinPosting, setFeedPinPosting] = useState<Record<number, boolean>>({});
  const [threadReplyReporting, setThreadReplyReporting] = useState<Record<number, boolean>>({});
  const [postEditId, setPostEditId] = useState<number | null>(null);
  const [postEditDrafts, setPostEditDrafts] = useState<Record<number, string>>({});
  const [postEditPosting, setPostEditPosting] = useState<Record<number, boolean>>({});
  const [postPhotoBusy, setPostPhotoBusy] = useState<Record<number, boolean>>({});
  const [threadEditTargets, setThreadEditTargets] = useState<
    Record<number, { threadReplyId: number; originalText: string } | undefined>
  >({});
  /** Ответ на конкретное сообщение треда (reply_to_id) — ключ id отчёта user_messages. */
  const [threadReplyTargets, setThreadReplyTargets] = useState<
    Record<number, { replyToThreadId: number; authorLabel: string; excerpt: string } | undefined>
  >({});
  /** Черновики/статусы комментов к сообщениям (pack_message) — ключ id сообщения-карточки. */
  const [msgThreadDrafts, setMsgThreadDrafts] = useState<Record<number, string>>({});
  const [msgThreadPosting, setMsgThreadPosting] = useState<Record<number, boolean>>({});
  const [msgThreadEdit, setMsgThreadEdit] = useState<
    Record<number, { replyId: number; originalText: string } | undefined>
  >({});
  /** Ответ на комментарий в треде сообщения (как в отчётах) — ключ id карточки pack_message. */
  const [msgThreadReplyTargets, setMsgThreadReplyTargets] = useState<
    Record<number, { replyToThreadId: number; authorLabel: string; excerpt: string } | undefined>
  >({});
  /** Идёт запрос подписки/отписки — ключ user_id автора. */
  const [followPosting, setFollowPosting] = useState<Record<number, boolean>>({});
  /** Чьи отчёты показывать: все / только мои / только друзей (на кого подписан). */
  const [feedScope, setFeedScope] = useState<"all" | "mine" | "friends">("all");
  /** Тип контента единой ленты: всё / только тренировки+системные / только сообщения чата. */
  const [feedTypeFilter, setFeedTypeFilter] = useState<"all" | "training" | "message">("all");
  /** Мультивыбор типов тренировок (пусто = «все типы»). */
  const [feedCategoryIds, setFeedCategoryIds] = useState<WorkoutCategoryId[]>([]);
  /** Развёрнут ли вертикальный список всех типов (открывается повторным тапом по «Все типы»). */
  const [catListOpen, setCatListOpen] = useState(false);
  /** Число training_done по видам спорта за всё время стаи (с бэкенда). */
  const [workoutTypeCounts, setWorkoutTypeCounts] = useState<Partial<Record<WorkoutCategoryId, number>>>({});
  const [viewportStyle, setViewportStyle] = useState<FeedViewportStyle>({});
  const feedHeaderRef = useRef<HTMLDivElement>(null);
  /** Самое свежее created_at в окне — курсор «новее» (since_ts) для инкрементального синка. */
  const newestTsRef = useRef("");
  /** Самое старое created_at обычной ленты (без закрепов) — курсор подгрузки вниз (before_ts). */
  const oldestTsRef = useRef("");
  const loadedOnceRef = useRef(false);
  /** Идёт полный синк — чтобы не плодить дубли (двойной fetch на маунте, наложение поллинга и пост-экшн-синков). */
  const fullSyncInFlightRef = useRef(false);
  /** Есть ли ещё более старые записи (false — дошли до конца истории). */
  const [hasMoreOlder, setHasMoreOlder] = useState(true);
  const [loadingOlder, setLoadingOlder] = useState(false);
  const loadingOlderRef = useRef(false);
  /** Маркер у низа списка: когда виден — подтягиваем более старые записи. */
  const olderSentinelRef = useRef<HTMLDivElement>(null);
  /** Sentinel сейчас в зоне видимости (наблюдатель) — «насос» догрузки крутится, пока true. */
  const [sentinelVisible, setSentinelVisible] = useState(false);

  const categoryFilterSet = useMemo(() => new Set(feedCategoryIds), [feedCategoryIds]);
  const feedCategoryOptionsByFrequency = useMemo(
    () => sortWorkoutCategoriesByCountDesc(WORKOUT_CATEGORY_OPTIONS, workoutTypeCounts),
    [workoutTypeCounts],
  );
  const feedCategoryChipsVisible = useMemo(() => {
    const withWorkouts = feedCategoryOptionsByFrequency.filter((c) => (workoutTypeCounts[c.id] ?? 0) > 0);
    for (const id of feedCategoryIds) {
      if (!withWorkouts.some((c) => c.id === id)) {
        const opt = WORKOUT_CATEGORY_OPTIONS.find((o) => o.id === id);
        if (opt) withWorkouts.push(opt);
      }
    }
    return withWorkouts;
  }, [feedCategoryOptionsByFrequency, workoutTypeCounts, feedCategoryIds]);
  const filterEpoch = feedFilterEpoch(feedScope, feedTypeFilter, feedCategoryIds);
  const filterEpochRef = useRef(filterEpoch);
  const [listSwapClass, setListSwapClass] = useState("");

  const feedWithOptimistic = useMemo(
    () => applyOptimisticFeedItem(feedItems, optimisticFeedItem),
    [feedItems, optimisticFeedItem],
  );

  const visibleFeedItems = useMemo(() => {
    return feedWithOptimistic.filter((it) => {
      if (it.type === "sick_leave") return false;
      // Закреплённое объявление видно всегда (поверх фильтров).
      if (it.is_pinned) return true;
      // Свой только что отправленный пост показываем всегда, даже если активен
      // фильтр по типам тренировок (иначе кажется, что отчёт не отправился).
      if (optimisticFeedItem && it.id === optimisticFeedItem.id) return true;
      // Фильтр типа контента: тренировки/системные (feed) vs сообщения чата (message).
      const message = isFeedMessage(it);
      if (feedTypeFilter === "training" && message) return false;
      if (feedTypeFilter === "message" && !message) return false;
      if (feedScope === "mine" && !it.is_you) return false;
      if (feedScope === "friends" && !it.is_friend) return false;
      if (feedCategoryIds.length > 0) {
        if (it.type !== "training_done") return false;
        if (!trainingDoneMatchesAnyCategory(it.text, categoryFilterSet)) return false;
      }
      return true;
    });
  }, [feedWithOptimistic, feedScope, feedTypeFilter, feedCategoryIds, categoryFilterSet, optimisticFeedItem]);

  // Закреплённые объявления — сверху ленты (свежезакреплённые первыми), остальное — хронологически.
  const orderedFeedItems = useMemo(() => {
    const pinnedAtMs = (it: PackFeedItemDTO) => Date.parse(it.pinned_at ?? "") || 0;
    const pinned = visibleFeedItems
      .filter((it) => it.is_pinned)
      .sort((a, b) => pinnedAtMs(b) - pinnedAtMs(a));
    const rest = visibleFeedItems.filter((it) => !it.is_pinned);
    return [...pinned, ...rest];
  }, [visibleFeedItems]);

  const hapticLight = useCallback(() => {
    window.Telegram?.WebApp?.HapticFeedback?.impactOccurred?.("light");
  }, []);

  const toggleFeedCategory = useCallback(
    (id: WorkoutCategoryId) => {
      hapticLight();
      setFeedCategoryIds((prev) => {
        const next = new Set(prev);
        if (next.has(id)) next.delete(id);
        else next.add(id);
        return sortWorkoutCategoryIds([...next], workoutTypeCounts);
      });
    },
    [hapticLight, workoutTypeCounts],
  );

  const clearFeedCategories = useCallback(() => {
    hapticLight();
    setFeedCategoryIds([]);
    setCatListOpen(false);
  }, [hapticLight]);

  const applyCatListAndClose = useCallback(() => {
    hapticLight();
    setCatListOpen(false);
  }, [hapticLight]);

  useEffect(() => {
    if (!catListOpen) return;
    const onScroll = () => setCatListOpen(false);
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, [catListOpen]);

  // Смена фильтра: список не прыгает с середины страницы — до paint уходим наверх
  // и кратко помечаем ленту, чтобы карточки не мерялись через content-visibility.
  useLayoutEffect(() => {
    if (filterEpochRef.current === filterEpoch) return;
    filterEpochRef.current = filterEpoch;
    applyScrollY(0);
    setListSwapClass("feed__list--swap");
  }, [filterEpoch]);

  useEffect(() => {
    if (!listSwapClass) return;
    const reduced =
      typeof window.matchMedia === "function" &&
      window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    const id = window.setTimeout(() => setListSwapClass(""), reduced ? 0 : 220);
    return () => window.clearTimeout(id);
  }, [listSwapClass]);

  // «Все типы»: список открыт — повторный тап сворачивает и оставляет текущий
  // фильтр. Список закрыт и выбраны типы — сброс к «все типы». Список закрыт и
  // уже «все типы» — разворачивает вертикальный список.
  const toggleAllTypes = useCallback(() => {
    hapticLight();
    if (catListOpen) {
      setCatListOpen(false);
      return;
    }
    if (feedCategoryIds.length > 0) {
      setFeedCategoryIds([]);
    } else {
      setCatListOpen(true);
    }
  }, [catListOpen, feedCategoryIds.length, hapticLight]);

  const refreshUnreadFeedCards = useCallback(async () => {
    if (!inTelegram || !initData.trim()) {
      setUnreadFeedCardIds(new Set());
      return;
    }
    const summary = await fetchFeedThreadUnreadSummary(initData);
    setUnreadFeedCardIds(new Set(summary.userMessageIds));
  }, [inTelegram, initData]);

  useEffect(() => {
    if (!active || !inTelegram || !initData.trim()) return;
    void refreshUnreadFeedCards();
    const t = window.setInterval(() => void refreshUnreadFeedCards(), 30_000);
    return () => window.clearInterval(t);
  }, [active, inTelegram, initData, refreshUnreadFeedCards, feedThreadUnreadCount]);

  const markFeedCardThreadRead = useCallback(
    (userMessageId: number) => {
      if (userMessageId <= 0 || !initData.trim()) return;
      setUnreadFeedCardIds((prev) => {
        if (!prev.has(userMessageId)) return prev;
        const next = new Set(prev);
        next.delete(userMessageId);
        return next;
      });
      void (async () => {
        await clearFeedThreadUnread(initData, userMessageId);
        onRefreshTabBadges?.();
        await refreshUnreadFeedCards();
      })();
    },
    [initData, onRefreshTabBadges, refreshUnreadFeedCards],
  );

  // Бейдж «Стая» = непрочитанные комментарии к твоим отчётам плюс новые сообщения
  // общего чата. И то и другое читается здесь, в ленте: сообщения давно приходят
  // сюда же отдельными карточками (source=message), а прежний отдельный экран чата
  // в приложении не открывается. Поэтому гасим оба счётчика при открытии ленты —
  // иначе бейдж висел бы вечно, и погасить его было бы нечем.
  useEffect(() => {
    if (!active || sub !== "activity" || !inTelegram || !initData.trim()) return;

    onFeedThreadRead?.();
    setUnreadFeedCardIds(new Set());
    void (async () => {
      await Promise.all([clearFeedThreadUnread(initData), clearPackGroupUnread(initData)]);
      onRefreshTabBadges?.();
    })();
  }, [
    active,
    sub,
    inTelegram,
    initData,
    onFeedThreadRead,
    onRefreshTabBadges,
  ]);

  const syncFeed = useCallback(
    async (opts?: { full?: boolean; silent?: boolean; reset?: boolean; force?: boolean }) => {
      if (!apiBase || !inTelegram || !initData) {
        setLoading(false);
        setUseMockFeed(true);
        setFeedItems([]);
        setErr(null);
        newestTsRef.current = "";
        oldestTsRef.current = "";
        return;
      }
      const full = opts?.full === true || newestTsRef.current === "";
      const sinceTs = full ? "" : newestTsRef.current;
      // Дедуп: если полный синк уже выполняется, новый полный синк пропускаем
      // (защищает от двойного fetch на маунте и наложения поллинга/пост-экшн-синков).
      // force=true — ручное обновление по кнопке: дедуп игнорируем, чтобы клик всегда срабатывал.
      if (full && !opts?.force && fullSyncInFlightRef.current) return;
      if (full) fullSyncInFlightRef.current = true;
      if (!opts?.silent) setErr(null);
      if (!opts?.silent && !loadedOnceRef.current) setLoading(true);
      setUseMockFeed(false);
      try {
        const res = await fetch(`${apiBase}/api/miniapp/feed`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ init_data: initData, ...(sinceTs ? { since_ts: sinceTs } : {}) }),
        });
        const j = (await res.json().catch(() => ({}))) as {
          ok?: boolean;
          items?: PackFeedItemDTO[];
          pinned?: PackFeedItemDTO[];
          workout_type_counts?: Partial<Record<WorkoutCategoryId, number>>;
          error?: string;
        };
        if (!res.ok) {
          if (res.status === 403) {
            setErr("Нет доступа к ленте стаи: нужна подписка/участие в группе, как в боте.");
            if (!opts?.silent) setFeedItems([]);
            return;
          }
          if (!opts?.silent) {
            setErr(j.error ?? `Ошибка ${res.status}`);
            setFeedItems([]);
          }
          return;
        }
        // Успешный ответ (в т.ч. тихий поллинг) снимает «липкий» баннер ошибки.
        setErr(null);
        const incoming = j.items ?? [];
        const pinned = j.pinned ?? [];
        if (j.workout_type_counts) setWorkoutTypeCounts(j.workout_type_counts);
        if (full || sinceTs === "") {
          const reconciled = reconcilePinnedFeed(incoming, pinned);
          if (opts?.reset) {
            // Явная перезагрузка (маунт / refreshToken / pull-to-refresh): окно с нуля.
            setFeedItems(reconciled);
            oldestTsRef.current = minRegularFeedTs(reconciled);
            setHasMoreOlder(incoming.length >= 50);
          } else {
            // Обычный полный синк (поллинг тредов/реакций, пост-экшн): НЕ схлопываем ленту —
            // сохраняем уже догруженные старые страницы, иначе бесконечная переподгрузка.
            setFeedItems((prev) => {
              if (prev.length === 0) {
                oldestTsRef.current = minRegularFeedTs(reconciled);
                return reconciled;
              }
              const windowOldestTs = minRegularFeedTs(reconciled);
              const olderKept =
                windowOldestTs !== ""
                  ? prev.filter((p) => p.id > 0 && !p.is_pinned && tsMs(p.created_at) < tsMs(windowOldestTs))
                  : [];
              const next =
                olderKept.length > 0 ? sortPackFeedItemsDesc([...reconciled, ...olderKept]) : reconciled;
              oldestTsRef.current = minRegularFeedTs(next);
              return next;
            });
            // hasMoreOlder выставляем только на самой первой загрузке окна; дальше им
            // управляет loadOlder (станет false, когда дойдём до начала истории).
            if (!loadedOnceRef.current) setHasMoreOlder(incoming.length >= 50);
          }
          {
            const n = newestFeedTs(incoming);
            if (n !== "") newestTsRef.current = n;
          }
          if (
            optimisticFeedItem &&
            feedHasMatchingTrainingReport(incoming, optimisticFeedItem.text, optimisticFeedItem.user_id)
          ) {
            onOptimisticConsumed?.();
          }
        } else {
          setFeedItems((prev) => {
            const merged = mergePackFeedIncremental(prev, incoming);
            const next = reconcilePinnedFeed(merged, pinned);
            const n = newestFeedTs(incoming);
            if (n !== "" && tsMs(n) > tsMs(newestTsRef.current)) newestTsRef.current = n;
            if (
              optimisticFeedItem &&
              feedHasMatchingTrainingReport(next, optimisticFeedItem.text, optimisticFeedItem.user_id)
            ) {
              onOptimisticConsumed?.();
            }
            return next;
          });
        }
        loadedOnceRef.current = true;
      } catch (e) {
        if (!opts?.silent) {
          setErr(e instanceof Error ? e.message : "Сеть");
          setFeedItems([]);
        }
      } finally {
        if (full) fullSyncInFlightRef.current = false;
        if (!opts?.silent) setLoading(false);
      }
    },
    [inTelegram, initData, optimisticFeedItem, onOptimisticConsumed],
  );

  /** Подгрузка более старых записей (скролл вниз): before_ts = самое старое видимое created_at. */
  const loadOlder = useCallback(async () => {
    if (!apiBase || !inTelegram || !initData) return;
    if (loadingOlderRef.current) return;
    const beforeTs = oldestTsRef.current;
    if (beforeTs === "") return;
    loadingOlderRef.current = true;
    setLoadingOlder(true);
    try {
      const res = await fetch(`${apiBase}/api/miniapp/feed`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ init_data: initData, before_ts: beforeTs }),
      });
      const j = (await res.json().catch(() => ({}))) as { ok?: boolean; items?: PackFeedItemDTO[]; error?: string };
      if (!res.ok) return;
      const older = j.items ?? [];
      if (older.length === 0) {
        setHasMoreOlder(false);
        return;
      }
      // Курсор реально ушёл «глубже» — иначе (все записи уже были) насос застрянет: стоп.
      // Считаем ДО setFeedItems: updater у React 18 выполняется асинхронно, поэтому чтение
      // флага сразу после setState вернуло бы устаревшее значение. Все older старше before_ts,
      // так что их самое старое created_at и есть новый курсор.
      const olderOldest = minRegularFeedTs(older);
      const advanced =
        olderOldest !== "" && (oldestTsRef.current === "" || tsMs(olderOldest) < tsMs(oldestTsRef.current));
      setFeedItems((prev) => {
        const next = mergePackFeedIncremental(prev, older);
        oldestTsRef.current = minRegularFeedTs(next);
        return next;
      });
      // Меньше полной страницы или курсор не сдвинулся — история исчерпана.
      if (older.length < 50 || !advanced) setHasMoreOlder(false);
    } catch {
      // Тихо: повторим при следующем скролле/обновлении.
    } finally {
      loadingOlderRef.current = false;
      setLoadingOlder(false);
    }
  }, [inTelegram, initData]);

  // Бесконечный скролл: наблюдаем маркер у низа списка и только фиксируем его видимость.
  // Саму догрузку крутит эффект-«насос» ниже — это чинит застревание, когда после
  // подгрузки sentinel остаётся в зоне видимости (IntersectionObserver не шлёт новое
  // событие, пока элемент не выйдет из вида и не войдёт снова).
  useEffect(() => {
    const sentinel = olderSentinelRef.current;
    if (!sentinel) {
      setSentinelVisible(false);
      return;
    }
    const io = new IntersectionObserver(
      (entries) => {
        const visible = entries.some((e) => e.isIntersecting);
        setSentinelVisible(visible);
      },
      { rootMargin: "800px 0px" },
    );
    io.observe(sentinel);
    return () => {
      io.disconnect();
      setSentinelVisible(false);
    };
  }, [useMockFeed, feedItems.length]);

  // «Насос» догрузки: пока маркер виден и есть более старые записи — тянем страницу
  // за страницей. Каждая успешная подгрузка меняет feedItems.length → эффект
  // перезапускается и, если маркер всё ещё виден, грузит следующую страницу.
  useEffect(() => {
    if (useMockFeed || !hasMoreOlder || loadingOlder) return;
    if (!sentinelVisible) return;
    void loadOlder();
  }, [sentinelVisible, hasMoreOlder, loadingOlder, useMockFeed, loadOlder, feedItems.length]);

  const toggleFollowAuthor = useCallback(
    async (authorId: number, currentlyFriend: boolean) => {
      if (!apiBase || !initData || authorId <= 0) return;
      let busy = false;
      setFollowPosting((p) => {
        busy = Boolean(p[authorId]);
        return busy ? p : { ...p, [authorId]: true };
      });
      if (busy) return;
      const next = !currentlyFriend;
      // Оптимистично переключаем флаг на всех карточках этого автора.
      const setFriendOnAuthor = (val: boolean) =>
        setFeedItems((prev) => prev.map((it) => (it.user_id === authorId ? { ...it, is_friend: val } : it)));
      setFriendOnAuthor(next);
      try {
        const res = await fetch(`${apiBase}/api/miniapp/friends/${next ? "follow" : "unfollow"}`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ init_data: initData, target_id: authorId }),
        });
        const j = (await res.json().catch(() => ({}))) as { ok?: boolean; error?: string };
        if (!res.ok || !j.ok) {
          setFriendOnAuthor(currentlyFriend);
          showAlert(j.error ?? `Ошибка ${res.status}`);
        } else {
          hapticLight();
        }
      } catch (e) {
        setFriendOnAuthor(currentlyFriend);
        showAlert(e instanceof Error ? e.message : "Сеть");
      } finally {
        setFollowPosting((p) => ({ ...p, [authorId]: false }));
      }
    },
    [apiBase, initData, showAlert, hapticLight],
  );

  const postTrainingReact = useCallback(
    (userMessageId: number, emoji: string) => {
      if (!apiBase || !initData) return;
      hapticLight();

      const applyToggle = () =>
        setFeedItems((prev) =>
          prev.map((it) =>
            it.id === userMessageId && !isFeedMessage(it)
              ? { ...it, reactions: optimisticTogglePackFeedReaction(it.reactions, emoji) }
              : it,
          ),
        );

      // Оптимистично переключаем реакцию сразу. Откат — повторное переключение
      // того же эмодзи на этом же посте (toggle идемпотентен), чтобы не затереть
      // параллельные оптимистичные изменения других реакций/постов.
      applyToggle();

      void (async () => {
        try {
          const res = await fetch(`${apiBase}/api/miniapp/feed/training/react`, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ init_data: initData, user_message_id: userMessageId, emoji }),
          });
          const j = (await res.json().catch(() => ({}))) as { error?: string };
          if (!res.ok) {
            applyToggle();
            showAlert(j.error === "invalid_emoji" ? "Такую реакцию нельзя" : j.error ?? `Ошибка ${res.status}`);
            return;
          }
          void syncFeed({ full: true, silent: true });
        } catch (e) {
          applyToggle();
          showAlert(e instanceof Error ? e.message : "Сеть");
        }
      })();
    },
    [apiBase, initData, syncFeed, showAlert, hapticLight],
  );

  // Реакция на сообщение общего чата в единой ленте (эндпоинт pack-group).
  const postMessageReact = useCallback(
    (messageId: number, emoji: string) => {
      if (!apiBase || !initData) return;
      hapticLight();

      const applyToggle = () =>
        setFeedItems((prev) =>
          prev.map((it) =>
            it.id === messageId && isFeedMessage(it)
              ? { ...it, reactions: optimisticTogglePackFeedReaction(it.reactions, emoji) }
              : it,
          ),
        );

      applyToggle();

      void (async () => {
        try {
          const res = await fetch(`${apiBase}/api/miniapp/pack-group/react`, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ init_data: initData, message_id: messageId, emoji }),
          });
          const j = (await res.json().catch(() => ({}))) as { error?: string };
          if (!res.ok) {
            applyToggle();
            showAlert(j.error === "invalid_emoji" ? "Такую реакцию нельзя" : j.error ?? `Ошибка ${res.status}`);
            return;
          }
          void syncFeed({ full: true, silent: true });
        } catch (e) {
          applyToggle();
          showAlert(e instanceof Error ? e.message : "Сеть");
        }
      })();
    },
    [apiBase, initData, syncFeed, showAlert, hapticLight],
  );

  // --- Комменты (треды) к сообщениям pack_message: коммент = сообщение чата с reply_to ---

  // Публикация коммента под сообщением-карточкой (текст и/или фото).
  // replyToCommentId — ответ конкретному комментарию (Лео или участник); иначе — на саму карточку.
  const postMessageComment = useCallback(
    async (cardId: number, text: string, photo?: File | null, replyToCommentId?: number) => {
      const t = text.trim();
      if (!t && !photo) {
        showAlert("Введи текст комментария или прикрепи фото.");
        return;
      }
      if (!apiBase || !initData) return;
      const replyToId = packMessageCommentReplyToId(cardId, replyToCommentId);
      setMsgThreadPosting((p) => ({ ...p, [cardId]: true }));
      try {
        let res: Response;
        if (photo) {
          const fd = new FormData();
          fd.append("init_data", initData);
          fd.append("text", t);
          fd.append("reply_to_id", String(replyToId));
          fd.append("photo", photo, photo.name || "photo.jpg");
          res = await fetch(`${apiBase}/api/miniapp/pack-group/messages/photo`, { method: "POST", body: fd });
        } else {
          res = await fetch(`${apiBase}/api/miniapp/pack-group/messages`, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ init_data: initData, text: t, reply_to_id: replyToId }),
          });
        }
        const j = (await res.json().catch(() => ({}))) as { error?: string; message?: string };
        if (!res.ok) {
          if (isModerationError(j.error)) {
            showAlert(moderationUserMessage(j.error, j.message));
            return;
          }
          showAlert(j.error ?? `Ошибка ${res.status}`);
          return;
        }
        setMsgThreadDrafts((d) => ({ ...d, [cardId]: "" }));
        setMsgThreadReplyTargets((r) => ({ ...r, [cardId]: undefined }));
        await syncFeed({ full: true, silent: true });
      } catch (e) {
        showAlert(e instanceof Error ? e.message : "Сеть");
      } finally {
        setMsgThreadPosting((p) => ({ ...p, [cardId]: false }));
      }
    },
    [apiBase, initData, syncFeed, showAlert],
  );

  // Редактирование своего коммента-сообщения.
  const editMessageComment = useCallback(
    async (cardId: number, replyId: number, text: string) => {
      const t = text.trim();
      if (!t) {
        showAlert("Комментарий не может быть пустым.");
        return;
      }
      if (!apiBase || !initData) return;
      setMsgThreadPosting((p) => ({ ...p, [cardId]: true }));
      try {
        const res = await fetch(`${apiBase}/api/miniapp/pack-group/messages/edit`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ init_data: initData, message_id: replyId, text: t }),
        });
        const j = (await res.json().catch(() => ({}))) as { error?: string; message?: string };
        if (!res.ok) {
          if (isModerationError(j.error)) {
            showAlert(moderationUserMessage(j.error, j.message));
            return;
          }
          showAlert(j.error ?? `Ошибка ${res.status}`);
          return;
        }
        setMsgThreadEdit((r) => ({ ...r, [cardId]: undefined }));
        setMsgThreadDrafts((d) => ({ ...d, [cardId]: "" }));
        await syncFeed({ full: true, silent: true });
      } catch (e) {
        showAlert(e instanceof Error ? e.message : "Сеть");
      } finally {
        setMsgThreadPosting((p) => ({ ...p, [cardId]: false }));
      }
    },
    [apiBase, initData, syncFeed, showAlert],
  );

  // Удаление/скрытие коммента-сообщения (своё — удалить, админ — скрыть).
  const deleteMessageComment = useCallback(
    async (replyId: number) => {
      if (!apiBase || !initData) return;
      setThreadReplyDeleting((p) => ({ ...p, [replyId]: true }));
      try {
        const res = await fetch(`${apiBase}/api/miniapp/pack-group/messages/delete`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ init_data: initData, message_id: replyId }),
        });
        const j = (await res.json().catch(() => ({}))) as { error?: string };
        if (!res.ok) {
          showAlert(j.error ?? `Ошибка ${res.status}`);
          return;
        }
        await syncFeed({ full: true, silent: true });
      } catch (e) {
        showAlert(e instanceof Error ? e.message : "Сеть");
      } finally {
        setThreadReplyDeleting((p) => ({ ...p, [replyId]: false }));
      }
    },
    [apiBase, initData, syncFeed, showAlert],
  );

  // Жалоба на сообщение или коммент (оба — сообщения чата).
  const reportMessageContent = useCallback(
    async (messageId: number) => {
      if (!apiBase || !initData) return;
      setThreadReplyReporting((p) => ({ ...p, [messageId]: true }));
      setFeedReportPosting((p) => ({ ...p, [messageId]: true }));
      try {
        const res = await fetch(`${apiBase}/api/miniapp/pack-group/report`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ init_data: initData, message_id: messageId }),
        });
        const j = (await res.json().catch(() => ({}))) as { error?: string };
        if (!res.ok) {
          const errMap: Record<string, string> = {
            cannot_report_self: "Нельзя пожаловаться на своё сообщение",
            cannot_report_leo: "На это сообщение пожаловаться нельзя",
            already_reported: "Ты уже отправлял жалобу на это",
          };
          showAlert(errMap[j.error ?? ""] ?? j.error ?? `Ошибка ${res.status}`);
          return;
        }
        showAlert("Жалоба отправлена. Админы увидят её в поддержке.");
      } catch (e) {
        showAlert(e instanceof Error ? e.message : "Сеть");
      } finally {
        setThreadReplyReporting((p) => ({ ...p, [messageId]: false }));
        setFeedReportPosting((p) => ({ ...p, [messageId]: false }));
      }
    },
    [apiBase, initData, showAlert],
  );

  const confirmReportMessage = useCallback(
    async (messageId: number) => {
      if (!(await tgConfirm("Отправить жалобу?\n\nАдмины проверят её в разделе поддержки."))) return;
      void reportMessageContent(messageId);
    },
    [reportMessageContent],
  );

  // Удаление/скрытие сообщения-карточки (своё — удалить, админ — скрыть).
  const deleteMessageCard = useCallback(
    async (cardId: number) => {
      if (!apiBase || !initData) return;
      if (!(await tgConfirm("Удалить сообщение из ленты?"))) return;
      setFeedDeletePosting((p) => ({ ...p, [cardId]: true }));
      try {
        const res = await fetch(`${apiBase}/api/miniapp/pack-group/messages/delete`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ init_data: initData, message_id: cardId }),
        });
        const j = (await res.json().catch(() => ({}))) as { error?: string };
        if (!res.ok) {
          showAlert(j.error ?? `Ошибка ${res.status}`);
          return;
        }
        setFeedItems((prev) => prev.filter((it) => !(it.id === cardId && isFeedMessage(it))));
        await syncFeed({ full: true, silent: true });
      } catch (e) {
        showAlert(e instanceof Error ? e.message : "Сеть");
      } finally {
        setFeedDeletePosting((p) => ({ ...p, [cardId]: false }));
      }
    },
    [apiBase, initData, syncFeed, showAlert],
  );

  const voteFeedPoll = useCallback(
    async (userMessageId: number, optionIndex: number) => {
      if (!apiBase || !initData) return;
      try {
        const res = await fetch(`${apiBase}/api/miniapp/feed/poll/vote`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ init_data: initData, user_message_id: userMessageId, option_index: optionIndex }),
        });
        const j = (await res.json().catch(() => ({}))) as { error?: string };
        if (!res.ok) {
          const errMap: Record<string, string> = {
            not_found: "Опрос не найден",
            invalid_option: "Неверный вариант",
            forbidden: "Нет доступа",
            chat_mismatch: "Открой miniapp из чата стаи",
            poll_vote_error: "Не удалось сохранить голос",
          };
          showAlert(errMap[j.error ?? ""] ?? j.error ?? `Ошибка ${res.status}`);
          return;
        }
        await syncFeed({ full: true, silent: true });
      } catch (e) {
        showAlert(e instanceof Error ? e.message : "Сеть");
      }
    },
    [apiBase, initData, syncFeed, showAlert],
  );

  const postTrainingThread = useCallback(
    async (userMessageId: number, text: string, replyToThreadId?: number, photo?: File | null, postAs?: "self" | "leo" | "admin") => {
      const t = text.trim();
      if (!t && !photo) {
        showAlert("Введи текст комментария или прикрепи фото.");
        return;
      }
      if (!apiBase || !initData) return;
      setThreadPosting((p) => ({ ...p, [userMessageId]: true }));
      try {
        let res: Response;
        if (photo) {
          // Multipart: фото + (опционально) текст.
          const fd = new FormData();
          fd.append("init_data", initData);
          fd.append("user_message_id", String(userMessageId));
          fd.append("text", t);
          fd.append("reply_to_id", String(replyToThreadId ?? 0));
          if (postAs && postAs !== "self") fd.append("post_as", postAs);
          fd.append("photo", photo);
          res = await fetch(`${apiBase}/api/miniapp/feed/training/thread`, {
            method: "POST",
            body: fd,
          });
        } else {
          res = await fetch(`${apiBase}/api/miniapp/feed/training/thread`, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({
              init_data: initData,
              user_message_id: userMessageId,
              text: t,
              reply_to_id: replyToThreadId ?? 0,
              post_as: postAs ?? "self",
            }),
          });
        }
        const j = (await res.json().catch(() => ({}))) as {
          error?: string;
          message?: string;
          thread?: PackFeedThreadReplyDTO[];
        };
        if (!res.ok) {
          if (isModerationError(j.error)) {
            showAlert(moderationUserMessage(j.error, j.message));
            return;
          }
          const errMap: Record<string, string> = {
            empty_text: "Пустой комментарий",
            text_too_long: "Слишком длинно (максимум 500 символов)",
            not_found: "Запись не найдена (обнови ленту)",
            forbidden: "Нет доступа",
            chat_mismatch: "Открой мини-апп из чата стаи",
            thread_error: "Сервер не сохранил комментарий (таблица миграции или БД)",
            invalid_reply: "Не удалось ответить на это сообщение (обнови ленту)",
            invalid_multipart: "Не удалось прочитать фото — попробуй ещё раз",
            photo_open_error: "Не удалось открыть фото",
            photo_too_large: "Фото слишком большое",
            unsupported_type: "Неподдерживаемый формат фото",
            media_write_error: "Не удалось загрузить фото — попробуй ещё раз",
            media_not_configured: "Загрузка фото недоступна",
          };
          showAlert(errMap[j.error ?? ""] ?? j.error ?? `Ошибка ${res.status}`);
          return;
        }
        const postedThread = j.thread;
        setThreadDrafts((d) => ({ ...d, [userMessageId]: "" }));
        setThreadReplyTargets((r) => ({ ...r, [userMessageId]: undefined }));
        if (Array.isArray(postedThread)) {
          setFeedItems((prev) =>
            prev.map((it) => (it.id === userMessageId ? { ...it, thread: postedThread } : it)),
          );
        } else {
          await syncFeed({ full: true, silent: true });
        }
        if (replyToThreadId) {
          window.setTimeout(() => void syncFeed({ full: true, silent: true }), 5000);
        }
      } catch (e) {
        showAlert(e instanceof Error ? e.message : "Сеть");
      } finally {
        setThreadPosting((p) => ({ ...p, [userMessageId]: false }));
      }
    },
    [apiBase, initData, syncFeed, showAlert],
  );

  const editFeedPost = useCallback(
    async (item: PackFeedItemDTO) => {
      const draft = (postEditDrafts[item.id] ?? "").trim();
      if (!draft) {
        showAlert("Текст не может быть пустым.");
        return;
      }
      if (!apiBase || !initData) return;
      const payloadText = buildFeedPostTextForSave(item.text, item.type, draft);
      setPostEditPosting((p) => ({ ...p, [item.id]: true }));
      try {
        const res = await fetch(`${apiBase}/api/miniapp/feed/edit`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            init_data: initData,
            user_message_id: item.id,
            text: payloadText,
          }),
        });
        const j = (await res.json().catch(() => ({}))) as { error?: string; message?: string };
        if (!res.ok) {
          if (isModerationError(j.error)) {
            showAlert(moderationUserMessage(j.error, j.message));
            return;
          }
          const errMap: Record<string, string> = {
            empty_text: "Текст не может быть пустым",
            text_too_long: "Слишком длинный текст",
            not_found: "Пост не найден (обнови ленту)",
            forbidden: "Нет доступа",
            chat_mismatch: "Открой мини-апп из чата стаи",
            feed_edit_error: "Не удалось сохранить",
          };
          showAlert(errMap[j.error ?? ""] ?? j.error ?? `Ошибка ${res.status}`);
          return;
        }
        setPostEditId(null);
        setPostEditDrafts((d) => {
          const next = { ...d };
          delete next[item.id];
          return next;
        });
        setFeedItems((prev) =>
          prev.map((it) =>
            it.id === item.id
              ? { ...it, text: payloadText, edited_at: new Date().toISOString() }
              : it,
          ),
        );
        await syncFeed({ full: true, silent: true });
      } catch (e) {
        showAlert(e instanceof Error ? e.message : "Сеть");
      } finally {
        setPostEditPosting((p) => ({ ...p, [item.id]: false }));
      }
    },
    [apiBase, initData, postEditDrafts, showAlert, syncFeed],
  );

  const setFeedPostPhoto = useCallback(
    async (item: PackFeedItemDTO, file: File) => {
      if (!apiBase || !initData) return;
      setPostPhotoBusy((p) => ({ ...p, [item.id]: true }));
      try {
        const fd = new FormData();
        fd.append("init_data", initData);
        fd.append("user_message_id", String(item.id));
        fd.append("photo", file);
        const res = await fetch(`${apiBase}/api/miniapp/feed/photo`, { method: "POST", body: fd });
        const j = (await res.json().catch(() => ({}))) as { error?: string; message?: string; photo_url?: string };
        if (!res.ok) {
          if (isModerationError(j.error)) {
            showAlert(moderationUserMessage(j.error, j.message));
            return;
          }
          const errMap: Record<string, string> = {
            photo_too_large: "Фото слишком большое (до 6 МБ)",
            unsupported_image: "Формат фото не поддерживается",
            missing_photo: "Выбери фото",
            invalid_multipart: "Не удалось прочитать фото — попробуй ещё раз",
            not_found: "Пост не найден (обнови ленту)",
            forbidden: "Нет доступа",
            chat_mismatch: "Открой мини-апп из чата стаи",
            feed_photo_error: "Не удалось сохранить фото",
          };
          showAlert(errMap[j.error ?? ""] ?? j.error ?? `Ошибка ${res.status}`);
          return;
        }
        setFeedItems((prev) =>
          prev.map((it) => (it.id === item.id ? { ...it, training_photo_url: j.photo_url } : it)),
        );
        await syncFeed({ full: true, silent: true });
      } catch (e) {
        showAlert(e instanceof Error ? e.message : "Сеть");
      } finally {
        setPostPhotoBusy((p) => ({ ...p, [item.id]: false }));
      }
    },
    [apiBase, initData, showAlert, syncFeed],
  );

  const removeFeedPostPhoto = useCallback(
    async (item: PackFeedItemDTO) => {
      if (!apiBase || !initData) return;
      if (!(await tgConfirm("Удалить фото из поста?"))) return;
      setPostPhotoBusy((p) => ({ ...p, [item.id]: true }));
      try {
        const res = await fetch(`${apiBase}/api/miniapp/feed/photo/delete`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ init_data: initData, user_message_id: item.id }),
        });
        const j = (await res.json().catch(() => ({}))) as { error?: string };
        if (!res.ok) {
          const errMap: Record<string, string> = {
            not_found: "Пост не найден (обнови ленту)",
            forbidden: "Нет доступа",
            chat_mismatch: "Открой мини-апп из чата стаи",
            feed_photo_error: "Не удалось удалить фото",
          };
          showAlert(errMap[j.error ?? ""] ?? j.error ?? `Ошибка ${res.status}`);
          return;
        }
        setFeedItems((prev) =>
          prev.map((it) => (it.id === item.id ? { ...it, training_photo_url: undefined } : it)),
        );
        await syncFeed({ full: true, silent: true });
      } catch (e) {
        showAlert(e instanceof Error ? e.message : "Сеть");
      } finally {
        setPostPhotoBusy((p) => ({ ...p, [item.id]: false }));
      }
    },
    [apiBase, initData, showAlert, syncFeed],
  );

  const editTrainingThreadReply = useCallback(
    async (
      trainingUserMessageId: number,
      threadReplyId: number,
      text: string,
      originalText: string,
    ) => {
      const t = text.trim();
      if (!t) {
        showAlert("Комментарий не может быть пустым.");
        return;
      }
      if (!apiBase || !initData) return;
      setThreadPosting((p) => ({ ...p, [trainingUserMessageId]: true }));
      try {
        const res = await fetch(`${apiBase}/api/miniapp/feed/training/thread/edit`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            init_data: initData,
            thread_reply_id: threadReplyId,
            text: t,
          }),
        });
        const j = (await res.json().catch(() => ({}))) as {
          error?: string;
          message?: string;
          thread?: PackFeedThreadReplyDTO[];
        };
        if (!res.ok) {
          if (isModerationError(j.error)) {
            showAlert(moderationUserMessage(j.error, j.message));
            return;
          }
          const errMap: Record<string, string> = {
            empty_text: "Пустой комментарий",
            text_too_long: "Слишком длинно (максимум 500 символов)",
            not_found: "Комментарий не найден (обнови ленту)",
            forbidden: "Нет доступа",
            chat_mismatch: "Открой мини-апп из чата стаи",
            thread_edit_error: "Не удалось сохранить",
          };
          showAlert(errMap[j.error ?? ""] ?? j.error ?? `Ошибка ${res.status}`);
          return;
        }
        setThreadEditTargets((r) => ({ ...r, [trainingUserMessageId]: undefined }));
        setThreadDrafts((d) => ({ ...d, [trainingUserMessageId]: "" }));
        const updated = j.thread;
        if (Array.isArray(updated)) {
          setFeedItems((prev) =>
            prev.map((it) => (it.id === trainingUserMessageId ? { ...it, thread: updated } : it)),
          );
        } else {
          await syncFeed({ full: true, silent: true });
        }
      } catch (e) {
        setThreadDrafts((d) => ({ ...d, [trainingUserMessageId]: originalText }));
        showAlert(e instanceof Error ? e.message : "Сеть");
      } finally {
        setThreadPosting((p) => ({ ...p, [trainingUserMessageId]: false }));
      }
    },
    [apiBase, initData, showAlert, syncFeed],
  );

  const deleteTrainingThreadReply = useCallback(
    async (trainingUserMessageId: number, threadReplyId: number) => {
      if (!apiBase || !initData) return;
      setThreadReplyDeleting((p) => ({ ...p, [threadReplyId]: true }));
      try {
        const res = await fetch(`${apiBase}/api/miniapp/feed/training/thread/delete`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ init_data: initData, thread_reply_id: threadReplyId }),
        });
        const j = (await res.json().catch(() => ({}))) as {
          error?: string;
          thread?: PackFeedThreadReplyDTO[];
        };
        if (!res.ok) {
          const errMap: Record<string, string> = {
            not_found: "Комментарий не найден или уже удалён",
            forbidden: "Нет доступа",
            chat_mismatch: "Открой мини-апп из чата стаи",
            thread_delete_error: "Не удалось удалить",
          };
          showAlert(errMap[j.error ?? ""] ?? j.error ?? `Ошибка ${res.status}`);
          return;
        }
        const updated = j.thread;
        if (Array.isArray(updated)) {
          setFeedItems((prev) =>
            prev.map((it) => (it.id === trainingUserMessageId ? { ...it, thread: updated } : it)),
          );
        } else {
          await syncFeed({ full: true, silent: true });
        }
      } catch (e) {
        showAlert(e instanceof Error ? e.message : "Сеть");
      } finally {
        setThreadReplyDeleting((p) => ({ ...p, [threadReplyId]: false }));
      }
    },
    [apiBase, initData, syncFeed, showAlert],
  );

  const hideFeedPost = useCallback(
    async (userMessageId: number) => {
      if (!apiBase || !initData || !isAdmin) return;
      if (
        !(await tgConfirm(
          "Удалить публикацию из ленты?\n\nОна исчезнет у всех, но останется в админке (раздел «Скрытое») — оттуда её можно восстановить.",
        ))
      ) {
        return;
      }
      setFeedDeletePosting((p) => ({ ...p, [userMessageId]: true }));
      try {
        const res = await fetch(`${apiBase}/api/miniapp/feed/delete`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ init_data: initData, user_message_id: userMessageId }),
        });
        const j = (await res.json().catch(() => ({}))) as { error?: string };
        if (!res.ok) {
          const errMap: Record<string, string> = {
            not_found: "Пост не найден или уже скрыт",
            forbidden: "Нет прав",
            chat_mismatch: "Открой мини-апп из чата стаи",
            feed_delete_error: "Не удалось скрыть",
          };
          showAlert(errMap[j.error ?? ""] ?? j.error ?? `Ошибка ${res.status}`);
          return;
        }
        setFeedItems((prev) => prev.filter((it) => it.id !== userMessageId));
        setUnreadFeedCardIds((prev) => {
          if (!prev.has(userMessageId)) return prev;
          const next = new Set(prev);
          next.delete(userMessageId);
          return next;
        });
      } catch (e) {
        showAlert(e instanceof Error ? e.message : "Сеть");
      } finally {
        setFeedDeletePosting((p) => ({ ...p, [userMessageId]: false }));
      }
    },
    [apiBase, initData, isAdmin, showAlert],
  );

  const setFeedPostPinned = useCallback(
    async (userMessageId: number, pin: boolean) => {
      if (!apiBase || !initData || !isAdmin) return;
      setFeedPinPosting((p) => ({ ...p, [userMessageId]: true }));
      // Оптимистично — сразу двигаем карточку (вверх при закрепе / в хронологию при открепе).
      setFeedItems((prev) =>
        sortPackFeedItemsDesc(
          prev.map((it) =>
            it.id === userMessageId
              ? { ...it, is_pinned: pin, pinned_at: pin ? new Date().toISOString() : undefined }
              : it,
          ),
        ),
      );
      try {
        const res = await fetch(`${apiBase}/api/miniapp/feed/pin`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ init_data: initData, user_message_id: userMessageId, pin }),
        });
        const j = (await res.json().catch(() => ({}))) as { error?: string };
        if (!res.ok) {
          const errMap: Record<string, string> = {
            not_found: "Объявление не найдено или уже удалено",
            forbidden: "Закреплять может только админ",
            chat_mismatch: "Открой мини-апп из чата стаи",
            feed_pin_error: "Не удалось изменить закреп",
          };
          showAlert(errMap[j.error ?? ""] ?? j.error ?? `Ошибка ${res.status}`);
          // Откат — следующий синк восстановит истину с сервера.
          void syncFeed({ silent: true });
          return;
        }
        // Подтягиваем авторитетный набор закреплённых.
        void syncFeed({ silent: true });
      } catch (e) {
        showAlert(e instanceof Error ? e.message : "Сеть");
        void syncFeed({ silent: true });
      } finally {
        setFeedPinPosting((p) => ({ ...p, [userMessageId]: false }));
      }
    },
    [initData, isAdmin, showAlert, syncFeed],
  );

  const reportFeedContent = useCallback(
    async (userMessageId: number, threadReplyId = 0) => {
      if (!apiBase || !initData) return;
      const postingKey = threadReplyId > 0 ? threadReplyId : userMessageId;
      const setPosting =
        threadReplyId > 0 ? setThreadReplyReporting : setFeedReportPosting;
      setPosting((p) => ({ ...p, [postingKey]: true }));
      try {
        const res = await fetch(`${apiBase}/api/miniapp/feed/report`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            init_data: initData,
            user_message_id: userMessageId,
            thread_reply_id: threadReplyId,
          }),
        });
        const j = (await res.json().catch(() => ({}))) as { error?: string };
        if (!res.ok) {
          const errMap: Record<string, string> = {
            not_found: "Запись не найдена (обнови ленту)",
            forbidden: "Нет доступа",
            chat_mismatch: "Открой мини-апп из чата стаи",
            cannot_report_self: "Нельзя пожаловаться на своё сообщение",
            cannot_report_leo: "На это сообщение пожаловаться нельзя",
            already_reported: "Ты уже отправлял жалобу на это",
            report_error: "Не удалось отправить жалобу",
          };
          showAlert(errMap[j.error ?? ""] ?? j.error ?? `Ошибка ${res.status}`);
          return;
        }
        showAlert(
          threadReplyId > 0
            ? "Жалоба на комментарий отправлена. Админы увидят её в поддержке."
            : "Жалоба отправлена. Админы увидят её в поддержке.",
        );
      } catch (e) {
        showAlert(e instanceof Error ? e.message : "Сеть");
      } finally {
        setPosting((p) => ({ ...p, [postingKey]: false }));
      }
    },
    [apiBase, initData, showAlert],
  );

  const confirmReportPublication = useCallback(
    async (userMessageId: number) => {
      if (
        !(await tgConfirm(
          "Отправить жалобу на эту публикацию?\n\nАдмины проверят её в разделе поддержки.",
        ))
      ) {
        return;
      }
      void reportFeedContent(userMessageId);
    },
    [reportFeedContent],
  );

  const confirmReportComment = useCallback(
    async (userMessageId: number, threadReplyId: number) => {
      if (
        !(await tgConfirm(
          "Отправить жалобу на этот комментарий?\n\nАдмины проверят его в разделе поддержки.",
        ))
      ) {
        return;
      }
      void reportFeedContent(userMessageId, threadReplyId);
    },
    [reportFeedContent],
  );

  const toggleTrainingThreadLike = useCallback(
    (trainingUserMessageId: number, threadReplyId: number) => {
      if (!apiBase || !initData) return;
      hapticLight();

      const applyToggle = () =>
        setFeedItems((prev) =>
          prev.map((it) =>
            it.id === trainingUserMessageId && it.thread
              ? { ...it, thread: optimisticToggleThreadReplyLike(it.thread, threadReplyId) }
              : it,
          ),
        );

      // Оптимистично переключаем лайк; откат — повторный toggle того же лайка,
      // чтобы не затирать другие параллельные оптимистичные изменения.
      applyToggle();

      void (async () => {
        try {
          const res = await fetch(`${apiBase}/api/miniapp/feed/training/thread/like`, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ init_data: initData, thread_reply_id: threadReplyId }),
          });
          const j = (await res.json().catch(() => ({}))) as { error?: string; thread?: PackFeedThreadReplyDTO[] };
          if (!res.ok) {
            applyToggle();
            showAlert(j.error ?? `Ошибка ${res.status}`);
            return;
          }
          const updated = j.thread;
          if (Array.isArray(updated)) {
            setFeedItems((prev) =>
              prev.map((it) => (it.id === trainingUserMessageId ? { ...it, thread: updated } : it)),
            );
          } else {
            void syncFeed({ full: true, silent: true });
          }
        } catch (e) {
          applyToggle();
          showAlert(e instanceof Error ? e.message : "Сеть");
        }
      })();
    },
    [apiBase, initData, syncFeed, showAlert, hapticLight],
  );

  const activeRef = useRef(active);
  const syncFeedRef = useRef(syncFeed);
  activeRef.current = active;
  syncFeedRef.current = syncFeed;

  // Полный сброс окна — только refreshToken (отчёт / явный рефреш). Смена вкладки
  // и новая идентичность syncFeed не считаются ремаунтом: иначе «Загрузка…»
  // сдвигает карточки, а reset схлопывает догруженные страницы.
  useEffect(() => {
    newestTsRef.current = "";
    oldestTsRef.current = "";
    loadedOnceRef.current = false;
    setHasMoreOlder(true);
    if (activeRef.current) void syncFeedRef.current({ full: true, reset: true });
  }, [refreshToken]);

  useEffect(() => {
    if (!active || loadedOnceRef.current) return;
    void syncFeed({ full: true, reset: true });
  }, [active, syncFeed]);

  useEffect(() => {
    // Поллим ленту только когда открыта именно подвкладка «Лента»: на подвкладке
    // общего чата это лишняя сеть/батарея и фоновые апдейты состояния.
    if (!active || sub !== "activity" || !apiBase || !inTelegram || !initData.trim()) return;
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | null = null;

    const tick = async () => {
      if (cancelled) return;
      // Полный синк на каждом опросе: только так подтягиваются новые комментарии
      // (тред) и реакции к УЖЕ существующим постам. Инкрементальный since_id
      // возвращает лишь новые посты и не обновляет треды старых.
      await syncFeed({ full: true, silent: true });
      if (cancelled) return;
      timer = setTimeout(tick, FEED_POLL_MS);
    };

    timer = setTimeout(tick, FEED_POLL_MS);
    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
    };
  }, [active, sub, inTelegram, initData, syncFeed]);

  useLayoutEffect(() => {
    const sticky = feedHeaderRef.current;
    const feedRoot = sticky?.closest<HTMLElement>(".feed");
    if (!sticky || !feedRoot) return;
    // Реальный зазор: низ fixed-шапки минус верх .feed (учитывает safe-area, app padding, border).
    const write = () => {
      // .feed в обычном потоке, поэтому его top «уезжает» вверх (в минус) при скролле,
      // а fixed-шапка стоит на месте. Если write() сработает, пока лента прокручена
      // (ResizeObserver на смене высоты шапки — бейдж подвкладки, ширина числа стрика,
      // перенос чипов-фильтров), offset раздувается ровно на величину скролла и над
      // лентой остаётся огромный пустой блок. Компенсируем текущим скроллом, чтобы
      // зазор не зависел от позиции прокрутки.
      const scrollY = window.scrollY || document.documentElement.scrollTop || 0;
      const feedTop = feedRoot.getBoundingClientRect().top + scrollY;
      const headerBottom = sticky.getBoundingClientRect().bottom;
      const offset = headerBottom - feedTop;
      feedRoot.style.setProperty("--feed-header-h", `${Math.max(0, Math.ceil(offset))}px`);
      // Абсолютный низ fixed-шапки от viewport — для position:fixed чата (без safe-top + offset).
      feedRoot.style.setProperty("--feed-sticky-bottom", `${Math.max(0, Math.ceil(headerBottom))}px`);
    };
    write();
    const ro = new ResizeObserver(write);
    ro.observe(sticky);
    const onLoad = () => write();
    window.addEventListener("load", onLoad);
    requestAnimationFrame(write);
    return () => {
      ro.disconnect();
      window.removeEventListener("load", onLoad);
      feedRoot.style.removeProperty("--feed-header-h");
      feedRoot.style.removeProperty("--feed-sticky-bottom");
    };
  }, [sub, feedCategoryIds.length, feedScope, feedTypeFilter]);

  useEffect(() => {
    let raf = 0;

    const readViewport = () => {
      raf = 0;
      const vv = window.visualViewport;
      // Layout viewport (window.innerHeight) НЕ ужимается при открытии клавиатуры на iOS —
      // это именно то, что нужно для max-height карточки треда: размер карточки должен
      // остаться стабильным, пока пользователь печатает. Если использовать vv.height,
      // карточка резко сжимается в момент тапа по textarea и iOS не успевает корректно
      // докрутить поле над клавиатурой.
      const layoutH = Math.max(320, Math.floor(window.innerHeight || vv?.height || 320));
      const bottomNavH = Math.ceil(
        document.querySelector<HTMLElement>(".bottom-nav")?.getBoundingClientRect().height ?? 0,
      );

      setViewportStyle((prev) => {
        const next: FeedViewportStyle = {
          "--feed-vvh": `${layoutH}px`,
          "--feed-bottom-nav-h": `${bottomNavH}px`,
        };
        if (
          prev["--feed-vvh"] === next["--feed-vvh"] &&
          prev["--feed-bottom-nav-h"] === next["--feed-bottom-nav-h"]
        ) {
          return prev;
        }
        return next;
      });
    };

    const scheduleRead = () => {
      if (raf) return;
      raf = requestAnimationFrame(readViewport);
    };

    readViewport();
    const vv = window.visualViewport;
    vv?.addEventListener("resize", scheduleRead);
    vv?.addEventListener("scroll", scheduleRead);
    window.addEventListener("orientationchange", scheduleRead);
    const tg = window.Telegram?.WebApp as { onEvent?: (e: string, fn: () => void) => void } | undefined;
    tg?.onEvent?.("viewportChanged", scheduleRead);
    return () => {
      if (raf) cancelAnimationFrame(raf);
      vv?.removeEventListener("resize", scheduleRead);
      vv?.removeEventListener("scroll", scheduleRead);
      window.removeEventListener("orientationchange", scheduleRead);
      const tgOff = window.Telegram?.WebApp as { offEvent?: (e: string, fn: () => void) => void } | undefined;
      tgOff?.offEvent?.("viewportChanged", scheduleRead);
    };
  }, []);

  const handlePullRefresh = useCallback(async () => {
    hapticLight();
    newestTsRef.current = "";
    oldestTsRef.current = "";
    setHasMoreOlder(true);
    await Promise.all([syncFeed({ full: true, reset: true }), Promise.resolve(onRefreshAll?.())]);
  }, [syncFeed, onRefreshAll, hapticLight]);

  const ptr = usePullToRefresh({
    onRefresh: handlePullRefresh,
    // Важно завязать и на active: FeedScreen остаётся смонтированным (TabKeepAlive),
    // а touchmove-листенер PTR висит на document. Без проверки active он продолжал
    // глушить горизонтальные свайпы на других вкладках (барабан ачивок в профиле
    // «залипал» — preventDefault съедал первый свайп при scrollY===0).
    enabled: active && sub === "activity",
  });

  const ptrStatusText =
    ptr.status === "refreshing"
      ? "Обновление…"
      : ptr.status === "ready"
        ? "Отпусти, чтобы обновить"
        : ptr.status === "pulling"
          ? "Тяни вниз"
          : "";

  return (
    <div className="feed" style={viewportStyle}>
      {catListOpen && sub === "activity" && (
        <button
          type="button"
          className="feed__cat-list-dismiss"
          aria-label="Применить фильтр типов"
          onClick={applyCatListAndClose}
        />
      )}
      <div className="feed__sticky" ref={feedHeaderRef}>
        <div className="feed__sticky-inner">
        <header className="feed__header">
          <div className="feed__brand">Fat Leopard</div>
          <div className="feed__hero">
            <div className="feed__streak" aria-label={streakStreakAriaLabel(streak)} title={streakStreakAriaLabel(streak)}>
              <span className="feed__streak-row">
                <span className="feed__streak-word">Стрик</span>
                <span className="feed__streak-num">{streak}</span>
              </span>
            </div>
          </div>
        </header>
        {sub === "activity" && (
          <div className="feed__filters" aria-label="Фильтры ленты" data-no-ptr>
            {/* Единый ряд фильтров: тип контента (Тренировки/Сообщения) и «чьи»
                (Друзья/Мои) — независимые измерения, работают как пересечение (И).
                Напр. «Друзья» + «Сообщения» → сообщения только друзей. «Все» —
                сброс обоих измерений; активна, когда ни один фильтр не выбран. */}
            <div className="feed__filter-scope" role="group" aria-label="Фильтры ленты">
              <button
                type="button"
                className={`feed__filter-pill${
                  feedTypeFilter === "all" && feedScope === "all" ? " is-active" : ""
                }`}
                onClick={() => {
                  hapticLight();
                  setFeedTypeFilter("all");
                  setFeedScope("all");
                  setCatListOpen(false);
                }}
              >
                Все
              </button>
              <button
                type="button"
                className={`feed__filter-pill${feedScope === "mine" ? " is-active" : ""}`}
                onClick={() => {
                  hapticLight();
                  setFeedScope((p) => (p === "mine" ? "all" : "mine"));
                }}
              >
                Мои
              </button>
              <button
                type="button"
                className={`feed__filter-pill${feedScope === "friends" ? " is-active" : ""}`}
                onClick={() => {
                  hapticLight();
                  setFeedScope((p) => (p === "friends" ? "all" : "friends"));
                }}
              >
                Друзья
              </button>
              <button
                type="button"
                className={`feed__filter-pill feed__filter-pill--type${feedTypeFilter === "training" ? " is-active" : ""}`}
                onClick={() => {
                  hapticLight();
                  setFeedTypeFilter((p) => (p === "training" ? "all" : "training"));
                }}
              >
                Тренировки
              </button>
              <button
                type="button"
                className={`feed__filter-pill feed__filter-pill--type${feedTypeFilter === "message" ? " is-active" : ""}`}
                onClick={() => {
                  hapticLight();
                  setFeedTypeFilter((p) => {
                    const next = p === "message" ? "all" : "message";
                    if (next === "message") setCatListOpen(false);
                    return next;
                  });
                }}
              >
                Сообщения
              </button>
            </div>
            <div
              className={`feed__filter-cats-wrap${
                feedTypeFilter === "message" ? " feed__filter-cats-wrap--collapsed" : ""
              }`}
              aria-hidden={feedTypeFilter === "message"}
              {...(feedTypeFilter === "message" ? { inert: true as const } : {})}
            >
              <div className="feed__filter-cats-clip">
              <div className="feed__filter-cats" role="group" aria-label="Тип тренировки">
                <div
                  role="button"
                  tabIndex={0}
                  className={`feed__filter-chip feed__filter-chip--all${
                    feedCategoryIds.length === 0 ? " is-active" : ""
                  }${catListOpen ? " is-open" : ""}`}
                  aria-pressed={feedCategoryIds.length === 0}
                  aria-expanded={catListOpen}
                  onClick={toggleAllTypes}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === " ") {
                      e.preventDefault();
                      toggleAllTypes();
                    }
                  }}
                >
                  <span className="feed__filter-chip-label">Все типы</span>
                  <span className="feed__filter-chip-caret" aria-hidden>
                    ▾
                  </span>
                </div>
                {!catListOpen &&
                  feedCategoryChipsVisible.map((c) => {
                    const active = feedCategoryIds.includes(c.id);
                    const count = workoutTypeCounts[c.id] ?? 0;
                    return (
                      <div
                        key={c.id}
                        role="button"
                        tabIndex={0}
                        className={`feed__filter-chip${active ? " is-active" : ""}`}
                        aria-pressed={active}
                        onClick={() => toggleFeedCategory(c.id)}
                        onKeyDown={(e) => {
                          if (e.key === "Enter" || e.key === " ") {
                            e.preventDefault();
                            toggleFeedCategory(c.id);
                          }
                        }}
                        title={count > 0 ? `${c.label}: ${count}` : c.label}
                      >
                        <span className="feed__filter-chip-emoji" aria-hidden>
                          {c.emoji}
                        </span>
                        <span className="feed__filter-chip-label">{c.label}</span>
                        {count > 0 && (
                          <span className="feed__filter-chip-cnt" aria-hidden>
                            {count}
                          </span>
                        )}
                      </div>
                    );
                  })}
              </div>
              </div>
              {catListOpen && (
                <div className="feed__cat-list" role="listbox" aria-label="Все типы тренировок">
                  <button
                    type="button"
                    role="option"
                    aria-selected={feedCategoryIds.length === 0}
                    className={`feed__cat-list-item${feedCategoryIds.length === 0 ? " is-active" : ""}`}
                    onClick={() => void clearFeedCategories()}
                  >
                    <span className="feed__cat-list-emoji" aria-hidden>
                      🏷️
                    </span>
                    <span className="feed__cat-list-label">Все типы</span>
                    {feedCategoryIds.length === 0 && (
                      <span className="feed__cat-list-check" aria-hidden>
                        ✓
                      </span>
                    )}
                  </button>
                  {feedCategoryOptionsByFrequency.map((c) => {
                    const active = feedCategoryIds.includes(c.id);
                    const count = workoutTypeCounts[c.id] ?? 0;
                    return (
                      <button
                        key={c.id}
                        type="button"
                        role="option"
                        aria-selected={active}
                        className={`feed__cat-list-item${active ? " is-active" : ""}`}
                        onClick={() => toggleFeedCategory(c.id)}
                      >
                        <span className="feed__cat-list-emoji" aria-hidden>
                          {c.emoji}
                        </span>
                        <span className="feed__cat-list-label">{c.label}</span>
                        {count > 0 && (
                          <span className="feed__cat-list-cnt" aria-hidden>
                            {count}
                          </span>
                        )}
                        {active && (
                          <span className="feed__cat-list-check" aria-hidden>
                            ✓
                          </span>
                        )}
                      </button>
                    );
                  })}
                </div>
              )}
            </div>
          </div>
        )}
        </div>
      </div>
      <div className="feed__pull-shift">
      {sub === "activity" && (ptr.pull > 0 || ptr.status === "refreshing") ? (
        <div
          className={`feed__ptr feed__ptr--${ptr.status}`}
          style={{ height: `${ptr.pull}px` }}
          aria-hidden
        >
          <span className="feed__ptr-icon">
            {ptr.status === "refreshing" ? "↻" : ptr.status === "ready" ? "↑" : "↓"}
          </span>
          {ptrStatusText ? <span className="feed__ptr-label">{ptrStatusText}</span> : null}
        </div>
      ) : null}
      <div className={`feed__subpane${sub !== "activity" ? " feed__subpane--hidden" : ""}`}>
          <div
            className={`feed__pack-progress feed__pack-progress--plain${packGoalReached ? " is-complete" : ""}${packBonusThemeActive ? " is-bonus" : ""}`}
            aria-label={`Стая: ${packWeeklyProgressLabel(packWorkoutsWeek, packWorkoutsGoal)} тренировок за неделю`}
          >
            <div className="feed__pack-progress-row">
              <span className="feed__pack-progress-title">Неделя стаи</span>
              <div
                className="feed__pack-progress-bar"
                role="progressbar"
                aria-valuenow={Math.max(0, Math.floor(packWorkoutsWeek))}
                aria-valuemin={0}
                aria-valuemax={packWorkoutsGoal > 0 ? packWorkoutsGoal : PACK_WEEKLY_GOAL_DEFAULT}
              >
                {packWeeklyStepFillPcts(packWorkoutsWeek, packWorkoutsGoal).map((fillPct, i) => (
                  <div key={i} className="feed__pack-progress-segment">
                    <div className="feed__pack-progress-segment-fill" style={{ width: `${fillPct}%` }} />
                  </div>
                ))}
              </div>
              <span className="feed__pack-progress-count">{packWeeklyProgressLabel(packWorkoutsWeek, packWorkoutsGoal)}</span>
              <button
                type="button"
                className="feed__pack-progress-help"
                aria-label="Подсказка о недельной цели стаи"
                onClick={() => {
                  hapticLight();
                  showAlert(PACK_WEEKLY_GOAL_HINT);
                }}
              >
                ?
              </button>
            </div>
            {packGoalReached ? (
              <span className="feed__pack-progress-bonus">
                {packBonusThemeActive
                  ? "Цель достигнута · +50 кубков каждому · бонусная тема активна"
                  : "Цель достигнута · +50 кубков каждому"}
              </span>
            ) : null}
          </div>
          {err && <p className="feed__err">{err}</p>}
          {loading && <p className="feed__load muted">Загрузка…</p>}
          <div className={`feed__list${listSwapClass ? ` ${listSwapClass}` : ""}`}>
            {!loading && !useMockFeed && feedItems.length === 0 && !err && (
              <p className="feed__empty muted">Пока нет отчётов в базе (или нет MONETIZED_CHAT_ID).</p>
            )}
            {!loading &&
              !useMockFeed &&
              feedItems.length > 0 &&
              visibleFeedItems.length === 0 &&
              !err && (
                <p className="feed__empty muted">
                  {feedScope === "friends"
                    ? "Здесь отчёты друзей. Подпишись на участников в профиле → «Друзья по стае»."
                    : "Ничего не подходит под фильтры — выбери другой тип или «Все типы» / «Все»."}
                </p>
              )}
            {useMockFeed &&
              mockFallback(streak, !apiBase ? "no-api" : !inTelegram ? "no-telegram" : "no-initdata").map((c, i) => (
                <div key={`mock-${i}`} className="feed__card-slot feed__card-slot--them">
                  <ActivityCard {...c} />
                </div>
              ))}
            {!useMockFeed &&
              orderedFeedItems.map((it) => {
                const base = dtoToCard(it);
                const isAdminAnnouncement = it.type === "admin_post" || it.type === "admin_poll";
                const isPinnedAnnouncement = Boolean(it.is_pinned) && isAdminAnnouncement;
                const canPinCard = isAdmin && isAdminAnnouncement;
                const adminPostText = it.type === "admin_post" ? it.text.trim() : undefined;
                // Объявление от Лео (и закреплённое — тоже голос Лео): полный текст без сворачивания.
                const isLeoAnnouncementPost =
                  it.type === "admin_post" &&
                  ((it.username || "").trim() === "Лео" || isPinnedAnnouncement);
                // Мудрость дня читают целиком: это короткий текст дня, и «Показать
                // полностью» на нём — лишний шаг ради двух строк.
                const alwaysFullComment = it.type === "daily_wisdom" || isLeoAnnouncementPost;
                // Закреп — исключительно голос Лео: единый аватар/имя независимо от автора поста.
                const pinnedLeoProps: Partial<ActivityCardProps> = isPinnedAnnouncement
                  ? {
                      name: "Лео",
                      avatar: LEO_AVATAR_URL,
                      activity: "Объявление",
                      emoji: "🐆",
                      hideStreak: true,
                    }
                  : {};
                const supportsThread = feedSupportsThread(it.type);
                // Сообщение общего чата в единой ленте: реакции есть, тред (комменты) — Фаза 4.
                const isMessage = isFeedMessage(it);
                const supportsReactions =
                  supportsThread ||
                  isMessage ||
                  it.type === "pack_join" ||
                  it.type === "pack_rejoin" ||
                  it.type === "pack_removed" ||
                  it.type === "daily_wisdom";
                const isLeoSystemFeed =
                  it.type === "pack_join" ||
                  it.type === "pack_rejoin" ||
                  it.type === "daily_wisdom" ||
                  it.type === "pack_removed" ||
                  it.type === "admin_post" ||
                  it.type === "admin_poll" ||
                  it.type === "inactive_notice";
                const slotClass = `feed__card-slot${it.is_you && !isLeoSystemFeed ? " feed__card-slot--mine" : " feed__card-slot--them"}`;
                const threadReplies = (it.thread ?? []).map((tr) => {
                  const rq = mapFeedThreadQuote(tr);
                  return {
                    id: tr.id,
                    author: (tr.username || "").trim() || `Участник ${tr.user_id}`,
                    text: tr.text,
                    timeAgo: formatLocalDateTime(tr.created_at),
                    isYou: tr.is_you || (userId > 0 && tr.user_id === userId),
                    isLeo: Boolean(tr.is_leo),
                    isAdmin: Boolean(tr.is_admin),
                    adminName: tr.admin_name?.trim() || undefined,
                    authorPhotoUrl: tr.author_photo_url?.trim()
                      ? resolveFeedAvatarUrl(tr.author_photo_url.trim())
                      : undefined,
                    photoUrl: resolveTrainingPhotoUrl(tr.photo_url),
                    replyTo: rq,
                    likeCount: tr.like_count ?? 0,
                    likeMe: Boolean(tr.like_me),
                    likeVoters: Array.isArray(tr.like_voters) ? tr.like_voters : undefined,
                    edited: Boolean(tr.edited_at),
                  };
                });
                // Жалобы/удаление/редактирование сообщений идут через другие эндпоинты
                // (pack-group) — включим в Фазе 4; пока у сообщений только реакции.
                const canReportCard = !it.is_you && !isLeoSystemFeed && !isMessage;
                const canAdminDeleteCard = isAdmin && !isMessage;
                const isOwnPost = it.is_you || (userId > 0 && it.user_id === userId);
                const canEditPost = feedPostEditable(it.type, isOwnPost) && !isMessage;
                const postEditProps: Partial<ActivityCardProps> = canEditPost
                  ? {
                      onStartEditPost: () => {
                        setPostEditId(it.id);
                        setPostEditDrafts((d) => ({
                          ...d,
                          [it.id]: extractEditableFeedPostText(it.text, it.type),
                        }));
                      },
                      commentEdited: Boolean(it.edited_at),
                      ...(postEditId === it.id
                        ? {
                            postEdit: {
                              draft:
                                postEditDrafts[it.id] ?? extractEditableFeedPostText(it.text, it.type),
                              onDraftChange: (v: string) =>
                                setPostEditDrafts((d) => ({ ...d, [it.id]: v })),
                              onSave: () => void editFeedPost(it),
                              onCancel: () => {
                                setPostEditId((cur) => (cur === it.id ? null : cur));
                                setPostEditDrafts((d) => {
                                  const next = { ...d };
                                  delete next[it.id];
                                  return next;
                                });
                              },
                              posting: postEditPosting[it.id] ?? false,
                              photoUrl: resolveTrainingPhotoUrl(it.training_photo_url),
                              onAttachPhoto: (file: File) => void setFeedPostPhoto(it, file),
                              onRemovePhoto: it.training_photo_url
                                ? () => void removeFeedPostPhoto(it)
                                : undefined,
                              photoBusy: postPhotoBusy[it.id] ?? false,
                            },
                          }
                        : {}),
                    }
                  : {};
                // Подписка возможна только на реального участника стаи (не свой пост, не системная карточка Лео).
                const canFollowAuthor = !it.is_you && !isLeoSystemFeed && it.user_id > 0;
                const followProps: Partial<ActivityCardProps> = canFollowAuthor
                  ? {
                      onToggleFollow: () => void toggleFollowAuthor(it.user_id, Boolean(it.is_friend)),
                      isFriend: Boolean(it.is_friend),
                      followPosting: followPosting[it.user_id] ?? false,
                    }
                  : {};
                if (isMessage) {
                  // Сообщение чата в ленте: реакции + фото + комменты (тред из ответов).
                  return (
                    <div key={feedItemKey(it)} className={slotClass}>
                      <ActivityCard
                        {...base}
                        {...followProps}
                        reactions={mergeFeedReactionsForType("pack_message", it.reactions)}
                        onReactionClick={(emoji) => void postMessageReact(it.id, emoji)}
                        onReport={!it.is_you ? () => void confirmReportMessage(it.id) : undefined}
                        reportPosting={feedReportPosting[it.id] ?? false}
                        onAdminDelete={it.is_you || isAdmin ? () => void deleteMessageCard(it.id) : undefined}
                        adminDeletePosting={feedDeletePosting[it.id] ?? false}
                        threadReplies={threadReplies}
                        onThreadReplyDelete={(rid) => void deleteMessageComment(rid)}
                        onThreadReplyEdit={(rid, text) => {
                          setMsgThreadEdit((r) => ({ ...r, [it.id]: { replyId: rid, originalText: text } }));
                          setMsgThreadDrafts((d) => ({ ...d, [it.id]: text }));
                          setMsgThreadReplyTargets((r) => ({ ...r, [it.id]: undefined }));
                        }}
                        onThreadReplyReport={(rid) => void confirmReportMessage(rid)}
                        threadReplyDeleting={threadReplyDeleting}
                        threadReplyReporting={threadReplyReporting}
                        isAdmin={isAdmin}
                        threadReplyIntent={msgThreadReplyTargets[it.id] ?? null}
                        onCancelThreadReplyIntent={() =>
                          setMsgThreadReplyTargets((r) => ({ ...r, [it.id]: undefined }))
                        }
                        onThreadReplyIntent={(payload) => {
                          setMsgThreadEdit((r) => ({ ...r, [it.id]: undefined }));
                          setMsgThreadReplyTargets((r) => ({
                            ...r,
                            [it.id]: {
                              replyToThreadId: payload.replyToThreadId,
                              authorLabel: payload.authorLabel,
                              excerpt: payload.excerpt,
                            },
                          }));
                        }}
                        threadComposer={{
                          draft: msgThreadDrafts[it.id] ?? "",
                          onDraftChange: (v) => setMsgThreadDrafts((d) => ({ ...d, [it.id]: v })),
                          onSubmit: (text, photo) => {
                            const edit = msgThreadEdit[it.id];
                            if (edit) {
                              void editMessageComment(it.id, edit.replyId, text);
                              return;
                            }
                            void postMessageComment(
                              it.id,
                              text,
                              photo,
                              msgThreadReplyTargets[it.id]?.replyToThreadId,
                            );
                          },
                          posting: msgThreadPosting[it.id] ?? false,
                          editReplyId: msgThreadEdit[it.id]?.replyId,
                          onCancelEdit: () => {
                            setMsgThreadEdit((r) => ({ ...r, [it.id]: undefined }));
                            setMsgThreadDrafts((d) => ({ ...d, [it.id]: "" }));
                          },
                        }}
                      />
                    </div>
                  );
                }
                if (!supportsThread) {
                  return (
                    <div key={feedItemKey(it)} className={`${slotClass}${isPinnedAnnouncement ? " feed__card-slot--pinned" : ""}`}>
                      <ActivityCard
                        {...base}
                        {...pinnedLeoProps}
                        {...followProps}
                        {...postEditProps}
                        comment={adminPostText ?? base.comment}
                        pinned={isPinnedAnnouncement}
                        commentAlwaysFull={alwaysFullComment}
                        onTogglePin={canPinCard ? () => void setFeedPostPinned(it.id, !it.is_pinned) : undefined}
                        pinPosting={feedPinPosting[it.id] ?? false}
                        reactions={
                          supportsReactions
                            ? mergeFeedReactionsForType(it.type, it.reactions, it.type === "training_done" ? it.text : undefined)
                            : undefined
                        }
                        onReactionClick={
                          supportsReactions
                            ? (emoji) =>
                                isMessage
                                  ? void postMessageReact(it.id, emoji)
                                  : void postTrainingReact(it.id, emoji)
                            : undefined
                        }
                        onReport={canReportCard ? () => confirmReportPublication(it.id) : undefined}
                        reportPosting={feedReportPosting[it.id] ?? false}
                        onAdminDelete={canAdminDeleteCard ? () => void hideFeedPost(it.id) : undefined}
                        adminDeletePosting={feedDeletePosting[it.id] ?? false}
                        poll={
                          it.poll
                            ? {
                                totalVotes: it.poll.total_votes ?? 0,
                                options: (it.poll.options ?? []).map((option, optionIndex) => ({
                                  label: option.label,
                                  votes: option.votes,
                                  selected: it.poll?.my_vote_index === optionIndex,
                                })),
                                onVote: (optionIndex) => void voteFeedPoll(it.id, optionIndex),
                              }
                            : undefined
                        }
                      />
                    </div>
                  );
                }
                return (
                  <div key={feedItemKey(it)} className={`${slotClass}${isPinnedAnnouncement ? " feed__card-slot--pinned" : ""}`}>
                    <ActivityCard
                      {...base}
                      {...pinnedLeoProps}
                      {...followProps}
                      {...postEditProps}
                      comment={adminPostText ?? base.comment}
                      pinned={isPinnedAnnouncement}
                      commentAlwaysFull={alwaysFullComment}
                      onTogglePin={canPinCard ? () => void setFeedPostPinned(it.id, !it.is_pinned) : undefined}
                      pinPosting={feedPinPosting[it.id] ?? false}
                      poll={
                        it.poll
                          ? {
                              totalVotes: it.poll.total_votes ?? 0,
                              options: (it.poll.options ?? []).map((option, optionIndex) => ({
                                label: option.label,
                                votes: option.votes,
                                selected: it.poll?.my_vote_index === optionIndex,
                              })),
                              onVote: (optionIndex) => void voteFeedPoll(it.id, optionIndex),
                            }
                          : undefined
                      }
                      reactions={mergeFeedReactionsForType(it.type, it.reactions, it.type === "training_done" ? it.text : undefined)}
                      onReactionClick={(emoji) => void postTrainingReact(it.id, emoji)}
                      hasUnreadThread={unreadFeedCardIds.has(it.id)}
                      onThreadOpened={() => markFeedCardThreadRead(it.id)}
                      onLeoReplyDisplayed={(replyId) => reportLeoCommentDisplayed(initData, replyId)}
                      threadReplies={threadReplies}
                      onThreadReplyDelete={(replyId) => void deleteTrainingThreadReply(it.id, replyId)}
                      onThreadReplyLike={(replyId) => void toggleTrainingThreadLike(it.id, replyId)}
                      onThreadReplyEdit={(replyId, text) => {
                        setThreadEditTargets((r) => ({
                          ...r,
                          [it.id]: { threadReplyId: replyId, originalText: text },
                        }));
                        setThreadDrafts((d) => ({ ...d, [it.id]: text }));
                        setThreadReplyTargets((r) => ({ ...r, [it.id]: undefined }));
                      }}
                      threadReplyDeleting={threadReplyDeleting}
                      isAdmin={isAdmin}
                      adminVoiceAvailable={isAdmin && isAdminAnnouncement}
                      threadReplyIntent={threadReplyTargets[it.id] ?? null}
                      onCancelThreadReplyIntent={() =>
                        setThreadReplyTargets((r) => ({ ...r, [it.id]: undefined }))
                      }
                      onThreadReplyIntent={(payload) =>
                        setThreadReplyTargets((r) => ({
                          ...r,
                          [it.id]: {
                            replyToThreadId: payload.replyToThreadId,
                            authorLabel: payload.authorLabel,
                            excerpt: payload.excerpt,
                          },
                        }))
                      }
                      threadComposer={{
                        draft: threadDrafts[it.id] ?? "",
                        onDraftChange: (v) => setThreadDrafts((d) => ({ ...d, [it.id]: v })),
                        onSubmit: (text, photo, postAs) => {
                          const edit = threadEditTargets[it.id];
                          if (edit) {
                            void editTrainingThreadReply(
                              it.id,
                              edit.threadReplyId,
                              text,
                              edit.originalText,
                            );
                            return;
                          }
                          void postTrainingThread(
                            it.id,
                            text,
                            threadReplyTargets[it.id]?.replyToThreadId,
                            photo,
                            postAs,
                          );
                        },
                        posting: threadPosting[it.id] ?? false,
                        editReplyId: threadEditTargets[it.id]?.threadReplyId,
                        onCancelEdit: () => {
                          setThreadEditTargets((r) => ({ ...r, [it.id]: undefined }));
                          setThreadDrafts((d) => ({ ...d, [it.id]: "" }));
                        },
                      }}
                      onReport={canReportCard ? () => confirmReportPublication(it.id) : undefined}
                      reportPosting={feedReportPosting[it.id] ?? false}
                      onAdminDelete={canAdminDeleteCard ? () => void hideFeedPost(it.id) : undefined}
                      adminDeletePosting={feedDeletePosting[it.id] ?? false}
                      onThreadReplyReport={(replyId) => confirmReportComment(it.id, replyId)}
                      threadReplyReporting={threadReplyReporting}
                    />
                  </div>
                );
              })}
            {!useMockFeed && !loading && feedItems.length > 0 && (
              <div ref={olderSentinelRef} className="feed__older">
                {loadingOlder ? (
                  <p className="feed__load muted">Загрузка истории…</p>
                ) : hasMoreOlder ? (
                  <button
                    type="button"
                    className="feed__load-more"
                    onClick={() => void loadOlder()}
                  >
                    Показать ещё
                  </button>
                ) : (
                  <p className="feed__load muted">Это начало истории стаи</p>
                )}
              </div>
            )}
          </div>
      </div>
      </div>
    </div>
  );
}
