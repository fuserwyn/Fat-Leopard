import { useCallback, useEffect, useRef, useState, type ChangeEvent } from "react";
import { formatChatTime, timeAgoFromISO } from "../lib/timeAgo";
import { resolveTrainingPhotoUrl } from "../lib/packFeed";
import { tgConfirm } from "../lib/tgConfirm";
import {
  fetchAdminHidden,
  fetchAdminOverview,
  fetchAdminPaywallPrice,
  fetchAdminReports,
  fetchAdminSupportInbox,
  fetchAdminSupportThread,
  fetchAdminUserCard,
  fetchAdminUsers,
  publishAdminPost,
  resetAdminPaywallPrice,
  restoreAdminHidden,
  saveAdminPaywallPrice,
  sendAdminReportAction,
  sendAdminSupportReply,
  sendAdminSupportReplyPhoto,
  sendAdminUserAction,
  setAdminUserStat,
  type AdminHiddenItem,
  type AdminOverview,
  type AdminPaywallPrice,
  type AdminReport,
  type AdminSupportConv,
  type AdminSupportMsg,
  type AdminStatField,
  type AdminUserAction,
  type AdminUserCard,
  type AdminUserFilter,
  type AdminUserRow,
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
  { key: "community", icon: "👥", label: "Сообщество" },
  { key: "leo", avatar: LEO_AVATAR_URL, label: "Лео" },
  { key: "system", icon: "⚙️", label: "Система" },
  { key: "data", icon: "🗄", label: "Данные" },
  { key: "resources", icon: "💵", label: "Ресурсы" },
  { key: "tracker", icon: "🗂", label: "Трекер" },
];

type Props = {
  initData: string;
  inTelegram: boolean;
  showAlert: (m: string) => void;
  onClose: () => void;
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
  return id ? `id ${id}` : "пользователь";
}

function reportKind(type: string) {
  switch (type) {
    case "thread_reply":
      return "комментарий";
    case "pack_group_message":
      return "чат стаи";
    default:
      return "пост";
  }
}

function hiddenKind(kind: string) {
  switch (kind) {
    case "thread_reply":
      return "комментарий";
    case "pack_group_message":
      return "чат стаи";
    default:
      return "пост";
  }
}

export function AdminScreen({ initData, inTelegram, showAlert, onClose }: Props) {
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

  const loadOverview = useCallback(async () => {
    if (!inTelegram || !initData) return;
    try {
      const j = await fetchAdminOverview(initData);
      setOverview(j.overview);
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Не удалось загрузить админку");
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
      showAlert(e instanceof Error ? e.message : "Не удалось загрузить поддержку");
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
      showAlert(e instanceof Error ? e.message : "Не удалось открыть диалог");
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
      showAlert(e instanceof Error ? e.message : "Не удалось ответить");
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
      showAlert(e instanceof Error ? e.message : "Не удалось загрузить жалобы");
    }
  };

  const actReport = async (id: number, action: "dismiss" | "hide") => {
    if (busy) return;
    if (action === "hide" && !(await tgConfirm("Скрыть контент у всех и закрыть жалобу?"))) return;
    setBusy(true);
    try {
      await sendAdminReportAction(initData, id, action);
      setReports((prev) => prev.filter((r) => r.id !== id));
      void loadOverview();
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Не удалось обработать жалобу");
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
      showAlert(e instanceof Error ? e.message : "Не удалось загрузить скрытое");
    }
  };

  const restoreHidden = async (item: AdminHiddenItem) => {
    if (busy) return;
    if (!(await tgConfirm("Вернуть этот контент в ленту / чат?"))) return;
    setBusy(true);
    try {
      await restoreAdminHidden(initData, item.kind, item.id);
      setHidden((prev) => prev.filter((h) => !(h.kind === item.kind && h.id === item.id)));
      void loadOverview();
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Не удалось вернуть");
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
      showAlert(e instanceof Error ? e.message : "Не удалось загрузить пользователей");
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
      showAlert(e instanceof Error ? e.message : "Пользователь не найден");
      setPage("users");
    }
  };

  const changeStat = async (field: AdminStatField, mode: "set" | "add", sign = 1) => {
    if (!card || busy) return;
    const raw = Number(statValue[field]);
    if (!Number.isFinite(raw)) {
      showAlert("Впиши число.");
      return;
    }
    setBusy(true);
    try {
      const j = await setAdminUserStat(initData, card.user_id, field, mode, Math.round(raw) * sign);
      setCard(j.user);
      void loadOverview();
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Не удалось изменить");
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
      showAlert("Сохранил.");
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Не удалось сохранить");
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
      showAlert(e instanceof Error ? e.message : "Не удалось выполнить действие");
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
      showAlert(e instanceof Error ? e.message : "Не удалось выполнить действие");
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
      showAlert(e instanceof Error ? e.message : "Не удалось загрузить цену");
    }
  };

  const savePrice = async () => {
    if (busy) return;
    const n = Number.parseInt(priceInput.trim(), 10);
    if (!Number.isFinite(n) || n < 1 || n > 100000) {
      showAlert("Цена — целое число от 1 до 100000 ₽");
      return;
    }
    if (!(await tgConfirm(`Выставить цену доступа ${n} ₽? Новые оплаты пойдут по этой сумме.`))) return;
    setBusy(true);
    try {
      const j = await saveAdminPaywallPrice(initData, n);
      setPrice(j.price);
      setPriceInput(String(j.price.amount_rub));
      setOverview((prev) => ({ ...prev, access_price_rub: j.price.amount_rub }));
      showAlert(`Цена доступа: ${j.price.amount_rub} ₽`);
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Не удалось сохранить цену");
    } finally {
      setBusy(false);
    }
  };

  const resetPrice = async () => {
    if (busy || !price) return;
    if (!(await tgConfirm(`Вернуть цену к значению сервера (${price.default_amount_rub} ₽)?`))) return;
    setBusy(true);
    try {
      const j = await resetAdminPaywallPrice(initData);
      setPrice(j.price);
      setPriceInput(String(j.price.amount_rub));
      setOverview((prev) => ({ ...prev, access_price_rub: j.price.amount_rub }));
      showAlert(`Цена сброшена: ${j.price.amount_rub} ₽`);
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Не удалось сбросить цену");
    } finally {
      setBusy(false);
    }
  };

  const publish = async () => {
    const text = announce.trim();
    if (!text || busy) return;
    if (!(await tgConfirm("Опубликовать объявление в ленте стаи?"))) return;
    setBusy(true);
    try {
      await publishAdminPost(initData, text, announceAuthor);
      setAnnounce("");
      showAlert("Опубликовано в ленте");
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Не удалось опубликовать");
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
      ? (ADMIN_TABS.find((t) => t.key === tab)?.label ?? "Админка")
      : page === "support"
        ? "Поддержка"
        : page === "thread"
          ? userLabel(threadUser?.display_name, undefined, threadUser?.user_id)
          : page === "reports"
            ? "Жалобы"
            : page === "hidden"
              ? "Скрытое"
              : page === "users"
                ? "Участники"
                : page === "card"
                  ? userLabel(card?.display_name, card?.username, card?.user_id)
                  : page === "price"
                    ? "Цена доступа"
                    : page === "dashboards"
                      ? "Дашборды"
                      : page === "analytics"
                        ? "Аналитика"
                        : page === "visits"
                          ? "Посещения бота"
                          : page === "payments"
                            ? "Оплаты"
                            : page === "admins"
                              ? "Админы"
                              : page === "scheduled"
                                ? "Отложенные посты"
                                : page === "poll"
                                  ? "Опрос в ленту"
                                  : page === "leolab"
                                    ? "Тест Лео"
                                    : page === "prompts"
                                    ? "Промпты Леопарда"
                                    : page === "wipe"
                                    ? "Очистить ленту и чат"
                                    : "Объявление";

  return (
    <>
      <div className="admin-backdrop" aria-hidden="true" onClick={closeSheet} />
      <div ref={sheetRef} className="admin" role="dialog" aria-modal="true" aria-label="Админка">
      <div className="admin__grabber" aria-hidden="true" />
      <button type="button" className="admin__close" onClick={closeSheet} aria-label="Закрыть">
        ✕
      </button>
      <header className="admin__head">
        {page !== "home" ? (
          <button type="button" className="admin__back" onClick={back} aria-label="Назад">
            ←
          </button>
        ) : null}
        <h1 className="admin__title">{title}</h1>
      </header>

      {page === "home" && tab === "community" && (
        <div className="admin__body">
          {loading ? (
            <p className="admin__muted">Загрузка…</p>
          ) : (
            <>
              <div className="admin__stats">
                <div className="admin__stat">
                  <strong>{overview.users_active}</strong>
                  <span>активных</span>
                </div>
                <div className="admin__stat">
                  <strong>{overview.users_kicked}</strong>
                  <span>кикнутых</span>
                </div>
                <div className="admin__stat">
                  <strong>{overview.support_waiting}</strong>
                  <span>ждут ответа</span>
                </div>
                <div className="admin__stat">
                  <strong>{overview.reports_open}</strong>
                  <span>жалоб</span>
                </div>
                <div className="admin__stat">
                  <strong>{overview.hidden}</strong>
                  <span>скрыто</span>
                </div>
              </div>
              <div className="admin__tiles">
                <button type="button" className="admin__tile" onClick={() => void openSupport()}>
                  <span className="admin__tile-ico">💬</span>
                  <span className="admin__tile-text">
                    <b>Поддержка</b>
                    <small>{overview.support_waiting > 0 ? `${overview.support_waiting} без ответа` : "все отвечены"}</small>
                  </span>
                </button>
                <button type="button" className="admin__tile" onClick={() => void openReports()}>
                  <span className="admin__tile-ico">🚩</span>
                  <span className="admin__tile-text">
                    <b>Жалобы</b>
                    <small>{overview.reports_open > 0 ? `${overview.reports_open} открытых` : "пусто"}</small>
                  </span>
                </button>
                <button type="button" className="admin__tile" onClick={() => void openHidden()}>
                  <span className="admin__tile-ico">🙈</span>
                  <span className="admin__tile-text">
                    <b>Скрытое</b>
                    <small>вернуть посты и комментарии</small>
                  </span>
                </button>
                <button type="button" className="admin__tile" onClick={() => void openUsers()}>
                  <span className="admin__tile-ico">👥</span>
                  <span className="admin__tile-text">
                    <b>Участники</b>
                    <small>
                      {overview.users} за всю историю · {overview.users_active} активных · {overview.users_kicked}{" "}
                      кикнутых
                    </small>
                  </span>
                </button>
                <button type="button" className="admin__tile" onClick={() => setPage("announce")}>
                  <span className="admin__tile-ico">📣</span>
                  <span className="admin__tile-text">
                    <b>Объявление</b>
                    <small>пост в ленту от админа или Лео</small>
                  </span>
                </button>
                <button type="button" className="admin__tile" onClick={() => setPage("poll")}>
                  <span className="admin__tile-ico">🗳</span>
                  <span className="admin__tile-text">
                    <b>Опрос</b>
                    <small>вопрос и варианты в ленту</small>
                  </span>
                </button>
                <button type="button" className="admin__tile" onClick={() => setPage("scheduled")}>
                  <span className="admin__tile-ico">📅</span>
                  <span className="admin__tile-text">
                    <b>Отложенные</b>
                    <small>посты по расписанию</small>
                  </span>
                </button>
                <button type="button" className="admin__tile" onClick={() => setPage("payments")}>
                  <span className="admin__tile-ico">💳</span>
                  <span className="admin__tile-text">
                    <b>Оплаты</b>
                    <small>доступ и донаты · ⭐ и ₽</small>
                  </span>
                </button>
                <button type="button" className="admin__tile" onClick={() => void openPrice()}>
                  <span className="admin__tile-ico">💰</span>
                  <span className="admin__tile-text">
                    <b>Цена доступа</b>
                    <small>
                      {overview.access_price_rub > 0
                        ? `${overview.access_price_rub} ₽ · вход и возврат`
                        : "сколько платят за вход и возврат"}
                    </small>
                  </span>
                </button>
              </div>
              <p className="admin__muted admin__hint">
                Всё то же есть и в боте: /admin. Кубки и стрики конкретного человека — в карточке участника.
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
                <b>Промпты Леопарда</b>
                <small>характер и тексты: поле или файл .txt/.md</small>
              </span>
            </button>
            <button type="button" className="admin__tile" onClick={() => setPage("leolab")}>
              <span className="admin__tile-ico">🧪</span>
              <span className="admin__tile-text">
                <b>Тест Лео</b>
                <small>спросить, попробовать промпт, научить</small>
              </span>
            </button>
          </div>
          <p className="admin__muted admin__hint">
            Боевые промпты лежат в <code>ms_leo/internal/prompts/data/*.txt</code>. Замена в «Промптах»
            применяется сразу, без пересборки; сброс возвращает встроенный файл из репозитория.
          </p>
        </div>
      )}

      {page === "home" && tab === "system" && (
        <div className="admin__body">
          <div className="admin__tiles">
                <button type="button" className="admin__tile" onClick={() => setPage("dashboards")}>
                  <span className="admin__tile-ico">📊</span>
                  <span className="admin__tile-text">
                    <b>Дашборды</b>
                    <small>KPI, воронки и посещения</small>
                  </span>
                </button>
                <button type="button" className="admin__tile" onClick={() => setPage("analytics")}>
                  <span className="admin__tile-ico">📈</span>
                  <span className="admin__tile-text">
                    <b>Аналитика</b>
                    <small>таблицы: воронки, каналы, события</small>
                  </span>
                </button>
                <button type="button" className="admin__tile" onClick={() => setPage("visits")}>
                  <span className="admin__tile-ico">👣</span>
                  <span className="admin__tile-text">
                    <b>Посещения бота</b>
                    <small>кто и как часто заходит</small>
                  </span>
                </button>
                <button type="button" className="admin__tile" onClick={() => setPage("admins")}>
                  <span className="admin__tile-ico">🛡</span>
                  <span className="admin__tile-text">
                    <b>Админы</b>
                    <small>выдать и снять права</small>
                  </span>
                </button>
                <button type="button" className="admin__tile" onClick={() => setPage("wipe")}>
                  <span className="admin__tile-ico">🗑</span>
                  <span className="admin__tile-text">
                    <b>Очистить ленту и чат</b>
                    <small>удалить всё содержимое стаи</small>
                  </span>
                </button>
          </div>
          <p className="admin__muted admin__hint">
            Технические разделы: метрики продукта, доступы и разовые операции над содержимым стаи.
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
            <p className="admin__muted">Диалогов пока нет</p>
          ) : (
            <ul className="admin__list">
              {convs.map((c) => (
                <li key={c.user_id}>
                  <button type="button" className="admin__row" onClick={() => void openThread(c)}>
                    <div className="admin__row-top">
                      <b>{userLabel(c.display_name, undefined, c.user_id)}</b>
                      {c.needs_reply ? <span className="admin__badge">ждёт</span> : null}
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
                      aria-label="Открыть фото"
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
              <button type="button" className="admin__reply-photo-pending-remove" aria-label="Убрать фото" onClick={clearReplyPhoto}>
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
              aria-label="Прикрепить фото из галереи"
              disabled={busy}
              onClick={() => replyPhotoInputRef.current?.click()}
            >
              📎
            </button>
            <CameraButton className="admin__reply-attach" onChange={onPickReplyPhoto} disabled={busy} />
            <input
              value={reply}
              onChange={(e) => setReply(e.target.value)}
              placeholder={replyPhoto ? "Подпись к фото (необязательно)…" : "Ответ пользователю…"}
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
            <p className="admin__muted">Открытых жалоб нет</p>
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
                    на {userLabel(r.target_name, undefined, r.target_user_id)} · от{" "}
                    {userLabel(r.reporter_name, undefined, r.reporter_user_id)}
                  </p>
                  <div className="admin__actions">
                    <button type="button" className="admin__btn admin__btn--ghost" onClick={() => void actReport(r.id, "dismiss")} disabled={busy}>
                      Закрыть
                    </button>
                    <button type="button" className="admin__btn admin__btn--danger" onClick={() => void actReport(r.id, "hide")} disabled={busy}>
                      Скрыть
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
            <p className="admin__muted">Скрытого контента нет</p>
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
                      Вернуть
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
            За всю историю: <strong>{usersActive + usersKicked}</strong> · активных {usersActive} · кикнутых{" "}
            {usersKicked}
          </p>
          <div className="admin__filters" role="tablist" aria-label="Фильтр участников">
            <button
              type="button"
              role="tab"
              aria-selected={userFilter === "all"}
              className={userFilter === "all" ? "on" : ""}
              onClick={() => setUsersFilter("all")}
            >
              Все ({usersActive + usersKicked})
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={userFilter === "active"}
              className={userFilter === "active" ? "on" : ""}
              onClick={() => setUsersFilter("active")}
            >
              Активные ({usersActive})
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={userFilter === "kicked"}
              className={userFilter === "kicked" ? "on" : ""}
              onClick={() => setUsersFilter("kicked")}
            >
              Кикнутые ({usersKicked})
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
              placeholder="Имя, @ник или id"
            />
            <button type="submit">Найти</button>
          </form>
          {users.length === 0 ? (
            <p className="admin__muted">Никого не нашли</p>
          ) : (
            <>
              <ul className="admin__list">
                {users.map((u) => (
                  <li key={u.user_id}>
                    <button type="button" className="admin__row" onClick={() => void openCard(u.user_id)}>
                      <div className="admin__row-top">
                        <b>{userLabel(u.display_name, u.username, u.user_id)}</b>
                        {u.is_deleted ? <span className="admin__badge admin__badge--muted">кикнут</span> : null}
                      </div>
                      <p className="admin__row-meta">
                        {u.cups} кубков · стрик {u.streak_days}
                        {u.has_active_paywall ? " · доступ есть" : ""}
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
                    ◀ Назад
                  </button>
                  <span className="admin__muted">
                    {userOffset + 1}–{userOffset + users.length} из {usersTotal}
                  </span>
                  <button
                    type="button"
                    disabled={userOffset + users.length >= usersTotal}
                    onClick={() => void loadUsers(userQuery, userOffset + USERS_PAGE_SIZE, userFilter)}
                  >
                    Вперёд ▶
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
              {card.username ? ` · @${card.username.replace(/^@/, "")}` : " · ника нет"}
            </p>
            <dl className="admin__dl">
              <div>
                <dt>Кубки</dt>
                <dd>{card.cups}</dd>
              </div>
              <div>
                <dt>Тренировок</dt>
                <dd>{card.workouts_total}</dd>
              </div>
              <div>
                <dt>Уровень</dt>
                <dd>
                  {card.level} · {card.level_name}
                </dd>
              </div>
              <div>
                <dt>Стрик</dt>
                <dd>
                  {card.streak_days} (рекорд {card.max_streak_days})
                </dd>
              </div>
              <div>
                <dt>Без тренировки</dt>
                <dd>{card.days_since_last_training < 0 ? "—" : `${card.days_since_last_training} дн.`}</dd>
              </div>
              <div>
                <dt>Больничный</dt>
                <dd>{card.sick_leave}</dd>
              </div>
              <div>
                <dt>Спасти стрик</dt>
                <dd>
                  {card.streak_save_attempts_avail}/{card.streak_save_attempts_max}
                </dd>
              </div>
              <div>
                <dt>Доступ</dt>
                <dd>{card.has_active_paywall ? "оплачен" : "нет"}</dd>
              </div>
              <div>
                <dt>В стае</dt>
                <dd>{card.is_deleted ? "кикнут" : "да"}</dd>
              </div>
            </dl>
            {card.ugc_muted_until ? <p className="admin__warn">UGC-мьют до {formatChatTime(card.ugc_muted_until)}</p> : null}
            <p className="admin__row-meta">Нарушений UGC: {card.ugc_violations}</p>
          </div>
          <section className="admin__stats-edit">
            <h3 className="admin__stats-edit-title">Показатели</h3>
            <p className="admin__muted admin__hint">
              Уровень и попытки спасти стрик считаются от кубков — отдельно их задавать не нужно.
            </p>

            <div className="admin__stat-row">
              <span>Кубки</span>
              <input
                inputMode="numeric"
                value={statValue.cups}
                onChange={(e) => setStatValue((p) => ({ ...p, cups: e.target.value }))}
              />
            </div>

            <div className="admin__stat-row">
              <span>Стрик</span>
              <input
                inputMode="numeric"
                value={statValue.streak}
                onChange={(e) => setStatValue((p) => ({ ...p, streak: e.target.value }))}
              />
            </div>

            <div className="admin__stat-row">
              <span>Рекорд</span>
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
              {statsDirty ? "Сохранить показатели" : "Изменений нет"}
            </button>

            <div className="admin__stat-row">
              <span>Тренировки</span>
              <input
                inputMode="numeric"
                value={statValue.workouts}
                onChange={(e) => setStatValue((p) => ({ ...p, workouts: e.target.value }))}
              />
              <button type="button" disabled={busy} onClick={() => void changeStat("workouts", "add")}>
                Зачесть
              </button>
              <button type="button" disabled={busy} onClick={() => void changeStat("workouts", "add", -1)}>
                Снять
              </button>
            </div>
            <p className="admin__muted admin__hint">
              Тренировки — это записи о сессиях, поэтому «зачесть» добавляет их сегодняшним днём, а «снять» убирает
              последние.
            </p>
          </section>

          <div className="admin__actions admin__actions--stack">
            {card.sick_leave === "активен" || card.sick_leave === "ожидает" ? (
              <button type="button" className="admin__btn" disabled={busy} onClick={() => void actUser("sick_cancel", "Снять больничный?")}>
                Снять больничный
              </button>
            ) : (
              <button type="button" className="admin__btn" disabled={busy || card.is_deleted} onClick={() => void actUser("sick_set", "Оформить больничный этому участнику?")}>
                Выставить больничный
              </button>
            )}
            <button type="button" className="admin__btn" disabled={busy} onClick={() => void actUser("grant_save", "Выдать +1 попытку спасти стрик?")}>
              +1 спасти стрик
            </button>
            {card.ugc_muted_until ? (
              <button type="button" className="admin__btn" disabled={busy} onClick={() => void actUser("unmute", "Снять UGC-мьют?")}>
                Снять мьют
              </button>
            ) : (
              <button type="button" className="admin__btn" disabled={busy} onClick={() => void actUser("mute", "Замьютить публикации на 24 часа?")}>
                Мьют 24ч
              </button>
            )}
            {card.is_deleted ? (
              <>
                <button type="button" className="admin__btn" disabled={busy} onClick={() => void actUser("restore_full", "Вернуть в стаю с достижениями?")}>
                  Вернуть с прогрессом
                </button>
                <button type="button" className="admin__btn admin__btn--ghost" disabled={busy} onClick={() => void actUser("restore_scratch", "Вернуть в стаю с нуля?")}>
                  Вернуть с нуля
                </button>
              </>
            ) : (
              <button type="button" className="admin__btn admin__btn--danger" disabled={busy} onClick={() => setKickAsk(true)}>
                Удалить из стаи
              </button>
            )}
          </div>

          {kickAsk ? (
            <div className="admin__modal" role="dialog" aria-modal="true">
              <div className="admin__modal-box">
                <h3>Удалить из стаи?</h3>
                <p>
                  {userLabel(card.display_name, card.username, card.user_id)} потеряет доступ к ленте и чату. Кубки и
                  стрик сохранятся — вернуть можно кнопкой «Вернуть с прогрессом».
                </p>
                <div className="admin__modal-actions">
                  <button type="button" onClick={() => setKickAsk(false)}>
                    Отмена
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
                    Удалить
                  </button>
                </div>
              </div>
            </div>
          ) : null}
        </div>
      )}

      {page === "announce" && (
        <div className="admin__body">
          <div className="admin__voice" role="group" aria-label="От чьего имени">
            <button type="button" className={announceAuthor === "admin" ? "is-on" : ""} onClick={() => setAnnounceAuthor("admin")}>
              Админ
            </button>
            <button type="button" className={announceAuthor === "leo" ? "is-on" : ""} onClick={() => setAnnounceAuthor("leo")}>
              Лео
            </button>
          </div>
          <textarea
            className="admin__textarea"
            value={announce}
            onChange={(e) => setAnnounce(e.target.value)}
            placeholder="Текст объявления в ленту…"
            maxLength={4000}
            rows={7}
          />
          <button type="button" className="admin__btn admin__btn--wide" disabled={busy || !announce.trim()} onClick={() => void publish()}>
            Опубликовать
          </button>
        </div>
      )}

      {page === "price" && (
        <div className="admin__body">
          <p className="admin__muted admin__hint">
            Один платёж за вход в стаю и за возврат после удаления. Уже оплатившие не платят снова. Звёзды Telegram
            задаются отдельно на сервере.
          </p>
          <label className="admin__price">
            <span>Цена, ₽</span>
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
              Сейчас {price.amount_rub} ₽
              {price.is_custom ? " · задана админом" : " · значение сервера"}
              {price.is_custom ? ` · сброс вернёт ${price.default_amount_rub} ₽` : ""}
            </p>
          ) : (
            <p className="admin__muted">Загрузка…</p>
          )}
          <button type="button" className="admin__btn admin__btn--wide" disabled={busy || !priceInput.trim()} onClick={() => void savePrice()}>
            Сохранить
          </button>
          {price?.is_custom ? (
            <button type="button" className="admin__btn admin__btn--ghost admin__btn--wide" disabled={busy} onClick={() => void resetPrice()}>
              Сбросить к {price.default_amount_rub} ₽
            </button>
          ) : null}
        </div>
      )}
      <nav
        className="admin__tabs"
        aria-label="Разделы админки"
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

