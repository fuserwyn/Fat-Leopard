import { useCallback, useEffect, useRef, useState, type ChangeEvent } from "react";
import { formatChatTime, timeAgoFromISO } from "../lib/timeAgo";
import { resolveTrainingPhotoUrl } from "../lib/packFeed";
import { tgConfirm } from "../lib/tgConfirm";
import {
  fetchAdminHidden,
  fetchAdminOverview,
  fetchAdminPackGoal,
  fetchAdminPaywallPrice,
  fetchAdminReports,
  fetchAdminSupportInbox,
  fetchAdminSupportThread,
  fetchAdminUserCard,
  fetchAdminUsers,
  publishAdminPost,
  resetAdminPackGoal,
  resetAdminPaywallPrice,
  restoreAdminHidden,
  saveAdminPackGoal,
  saveAdminPaywallPrice,
  sendAdminReportAction,
  sendAdminSupportReply,
  sendAdminSupportReplyPhoto,
  sendAdminUserAction,
  setAdminUserStat,
  type AdminHiddenItem,
  type AdminOverview,
  type AdminPackGoal,
  type AdminPaywallPrice,
  type AdminReport,
  type AdminSupportConv,
  type AdminSupportMsg,
  type AdminStatField,
  type AdminUserAction,
  type AdminUserCard,
  type AdminUserFilter,
  type AdminUserRow,
  adminSickLeaveActive,
  formatAdminSickLeave,
} from "../lib/adminApi";
import { AdminDashboardScreen } from "./AdminDashboardScreen";
import { AdminDataScreen } from "./AdminDataScreen";
import { AdminOpsScreen, type AdminOpsSection } from "./AdminOpsScreen";
import { AdminResourcesScreen } from "./AdminResourcesScreen";
import { LeoLabScreen } from "./LeoLabScreen";
import { LeoPromptsScreen } from "./LeoPromptsScreen";
import { LEO_AVATAR_URL } from "../lib/leoAvatar";
import { TrackerScreen } from "./TrackerScreen";
import { CameraButton } from "./CameraButton";
import { PhotoLightbox } from "./PhotoLightbox";
import "./AdminScreen.css";

type Page =
  | "home"
  | "support"
  | "thread"
  | "reports"
  | "hidden"
  | "users"
  | "card"
  | "announce"
  | "price"
  | "packgoal"
  | "leolab"
  | "prompts"
  | "dashboards"
  | AdminOpsSection;

/** Вкладки админского таббара: у админа свои разделы, пользовательские тут ни к чему. */
type AdminTab = "community" | "leo" | "system" | "data" | "resources" | "tracker";

type AdminTabDef = {
  key: AdminTab;
  label: string;
  icon?: string;
  avatar?: string;
};

const ADMIN_TABS: AdminTabDef[] = [
  { key: "community", icon: "👥", label: "Community" },
  { key: "leo", avatar: LEO_AVATAR_URL, label: "Leo" },
  { key: "system", icon: "⚙️", label: "System" },
  { key: "data", icon: "🗄", label: "Data" },
  { key: "resources", icon: "💵", label: "Resources" },
  { key: "tracker", icon: "🗂", label: "Tracker" },
];

type Props = {
  initData: string;
  inTelegram: boolean;
  showAlert: (m: string) => void;
  onClose: () => void;
  /** Админ поменял недельную цель стаи — обновить прогресс-бар в ленте без перезагрузки. */
  onPackGoalChange?: (goal: number) => void;
};

const EMPTY_OVERVIEW: AdminOverview = {
  users: 0,
  users_active: 0,
  users_kicked: 0,
  reports_open: 0,
  support_waiting: 0,
  hidden: 0,
  payments: 0,
  access_price_rub: 0,
};

const USERS_PAGE_SIZE = 20;

function clip(text: string, n: number) {
  const t = text.trim();
  if (t.length <= n) return t;
  return `${t.slice(0, n - 1)}…`;
}

function userLabel(name?: string, username?: string, id?: number) {
  const dn = name?.trim();
  if (dn) return dn;
  const un = username?.trim();
  if (un) return un.startsWith("@") ? un : `@${un}`;
  return id ? `id ${id}` : "user";
}

function reportKind(type: string) {
  switch (type) {
    case "thread_reply":
      return "comment";
    case "pack_group_message":
      return "pack chat";
    default:
      return "post";
  }
}

function hiddenKind(kind: string) {
  switch (kind) {
    case "thread_reply":
      return "comment";
    case "pack_group_message":
      return "pack chat";
    default:
      return "post";
  }
}

export function AdminScreen({ initData, inTelegram, showAlert, onClose, onPackGoalChange }: Props) {
  const sheetRef = useRef<HTMLDivElement>(null);
  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;
  const closingRef = useRef(false);
  const closeSheet = useCallback(() => {
    if (closingRef.current) return;
    closingRef.current = true;
    const el = document.activeElement;
    if (el instanceof HTMLElement) el.blur();
    const sheet = sheetRef.current;
    if (!sheet) {
      onCloseRef.current();
      return;
    }
    sheet.style.transition = "transform 0.18s ease-in";
    sheet.style.transform = "translateY(105%)";
    window.setTimeout(() => onCloseRef.current(), 170);
  }, []);

  const [page, setPage] = useState<Page>("home");
  // Кик — единственное необратимое действие в карточке, поэтому подтверждаем
  // его своей модалкой: нативный confirm Telegram на части клиентов молча
  // не показывается, и участник улетал из стаи с одного тапа.
  const [kickAsk, setKickAsk] = useState(false);
  /** Что и на сколько правим в карточке: кубки, стрик, рекорд, тренировки. */
  const [statValue, setStatValue] = useState<Record<AdminStatField, string>>({
    cups: "",
    streak: "",
    record: "",
    workouts: "1",
  });
  const [tab, setTab] = useState<AdminTab>("community");
  const [overview, setOverview] = useState<AdminOverview>(EMPTY_OVERVIEW);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);

  const [convs, setConvs] = useState<AdminSupportConv[]>([]);
  const [threadUser, setThreadUser] = useState<AdminSupportConv | null>(null);
  const [thread, setThread] = useState<AdminSupportMsg[]>([]);
  const [reply, setReply] = useState("");
  const [replyPhoto, setReplyPhoto] = useState<File | null>(null);
  const [replyPhotoPreview, setReplyPhotoPreview] = useState<string | null>(null);
  const [lightboxUrl, setLightboxUrl] = useState<string | null>(null);
  const replyPhotoInputRef = useRef<HTMLInputElement | null>(null);

  const [reports, setReports] = useState<AdminReport[]>([]);
  const [hidden, setHidden] = useState<AdminHiddenItem[]>([]);

  const [users, setUsers] = useState<AdminUserRow[]>([]);
  const [userQuery, setUserQuery] = useState("");
  const [userFilter, setUserFilter] = useState<AdminUserFilter>("all");
  const [userOffset, setUserOffset] = useState(0);
  const [usersTotal, setUsersTotal] = useState(0);
  const [usersActive, setUsersActive] = useState(0);
  const [usersKicked, setUsersKicked] = useState(0);
  const [card, setCard] = useState<AdminUserCard | null>(null);

  const [announce, setAnnounce] = useState("");
  const [announceAuthor, setAnnounceAuthor] = useState<"admin" | "leo">("admin");

  const [price, setPrice] = useState<AdminPaywallPrice | null>(null);
  const [priceInput, setPriceInput] = useState("");
  const [packGoal, setPackGoal] = useState<AdminPackGoal | null>(null);
  const [packGoalInput, setPackGoalInput] = useState("");

  const loadOverview = useCallback(async () => {
    if (!inTelegram || !initData) return;
    try {
      const j = await fetchAdminOverview(initData);
      setOverview(j.overview);
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Couldn't load admin");
    } finally {
      setLoading(false);
    }
  }, [inTelegram, initData, showAlert]);

  useEffect(() => {
    void loadOverview();
  }, [loadOverview]);

  const openSupport = async () => {
    setPage("support");
    try {
      const j = await fetchAdminSupportInbox(initData);
      setConvs(j.conversations ?? []);
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Couldn't load support");
    }
  };

  const openThread = async (c: AdminSupportConv) => {
    setThreadUser(c);
    setPage("thread");
    setReply("");
    clearReplyPhoto();
    try {
      const j = await fetchAdminSupportThread(initData, c.user_id);
      setThread(j.messages ?? []);
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Couldn't open thread");
    }
  };

  const clearReplyPhoto = useCallback(() => {
    setReplyPhoto(null);
    setReplyPhotoPreview((prev) => {
      if (prev) URL.revokeObjectURL(prev);
      return null;
    });
    if (replyPhotoInputRef.current) replyPhotoInputRef.current.value = "";
  }, []);

  const onPickReplyPhoto = useCallback((e: ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0];
    if (!f) return;
    setReplyPhoto(f);
    setReplyPhotoPreview((prev) => {
      if (prev) URL.revokeObjectURL(prev);
      return URL.createObjectURL(f);
    });
  }, []);

  useEffect(() => {
    return () => {
      if (replyPhotoPreview) URL.revokeObjectURL(replyPhotoPreview);
    };
  }, [replyPhotoPreview]);

  const sendReply = async () => {
    if (!threadUser || busy) return;
    const text = reply.trim();
    const sentPhoto = replyPhoto;
    if (!text && !sentPhoto) return;
    setBusy(true);
    try {
      if (sentPhoto) {
        await sendAdminSupportReplyPhoto(initData, threadUser.user_id, text, sentPhoto);
      } else {
        await sendAdminSupportReply(initData, threadUser.user_id, text);
      }
      setReply("");
      clearReplyPhoto();
      const j = await fetchAdminSupportThread(initData, threadUser.user_id);
      setThread(j.messages ?? []);
      void loadOverview();
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Couldn't reply");
    } finally {
      setBusy(false);
    }
  };

  const openReports = async () => {
    setPage("reports");
    try {
      const j = await fetchAdminReports(initData);
      setReports(j.reports ?? []);
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Couldn't load reports");
    }
  };

  const actReport = async (id: number, action: "dismiss" | "hide") => {
    if (busy) return;
    if (action === "hide" && !(await tgConfirm("Hide content for everyone and close the report?"))) return;
    setBusy(true);
    try {
      await sendAdminReportAction(initData, id, action);
      setReports((prev) => prev.filter((r) => r.id !== id));
      void loadOverview();
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Couldn't handle report");
    } finally {
      setBusy(false);
    }
  };

  const openHidden = async () => {
    setPage("hidden");
    try {
      const j = await fetchAdminHidden(initData);
      setHidden(j.items ?? []);
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Couldn't load hidden");
    }
  };

  const restoreHidden = async (item: AdminHiddenItem) => {
    if (busy) return;
    if (!(await tgConfirm("Restore this content to feed / chat?"))) return;
    setBusy(true);
    try {
      await restoreAdminHidden(initData, item.kind, item.id);
      setHidden((prev) => prev.filter((h) => !(h.kind === item.kind && h.id === item.id)));
      void loadOverview();
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Couldn't restore");
    } finally {
      setBusy(false);
    }
  };

  const loadUsers = async (query = userQuery, offset = userOffset, filter = userFilter) => {
    try {
      const j = await fetchAdminUsers(initData, query.trim(), offset, filter);
      setUsers(j.users ?? []);
      setUsersTotal(j.total ?? 0);
      setUsersActive(j.users_active ?? 0);
      setUsersKicked(j.users_kicked ?? 0);
      setUserOffset(j.offset ?? offset);
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Couldn't load users");
    }
  };

  const openUsers = async () => {
    setPage("users");
    setUserQuery("");
    setUserFilter("all");
    setUserOffset(0);
    await loadUsers("", 0, "all");
  };

  const setUsersFilter = (filter: AdminUserFilter) => {
    setUserFilter(filter);
    setUserOffset(0);
    void loadUsers(userQuery, 0, filter);
  };

  // Поля показателей всегда показывают текущее состояние: правишь цифру и
  // жмёшь «Сохранить», а не гадаешь, что сейчас в базе.
  useEffect(() => {
    if (!card) return;
    setStatValue({
      cups: String(card.cups),
      streak: String(card.streak_days),
      record: String(card.max_streak_days),
      workouts: "1",
    });
  }, [card?.user_id, card?.cups, card?.streak_days, card?.max_streak_days]);

  const openCard = async (userId: number) => {
    setPage("card");
    try {
      const j = await fetchAdminUserCard(initData, userId);
      setCard(j.user);
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "User not found");
      setPage("users");
    }
  };

  const changeStat = async (field: AdminStatField, mode: "set" | "add", sign = 1) => {
    if (!card || busy) return;
    const raw = Number(statValue[field]);
    if (!Number.isFinite(raw)) {
      showAlert("Enter a number.");
      return;
    }
    setBusy(true);
    try {
      const j = await setAdminUserStat(initData, card.user_id, field, mode, Math.round(raw) * sign);
      setCard(j.user);
      void loadOverview();
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Couldn't update");
    } finally {
      setBusy(false);
    }
  };

  /** Что реально изменилось в полях по сравнению с карточкой. */
  const dirtyStats = (): { field: AdminStatField; value: number }[] => {
    if (!card) return [];
    const pairs: { field: AdminStatField; current: number }[] = [
      { field: "cups", current: card.cups },
      { field: "streak", current: card.streak_days },
      { field: "record", current: card.max_streak_days },
    ];
    const changed: { field: AdminStatField; value: number }[] = [];
    for (const { field, current } of pairs) {
      const value = Math.round(Number(statValue[field]));
      if (Number.isFinite(value) && value >= 0 && value !== current) {
        changed.push({ field, value });
      }
    }
    return changed;
  };

  const statsDirty = dirtyStats().length > 0;

  const saveStats = async () => {
    const changes = dirtyStats();
    if (!card || busy || changes.length === 0) return;
    setBusy(true);
    try {
      let fresh = card;
      for (const { field, value } of changes) {
        const j = await setAdminUserStat(initData, card.user_id, field, "set", value);
        fresh = j.user;
      }
      setCard(fresh);
      void loadOverview();
      showAlert("Saved.");
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Couldn't save");
    } finally {
      setBusy(false);
    }
  };

  const runUserAction = async (action: AdminUserAction) => {
    if (!card || busy) return;
    setBusy(true);
    try {
      await sendAdminUserAction(initData, card.user_id, action);
      const j = await fetchAdminUserCard(initData, card.user_id);
      setCard(j.user);
      void loadOverview();
      if (page === "users" || page === "card") {
        void loadUsers();
      }
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Couldn't complete action");
    } finally {
      setBusy(false);
    }
  };

  const actUser = async (action: AdminUserAction, confirmText: string) => {
    if (!card || busy) return;
    if (!(await tgConfirm(confirmText))) return;
    setBusy(true);
    try {
      await sendAdminUserAction(initData, card.user_id, action);
      const j = await fetchAdminUserCard(initData, card.user_id);
      setCard(j.user);
      void loadOverview();
      void loadUsers();
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Couldn't complete action");
    } finally {
      setBusy(false);
    }
  };

  const applyPackGoal = (g: AdminPackGoal) => {
    setPackGoal(g);
    setPackGoalInput(String(g.goal));
    onPackGoalChange?.(g.goal);
  };

  const openPackGoal = async () => {
    setPage("packgoal");
    try {
      const j = await fetchAdminPackGoal(initData);
      applyPackGoal(j.pack_goal);
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Couldn't load goal");
    }
  };

  const savePackGoal = async () => {
    if (busy) return;
    const n = Number.parseInt(packGoalInput.trim(), 10);
    if (!Number.isFinite(n) || n < 1 || n > 10000) {
      showAlert("Goal must be an integer from 1 to 10000 workouts");
      return;
    }
    const done = packGoal?.workouts_week ?? 0;
    const warn =
      packGoal && !packGoal.goal_reached && n <= done
        ? ` Pack already did ${done} — weekly bonus applies immediately.`
        : "";
    if (!(await tgConfirm(`Set pack goal ${n} workouts per week? Takes effect immediately, from this week.${warn}`))) return;
    setBusy(true);
    try {
      const j = await saveAdminPackGoal(initData, n);
      applyPackGoal(j.pack_goal);
      showAlert(`Pack goal: ${j.pack_goal.goal} workouts per week`);
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Couldn't save goal");
    } finally {
      setBusy(false);
    }
  };

  const resetPackGoal = async () => {
    if (busy || !packGoal) return;
    if (!(await tgConfirm(`Reset goal to default (${packGoal.default_goal})?`))) return;
    setBusy(true);
    try {
      const j = await resetAdminPackGoal(initData);
      applyPackGoal(j.pack_goal);
      showAlert(`Goal reset: ${j.pack_goal.goal} workouts per week`);
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Couldn't reset goal");
    } finally {
      setBusy(false);
    }
  };

  const openPrice = async () => {
    setPage("price");
    try {
      const j = await fetchAdminPaywallPrice(initData);
      setPrice(j.price);
      setPriceInput(String(j.price.amount_rub));
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Couldn't load price");
    }
  };

  const savePrice = async () => {
    if (busy) return;
    const n = Number.parseInt(priceInput.trim(), 10);
    if (!Number.isFinite(n) || n < 1 || n > 100000) {
      showAlert("Price must be an integer from 1 to 100000 ₽");
      return;
    }
    if (!(await tgConfirm(`Set access price ${n} ₽? New payments will use this amount.`))) return;
    setBusy(true);
    try {
      const j = await saveAdminPaywallPrice(initData, n);
      setPrice(j.price);
      setPriceInput(String(j.price.amount_rub));
      setOverview((prev) => ({ ...prev, access_price_rub: j.price.amount_rub }));
      showAlert(`Access price: ${j.price.amount_rub} ₽`);
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Couldn't save price");
    } finally {
      setBusy(false);
    }
  };

  const resetPrice = async () => {
    if (busy || !price) return;
    if (!(await tgConfirm(`Reset price to server default (${price.default_amount_rub} ₽)?`))) return;
    setBusy(true);
    try {
      const j = await resetAdminPaywallPrice(initData);
      setPrice(j.price);
      setPriceInput(String(j.price.amount_rub));
      setOverview((prev) => ({ ...prev, access_price_rub: j.price.amount_rub }));
      showAlert(`Price reset: ${j.price.amount_rub} ₽`);
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Couldn't reset price");
    } finally {
      setBusy(false);
    }
  };

  const publish = async () => {
    const text = announce.trim();
    if (!text || busy) return;
    if (!(await tgConfirm("Publish announcement to the pack feed?"))) return;
    setBusy(true);
    try {
      await publishAdminPost(initData, text, announceAuthor);
      setAnnounce("");
      showAlert("Published to feed");
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Couldn't publish");
    } finally {
      setBusy(false);
    }
  };

  const back = () => {
    if (page === "thread") {
      setPage("support");
      void openSupport();
      return;
    }
    if (page === "card") {
      setPage("users");
      void loadUsers();
      return;
    }
    setPage("home");
  };

  // Свайп шторки вниз = выйти из админки (как у тренировки). Тянуть можно за
  // грабер/шапку или за тело, когда скролл у самого верха. Из полей ввода и
  // внутренних модалок жест не начинаем. Touch + pointer: в мини-аппе и на десктопе.
  useEffect(() => {
    const sheet = sheetRef.current;
    if (!sheet) return;
    let startY = 0;
    let dy = 0;
    let tracking = false;
    let dragging = false;

    const blocked = (target: EventTarget | null) => {
      const hit = target as HTMLElement | null;
      if (hit?.closest("textarea, input, select, [contenteditable='true']")) return true;
      // Редактор фото и просмотр кадра: палец едет по холсту, это не закрытие шторки.
      if (hit?.closest(".admin__close, .admin__back, .admin__modal, .admin__tabs, .tracker-modal, .imged, .tracker-zoom")) return true;
      let el: HTMLElement | null = hit;
      while (el && el !== sheet) {
        const oy = window.getComputedStyle(el).overflowY;
        if ((oy === "auto" || oy === "scroll") && el.scrollTop > 1) return true;
        el = el.parentElement;
      }
      return false;
    };

    const begin = (y: number, target: EventTarget | null) => {
      if (closingRef.current || blocked(target)) return;
      startY = y;
      dy = 0;
      dragging = false;
      tracking = true;
    };

    const move = (y: number, ev?: Event) => {
      if (!tracking) return;
      dy = y - startY;
      if (!dragging) {
        if (dy > 14) {
          dragging = true;
          sheet.style.transition = "none";
          sheet.style.userSelect = "none";
        } else {
          if (dy < -8) tracking = false;
          return;
        }
      }
      ev?.preventDefault();
      sheet.style.transform = `translateY(${Math.max(0, dy)}px)`;
    };

    const finish = () => {
      if (!tracking) return;
      tracking = false;
      if (!dragging) return;
      dragging = false;
      sheet.style.userSelect = "";
      if (dy > 96) {
        closingRef.current = true;
        const active = document.activeElement;
        if (active instanceof HTMLElement) active.blur();
        sheet.style.transition = "transform 0.18s ease-in";
        sheet.style.transform = "translateY(105%)";
        window.setTimeout(() => onCloseRef.current(), 170);
      } else {
        sheet.style.transition = "transform 0.18s ease-out";
        sheet.style.transform = "";
      }
    };

    const onTouchStart = (e: TouchEvent) => {
      const t = e.touches[0];
      if (t) begin(t.clientY, e.target);
    };
    const onTouchMove = (e: TouchEvent) => {
      const t = e.touches[0];
      if (t) move(t.clientY, e);
    };
    const onPointerDown = (e: PointerEvent) => {
      if (e.pointerType === "touch") return;
      if (e.button !== 0) return;
      begin(e.clientY, e.target);
    };
    const onPointerMove = (e: PointerEvent) => {
      if (e.pointerType === "touch") return;
      move(e.clientY, e);
    };

    sheet.addEventListener("touchstart", onTouchStart, { passive: true });
    sheet.addEventListener("touchmove", onTouchMove, { passive: false });
    sheet.addEventListener("touchend", finish);
    sheet.addEventListener("touchcancel", finish);
    sheet.addEventListener("pointerdown", onPointerDown);
    window.addEventListener("pointermove", onPointerMove);
    window.addEventListener("pointerup", finish);
    window.addEventListener("pointercancel", finish);
    return () => {
      sheet.removeEventListener("touchstart", onTouchStart);
      sheet.removeEventListener("touchmove", onTouchMove);
      sheet.removeEventListener("touchend", finish);
      sheet.removeEventListener("touchcancel", finish);
      sheet.removeEventListener("pointerdown", onPointerDown);
      window.removeEventListener("pointermove", onPointerMove);
      window.removeEventListener("pointerup", finish);
      window.removeEventListener("pointercancel", finish);
    };
  }, []);

  const title =
    page === "home"
      ? (ADMIN_TABS.find((t) => t.key === tab)?.label ?? "Admin")
      : page === "support"
        ? "Support"
        : page === "thread"
          ? userLabel(threadUser?.display_name, undefined, threadUser?.user_id)
          : page === "reports"
            ? "Reports"
            : page === "hidden"
              ? "Hidden"
              : page === "users"
                ? "Members"
                : page === "card"
                  ? userLabel(card?.display_name, card?.username, card?.user_id)
                  : page === "price"
                    ? "Access price"
                    : page === "packgoal"
                      ? "Pack goal"
                    : page === "dashboards"
                      ? "Dashboards"
                      : page === "analytics"
                        ? "Analytics"
                        : page === "visits"
                          ? "Bot visits"
                          : page === "payments"
                            ? "Payments & donations"
                            : page === "admins"
                              ? "Admins"
                              : page === "scheduled"
                                ? "Scheduled posts"
                                : page === "poll"
                                  ? "Feed poll"
                                  : page === "leolab"
                                    ? "Leo test"
                                    : page === "prompts"
                                    ? "Fat Leopard prompts"
                                    : page === "wipe"
                                    ? "Clear feed and chat"
                                    : "Announcement";

  return (
    <>
      <div className="admin-backdrop" aria-hidden="true" onClick={closeSheet} />
      <div ref={sheetRef} className="admin" role="dialog" aria-modal="true" aria-label="Admin">
      <div className="admin__grabber" aria-hidden="true" />
      <button type="button" className="admin__close" onClick={closeSheet} aria-label="Close">
        ✕
      </button>
      <header className="admin__head">
        {page !== "home" ? (
          <button type="button" className="admin__back" onClick={back} aria-label="Back">
            ←
          </button>
        ) : null}
        <h1 className="admin__title">{title}</h1>
      </header>

      {page === "home" && tab === "community" && (
        <div className="admin__body">
          {loading ? (
            <p className="admin__muted">Loading…</p>
          ) : (
            <>
              <div className="admin__stats">
                <div className="admin__stat">
                  <strong>{overview.users_active}</strong>
                  <span>active</span>
                </div>
                <div className="admin__stat">
                  <strong>{overview.users_kicked}</strong>
                  <span>kicked</span>
                </div>
                <div className="admin__stat">
                  <strong>{overview.support_waiting}</strong>
                  <span>awaiting reply</span>
                </div>
                <div className="admin__stat">
                  <strong>{overview.reports_open}</strong>
                  <span>reports</span>
                </div>
                <div className="admin__stat">
                  <strong>{overview.hidden}</strong>
                  <span>hidden</span>
                </div>
              </div>
              <div className="admin__tiles">
                <button type="button" className="admin__tile" onClick={() => void openSupport()}>
                  <span className="admin__tile-ico">💬</span>
                  <span className="admin__tile-text">
                    <b>Support</b>
                    <small>{overview.support_waiting > 0 ? `${overview.support_waiting} awaiting reply` : "all answered"}</small>
                  </span>
                </button>
                <button type="button" className="admin__tile" onClick={() => void openReports()}>
                  <span className="admin__tile-ico">🚩</span>
                  <span className="admin__tile-text">
                    <b>Reports</b>
                    <small>{overview.reports_open > 0 ? `${overview.reports_open} open` : "empty"}</small>
                  </span>
                </button>
                <button type="button" className="admin__tile" onClick={() => void openHidden()}>
                  <span className="admin__tile-ico">🙈</span>
                  <span className="admin__tile-text">
                    <b>Hidden</b>
                    <small>restore posts and comments</small>
                  </span>
                </button>
                <button type="button" className="admin__tile" onClick={() => void openUsers()}>
                  <span className="admin__tile-ico">👥</span>
                  <span className="admin__tile-text">
                    <b>Members</b>
                    <small>
                      {overview.users} all-time · {overview.users_active} active · {overview.users_kicked}{" "}
                      kicked
                    </small>
                  </span>
                </button>
                <button type="button" className="admin__tile" onClick={() => setPage("announce")}>
                  <span className="admin__tile-ico">📣</span>
                  <span className="admin__tile-text">
                    <b>Announcement</b>
                    <small>feed post from admin or Leo</small>
                  </span>
                </button>
                <button type="button" className="admin__tile" onClick={() => setPage("poll")}>
                  <span className="admin__tile-ico">🗳</span>
                  <span className="admin__tile-text">
                    <b>Poll</b>
                    <small>question and options to feed</small>
                  </span>
                </button>
                <button type="button" className="admin__tile" onClick={() => setPage("scheduled")}>
                  <span className="admin__tile-ico">📅</span>
                  <span className="admin__tile-text">
                    <b>Scheduled</b>
                    <small>scheduled posts</small>
                  </span>
                </button>
                <button type="button" className="admin__tile" onClick={() => setPage("payments")}>
                  <span className="admin__tile-ico">💳</span>
                  <span className="admin__tile-text">
                    <b>Payments & donations</b>
                    <small>who paid and donated · ⭐ and ₽</small>
                  </span>
                </button>
                <button type="button" className="admin__tile" onClick={() => void openPrice()}>
                  <span className="admin__tile-ico">💰</span>
                  <span className="admin__tile-text">
                    <b>Access price</b>
                    <small>
                      {overview.access_price_rub > 0
                        ? `${overview.access_price_rub} ₽ · join and return`
                        : "what members pay to join and return"}
                    </small>
                  </span>
                </button>
                <button type="button" className="admin__tile" onClick={() => void openPackGoal()}>
                  <span className="admin__tile-ico">🎯</span>
                  <span className="admin__tile-text">
                    <b>Pack goal</b>
                    <small>{packGoal ? `${packGoal.goal} workouts per week` : "workouts per week until bonus"}</small>
                  </span>
                </button>
              </div>
              <p className="admin__muted admin__hint">
                Same commands in bot: /admin. Cups and streaks for a member — in the member card.
              </p>
            </>
          )}
        </div>
      )}

      {page === "home" && tab === "leo" && (
        <div className="admin__body">
          <div className="admin__tiles">
            <button type="button" className="admin__tile" onClick={() => setPage("prompts")}>
              <span className="admin__tile-ico">📜</span>
              <span className="admin__tile-text">
                <b>Fat Leopard prompts</b>
                <small>persona and copy: field or .txt/.md file</small>
              </span>
            </button>
            <button type="button" className="admin__tile" onClick={() => setPage("leolab")}>
              <span className="admin__tile-ico">🧪</span>
              <span className="admin__tile-text">
                <b>Leo test</b>
                <small>ask, try a prompt, teach</small>
              </span>
            </button>
          </div>
          <p className="admin__muted admin__hint">
            Production prompts live in <code>ms_leo/internal/prompts/data/*.txt</code>. Override them in Prompts
            applies immediately, no rebuild; reset restores the built-in repo file.
          </p>
        </div>
      )}

      {page === "home" && tab === "system" && (
        <div className="admin__body">
          <div className="admin__tiles">
                <button type="button" className="admin__tile" onClick={() => setPage("dashboards")}>
                  <span className="admin__tile-ico">📊</span>
                  <span className="admin__tile-text">
                    <b>Dashboards</b>
                    <small>KPI, funnels, and visits</small>
                  </span>
                </button>
                <button type="button" className="admin__tile" onClick={() => setPage("analytics")}>
                  <span className="admin__tile-ico">📈</span>
                  <span className="admin__tile-text">
                    <b>Analytics</b>
                    <small>tables: funnels, channels, events</small>
                  </span>
                </button>
                <button type="button" className="admin__tile" onClick={() => setPage("visits")}>
                  <span className="admin__tile-ico">👣</span>
                  <span className="admin__tile-text">
                    <b>Bot visits</b>
                    <small>who visits and how often</small>
                  </span>
                </button>
                <button type="button" className="admin__tile" onClick={() => setPage("admins")}>
                  <span className="admin__tile-ico">🛡</span>
                  <span className="admin__tile-text">
                    <b>Admins</b>
                    <small>grant and revoke rights</small>
                  </span>
                </button>
                <button type="button" className="admin__tile" onClick={() => setPage("wipe")}>
                  <span className="admin__tile-ico">🗑</span>
                  <span className="admin__tile-text">
                    <b>Clear feed and chat</b>
                    <small>delete all pack content</small>
                  </span>
                </button>
          </div>
          <p className="admin__muted admin__hint">
            Technical sections: product metrics, access, and one-off pack content ops.
          </p>
        </div>
      )}

      {page === "home" && tab === "data" && (
        <div className="admin__body">
          <AdminDataScreen initData={initData} showAlert={showAlert} />
        </div>
      )}

      {page === "home" && tab === "resources" && (
        <div className="admin__body">
          <AdminResourcesScreen initData={initData} showAlert={showAlert} />
        </div>
      )}

      {page === "home" && tab === "tracker" && (
        <div className="admin__body">
          <TrackerScreen initData={initData} showAlert={showAlert} />
        </div>
      )}

      {page === "leolab" && (
        <div className="admin__body">
          <LeoLabScreen initData={initData} showAlert={showAlert} />
        </div>
      )}

      {page === "prompts" && (
        <div className="admin__body">
          <LeoPromptsScreen initData={initData} showAlert={showAlert} />
        </div>
      )}

      {page === "dashboards" && (
        <div className="admin__body">
          <AdminDashboardScreen initData={initData} showAlert={showAlert} />
        </div>
      )}

      {(page === "analytics" ||
        page === "visits" ||
        page === "payments" ||
        page === "admins" ||
        page === "scheduled" ||
        page === "poll" ||
        page === "wipe") && (
        <div className="admin__body">
          <AdminOpsScreen section={page} initData={initData} showAlert={showAlert} />
        </div>
      )}

      {page === "support" && (
        <div className="admin__body">
          {convs.length === 0 ? (
            <p className="admin__muted">No conversations yet</p>
          ) : (
            <ul className="admin__list">
              {convs.map((c) => (
                <li key={c.user_id}>
                  <button type="button" className="admin__row" onClick={() => void openThread(c)}>
                    <div className="admin__row-top">
                      <b>{userLabel(c.display_name, undefined, c.user_id)}</b>
                      {c.needs_reply ? <span className="admin__badge">waiting</span> : null}
                    </div>
                    <p className="admin__row-sub">{clip(c.last_text, 90)}</p>
                    <span className="admin__row-meta">{timeAgoFromISO(c.last_created)}</span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}

      {page === "thread" && threadUser && (
        <div className="admin__thread">
          <div className="admin__thread-log">
            {thread.map((m) => {
              const photoUrl = resolveTrainingPhotoUrl(m.photo_url);
              return (
                <div key={m.id} className={`admin__bubble admin__bubble--${m.role}`}>
                  {photoUrl && (
                    <button
                      type="button"
                      className="admin__bubble-photo-wrap"
                      aria-label="Open photo"
                      onClick={() => setLightboxUrl(photoUrl)}
                    >
                      <img className="admin__bubble-photo" src={photoUrl} alt="" loading="lazy" referrerPolicy="no-referrer" />
                    </button>
                  )}
                  {m.text ? <p>{m.text}</p> : null}
                  <time>{timeAgoFromISO(m.created_at)}</time>
                </div>
              );
            })}
          </div>
          {replyPhotoPreview != null && (
            <div className="admin__reply-photo-pending">
              <img className="admin__reply-photo-pending-img" src={replyPhotoPreview} alt="" />
              <button type="button" className="admin__reply-photo-pending-remove" aria-label="Remove photo" onClick={clearReplyPhoto}>
                ✕
              </button>
            </div>
          )}
          <form
            className="admin__composer"
            onSubmit={(e) => {
              e.preventDefault();
              void sendReply();
            }}
          >
            <input
              ref={replyPhotoInputRef}
              type="file"
              accept="image/*"
              className="admin__reply-photo-input"
              onChange={onPickReplyPhoto}
              tabIndex={-1}
              aria-hidden
            />
            <button
              type="button"
              className="admin__reply-attach"
              aria-label="Attach photo from gallery"
              disabled={busy}
              onClick={() => replyPhotoInputRef.current?.click()}
            >
              📎
            </button>
            <CameraButton className="admin__reply-attach" onChange={onPickReplyPhoto} disabled={busy} />
            <input
              value={reply}
              onChange={(e) => setReply(e.target.value)}
              placeholder={replyPhoto ? "Photo caption (optional)…" : "Reply to user…"}
              maxLength={4000}
            />
            <button type="submit" className="admin__reply-send" disabled={busy || (!reply.trim() && !replyPhoto)}>
              ➤
            </button>
          </form>
        </div>
      )}

      {page === "reports" && (
        <div className="admin__body">
          {reports.length === 0 ? (
            <p className="admin__muted">No open reports</p>
          ) : (
            <ul className="admin__list">
              {reports.map((r) => (
                <li key={r.id} className="admin__card">
                  <div className="admin__row-top">
                    <b>{reportKind(r.target_type)}</b>
                    <span className="admin__row-meta">{timeAgoFromISO(r.created_at)}</span>
                  </div>
                  <p className="admin__row-sub">{clip(r.target_text || "—", 160)}</p>
                  <p className="admin__row-meta">
                    on {userLabel(r.target_name, undefined, r.target_user_id)} · from{" "}
                    {userLabel(r.reporter_name, undefined, r.reporter_user_id)}
                  </p>
                  <div className="admin__actions">
                    <button type="button" className="admin__btn admin__btn--ghost" onClick={() => void actReport(r.id, "dismiss")} disabled={busy}>
                      Close
                    </button>
                    <button type="button" className="admin__btn admin__btn--danger" onClick={() => void actReport(r.id, "hide")} disabled={busy}>
                      Hide
                    </button>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}

      {page === "hidden" && (
        <div className="admin__body">
          {hidden.length === 0 ? (
            <p className="admin__muted">No hidden content</p>
          ) : (
            <ul className="admin__list">
              {hidden.map((h) => (
                <li key={`${h.kind}-${h.id}`} className="admin__card">
                  <div className="admin__row-top">
                    <b>{hiddenKind(h.kind)}</b>
                    <span className="admin__row-meta">{timeAgoFromISO(h.created_at)}</span>
                  </div>
                  <p className="admin__row-sub">{clip(h.text || "—", 160)}</p>
                  <p className="admin__row-meta">{userLabel(h.author_name, undefined, h.author_user_id)}</p>
                  <div className="admin__actions">
                    <button type="button" className="admin__btn" onClick={() => void restoreHidden(h)} disabled={busy}>
                      Restore
                    </button>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}

      {page === "users" && (
        <div className="admin__body">
          <p className="admin__users-summary">
            All-time: <strong>{usersActive + usersKicked}</strong> · active {usersActive} · kicked{" "}
            {usersKicked}
          </p>
          <div className="admin__filters" role="tablist" aria-label="Member filter">
            <button
              type="button"
              role="tab"
              aria-selected={userFilter === "all"}
              className={userFilter === "all" ? "on" : ""}
              onClick={() => setUsersFilter("all")}
            >
              All ({usersActive + usersKicked})
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={userFilter === "active"}
              className={userFilter === "active" ? "on" : ""}
              onClick={() => setUsersFilter("active")}
            >
              Active ({usersActive})
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={userFilter === "kicked"}
              className={userFilter === "kicked" ? "on" : ""}
              onClick={() => setUsersFilter("kicked")}
            >
              Kicked ({usersKicked})
            </button>
          </div>
          <form
            className="admin__search"
            onSubmit={(e) => {
              e.preventDefault();
              setUserOffset(0);
              void loadUsers(userQuery, 0, userFilter);
            }}
          >
            <input
              value={userQuery}
              onChange={(e) => setUserQuery(e.target.value)}
              placeholder="Name, @username, or id"
            />
            <button type="submit">Find</button>
          </form>
          {users.length === 0 ? (
            <p className="admin__muted">No one found</p>
          ) : (
            <>
              <ul className="admin__list">
                {users.map((u) => (
                  <li key={u.user_id}>
                    <button type="button" className="admin__row" onClick={() => void openCard(u.user_id)}>
                      <div className="admin__row-top">
                        <b>{userLabel(u.display_name, u.username, u.user_id)}</b>
                        {u.is_deleted ? <span className="admin__badge admin__badge--muted">kicked</span> : null}
                      </div>
                      <p className="admin__row-meta">
                        {u.cups} cups · streak {u.streak_days}
                        {u.has_active_paywall ? " · access active" : ""}
                      </p>
                    </button>
                  </li>
                ))}
              </ul>
              {!userQuery.trim() && usersTotal > USERS_PAGE_SIZE ? (
                <div className="admin__pager">
                  <button
                    type="button"
                    disabled={userOffset === 0}
                    onClick={() => void loadUsers(userQuery, Math.max(0, userOffset - USERS_PAGE_SIZE), userFilter)}
                  >
                    ◀ Back
                  </button>
                  <span className="admin__muted">
                    {userOffset + 1}–{userOffset + users.length} of {usersTotal}
                  </span>
                  <button
                    type="button"
                    disabled={userOffset + users.length >= usersTotal}
                    onClick={() => void loadUsers(userQuery, userOffset + USERS_PAGE_SIZE, userFilter)}
                  >
                    Next ▶
                  </button>
                </div>
              ) : null}
            </>
          )}
        </div>
      )}

      {page === "card" && card && (
        <div className="admin__body">
          <div className="admin__card">
            <p className="admin__id">
              id {card.user_id}
              {card.username ? ` · @${card.username.replace(/^@/, "")}` : " · no username"}
            </p>
            <dl className="admin__dl">
              <div>
                <dt>Cups</dt>
                <dd>{card.cups}</dd>
              </div>
              <div>
                <dt>Workouts</dt>
                <dd>{card.workouts_total}</dd>
              </div>
              <div>
                <dt>Level</dt>
                <dd>
                  {card.level} · {card.level_name}
                </dd>
              </div>
              <div>
                <dt>Streak</dt>
                <dd>
                  {card.streak_days} (record {card.max_streak_days})
                </dd>
              </div>
              <div>
                <dt>Days without workout</dt>
                <dd>{card.days_since_last_training < 0 ? "—" : `${card.days_since_last_training} d`}</dd>
              </div>
              <div>
                <dt>Sick leave</dt>
                <dd>{formatAdminSickLeave(card.sick_leave)}</dd>
              </div>
              <div>
                <dt>Streak saves</dt>
                <dd>
                  {card.streak_save_attempts_avail}/{card.streak_save_attempts_max}
                </dd>
              </div>
              <div>
                <dt>Access</dt>
                <dd>{card.has_active_paywall ? "paid" : "no"}</dd>
              </div>
              <div>
                <dt>In pack</dt>
                <dd>{card.is_deleted ? "kicked" : "yes"}</dd>
              </div>
            </dl>
            {card.ugc_muted_until ? <p className="admin__warn">UGC mute until {formatChatTime(card.ugc_muted_until)}</p> : null}
            <p className="admin__row-meta">UGC violations: {card.ugc_violations}</p>
          </div>
          <section className="admin__stats-edit">
            <h3 className="admin__stats-edit-title">Stats</h3>
            <p className="admin__muted admin__hint">
              Level and streak saves are derived from cups — you don&apos;t set them separately.
            </p>

            <div className="admin__stat-row">
              <span>Cups</span>
              <input
                inputMode="numeric"
                value={statValue.cups}
                onChange={(e) => setStatValue((p) => ({ ...p, cups: e.target.value }))}
              />
            </div>

            <div className="admin__stat-row">
              <span>Streak</span>
              <input
                inputMode="numeric"
                value={statValue.streak}
                onChange={(e) => setStatValue((p) => ({ ...p, streak: e.target.value }))}
              />
            </div>

            <div className="admin__stat-row">
              <span>Record</span>
              <input
                inputMode="numeric"
                value={statValue.record}
                onChange={(e) => setStatValue((p) => ({ ...p, record: e.target.value }))}
              />
            </div>

            <button
              type="button"
              className="admin__btn admin__btn--wide"
              disabled={busy || !statsDirty}
              onClick={() => void saveStats()}
            >
              {statsDirty ? "Save stats" : "No changes"}
            </button>

            <div className="admin__stat-row">
              <span>Workouts</span>
              <input
                inputMode="numeric"
                value={statValue.workouts}
                onChange={(e) => setStatValue((p) => ({ ...p, workouts: e.target.value }))}
              />
              <button type="button" disabled={busy} onClick={() => void changeStat("workouts", "add")}>
                Credit
              </button>
              <button type="button" disabled={busy} onClick={() => void changeStat("workouts", "add", -1)}>
                Remove
              </button>
            </div>
            <p className="admin__muted admin__hint">
              Workouts are session records: Credit adds today&apos;s session; Remove drops the latest entries.
            </p>
          </section>

          <div className="admin__actions admin__actions--stack">
            {adminSickLeaveActive(card.sick_leave) ? (
              <button type="button" className="admin__btn" disabled={busy} onClick={() => void actUser("sick_cancel", "End sick leave?")}>
                End sick leave
              </button>
            ) : (
              <button type="button" className="admin__btn" disabled={busy || card.is_deleted} onClick={() => void actUser("sick_set", "Set sick leave for this member?")}>
                Set sick leave
              </button>
            )}
            <button type="button" className="admin__btn" disabled={busy} onClick={() => void actUser("grant_save", "Grant +1 streak save?")}>
              +1 streak save
            </button>
            {card.ugc_muted_until ? (
              <button type="button" className="admin__btn" disabled={busy} onClick={() => void actUser("unmute", "Remove UGC mute?")}>
                Unmute
              </button>
            ) : (
              <button type="button" className="admin__btn" disabled={busy} onClick={() => void actUser("mute", "Mute posts for 24 hours?")}>
                Mute 24h
              </button>
            )}
            {card.is_deleted ? (
              <>
                <button type="button" className="admin__btn" disabled={busy} onClick={() => void actUser("restore_full", "Return to pack with progress?")}>
                  Restore with progress
                </button>
                <button type="button" className="admin__btn admin__btn--ghost" disabled={busy} onClick={() => void actUser("restore_scratch", "Return to pack from scratch?")}>
                  Restore from scratch
                </button>
              </>
            ) : (
              <button type="button" className="admin__btn admin__btn--danger" disabled={busy} onClick={() => setKickAsk(true)}>
                Remove from pack
              </button>
            )}
          </div>

          {kickAsk ? (
            <div className="admin__modal" role="dialog" aria-modal="true">
              <div className="admin__modal-box">
                <h3>Remove from pack?</h3>
                <p>
                  {userLabel(card.display_name, card.username, card.user_id)} will lose feed and chat access. Cups and
                  streak stay — restore with Restore with progress.
                </p>
                <div className="admin__modal-actions">
                  <button type="button" onClick={() => setKickAsk(false)}>
                    Cancel
                  </button>
                  <button
                    type="button"
                    className="admin__btn--danger"
                    disabled={busy}
                    onClick={() => {
                      setKickAsk(false);
                      void runUserAction("kick");
                    }}
                  >
                    Remove
                  </button>
                </div>
              </div>
            </div>
          ) : null}
        </div>
      )}

      {page === "announce" && (
        <div className="admin__body">
          <div className="admin__voice" role="group" aria-label="Post as">
            <button type="button" className={announceAuthor === "admin" ? "is-on" : ""} onClick={() => setAnnounceAuthor("admin")}>
              Admin
            </button>
            <button type="button" className={announceAuthor === "leo" ? "is-on" : ""} onClick={() => setAnnounceAuthor("leo")}>
              Leo
            </button>
          </div>
          <textarea
            className="admin__textarea"
            value={announce}
            onChange={(e) => setAnnounce(e.target.value)}
            placeholder="Announcement for the feed…"
            maxLength={4000}
            rows={7}
          />
          <button type="button" className="admin__btn admin__btn--wide" disabled={busy || !announce.trim()} onClick={() => void publish()}>
            Publish
          </button>
        </div>
      )}

      {page === "price" && (
        <div className="admin__body">
          <p className="admin__muted admin__hint">
            One payment for pack entry and for return after removal. Members who already paid don&apos;t pay again.
            Telegram Stars are configured on the server.
          </p>
          <label className="admin__price">
            <span>Price, ₽</span>
            <input
              type="number"
              inputMode="numeric"
              min={1}
              max={100000}
              step={1}
              value={priceInput}
              onChange={(e) => setPriceInput(e.target.value)}
            />
          </label>
          {price ? (
            <p className="admin__row-meta">
              Currently {price.amount_rub} ₽
              {price.is_custom ? " · set by admin" : " · server default"}
              {price.is_custom ? ` · reset restores ${price.default_amount_rub} ₽` : ""}
            </p>
          ) : (
            <p className="admin__muted">Loading…</p>
          )}
          <button type="button" className="admin__btn admin__btn--wide" disabled={busy || !priceInput.trim()} onClick={() => void savePrice()}>
            Save
          </button>
          {price?.is_custom ? (
            <button type="button" className="admin__btn admin__btn--ghost admin__btn--wide" disabled={busy} onClick={() => void resetPrice()}>
              Reset to {price.default_amount_rub} ₽
            </button>
          ) : null}
        </div>
      )}
      {page === "packgoal" && (
        <div className="admin__body">
          <p className="admin__muted admin__hint">
            How many pack workouts per week (from Monday 00:00 MSK) so everyone gets bonus cups and the Pack theme for a
            day. A new goal applies immediately for the current week.
          </p>
          <label className="admin__price">
            <span>Goal, workouts</span>
            <input
              type="number"
              inputMode="numeric"
              min={1}
              max={10000}
              step={1}
              value={packGoalInput}
              onChange={(e) => setPackGoalInput(e.target.value)}
            />
          </label>
          {packGoal ? (
            <p className="admin__row-meta">
              Currently {packGoal.goal}
              {packGoal.is_custom ? " · set by admin" : " · default"}
              {` · this week ${packGoal.workouts_week}/${packGoal.goal}`}
              {packGoal.goal_reached ? " · goal reached" : ""}
            </p>
          ) : (
            <p className="admin__muted">Loading…</p>
          )}
          <button type="button" className="admin__btn admin__btn--wide" disabled={busy || !packGoalInput.trim()} onClick={() => void savePackGoal()}>
            Save
          </button>
          {packGoal?.is_custom ? (
            <button type="button" className="admin__btn admin__btn--ghost admin__btn--wide" disabled={busy} onClick={() => void resetPackGoal()}>
              Reset to {packGoal.default_goal}
            </button>
          ) : null}
        </div>
      )}
      <nav
        className="admin__tabs"
        aria-label="Admin sections"
        style={{ gridTemplateColumns: `repeat(${ADMIN_TABS.length}, minmax(0, 1fr))` }}
      >
        {ADMIN_TABS.map((t) => (
          <button
            key={t.key}
            type="button"
            className={`admin__tab${tab === t.key ? " on" : ""}`}
            aria-current={tab === t.key ? "page" : undefined}
            onClick={() => {
              setTab(t.key);
              setPage("home");
            }}
          >
            {t.avatar ? (
              <img className="admin__tab-avatar" src={t.avatar} width={20} height={20} alt="" aria-hidden />
            ) : (
              <span className="admin__tab-ico" aria-hidden>
                {t.icon}
              </span>
            )}
            <span className="admin__tab-label">{t.label}</span>
          </button>
        ))}
      </nav>
      </div>
      {lightboxUrl && <PhotoLightbox src={lightboxUrl} onClose={() => setLightboxUrl(null)} />}
    </>
  );
}

