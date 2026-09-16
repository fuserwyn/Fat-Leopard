import { LEO_AVATAR_URL } from "./leoAvatar";
import { formatLocalDateTime } from "./timeAgo";
import {
  parseTrainingDoneCategories,
  stripLeadingCategoryFromTrainingReport,
  trainingDoneCategoryDisplayLabel,
  trainingDoneCategoryEmoji,
  WORKOUT_CATEGORY_OPTIONS,
  type WorkoutCategoryId,
} from "./workoutCategories";
import type { ActivityCardProps } from "../components/ActivityCard";

const viteMiniappApi = (import.meta.env.VITE_MINIAPP_API_URL as string | undefined)?.replace(/\/$/, "").trim() ?? "";
const browserOrigin = typeof window !== "undefined" ? window.location.origin.replace(/\/$/, "") : "";

/** Лента: в БД мог остаться старый origin; API additionally canonicalizes via MINIAPP_PUBLIC_BASE_URL. */
export function resolveTrainingPhotoUrl(stored: string | undefined): string | undefined {
  const raw = (stored ?? "").trim();
  if (!raw) return undefined;
  const marker = "/api/miniapp/media/";
  const idx = raw.indexOf(marker);
  let path = "";
  if (idx >= 0) {
    path = raw.slice(idx);
  } else if (raw.startsWith(marker.slice(1))) {
    path = "/" + raw;
  } else if (raw.startsWith(marker)) {
    path = raw;
  } else {
    return raw;
  }
  // Уже канонический https с API — не перетираем ошибочным VITE_MINIAPP_API_URL (частая причина битых картинок).
  if (raw.startsWith("https://") && !raw.includes("127.0.0.1") && !raw.includes("localhost")) {
    return raw;
  }
  if (viteMiniappApi) return viteMiniappApi + path;
  if (browserOrigin && browserOrigin.startsWith("http")) return browserOrigin + path;
  return raw;
}

/** Аватары в ленте: абсолютные URL как есть; относительные `/api/miniapp/...` — к базе API (прокси user-avatar). */
export function resolveFeedAvatarUrl(stored: string | undefined): string {
  const raw = (stored ?? "").trim();
  if (!raw) return "";
  if (raw.startsWith("http://") || raw.startsWith("https://")) return raw;
  if (raw.startsWith("/") && viteMiniappApi) return viteMiniappApi + raw;
  return raw;
}

/** Совпадает с ms_leo TrainingFeedApprovingEmojis. */
export const TRAINING_FEED_APPROVING_EMOJIS = [
  "🔥",
  "💪",
  "👏",
  "❤️",
  "🎉",
  "🙌",
  "✨",
  "🤝",
  "🏆",
  "🥳",
  "🤩",
  "🤗",
  "👌",
  "🫶",
  "🧡",
  "💜",
  "🤘",
] as const;

/** Эмодзи вида спорта для реакций (совпадает с ms_leo TrainingCategoryReactionEmoji). */
export const TRAINING_CATEGORY_REACTION_EMOJI: Record<WorkoutCategoryId, string> = {
  run: "🏃",
  walk: "🚶",
  bike: "🚴",
  swim: "🏊",
  yoga: "🧘",
  rowing: "🚣",
  workout: "🔥",
  crossfit: "🎯",
  stretch: "🧎",
  dance: "💃",
  hiit: "⚡",
  cardio: "💓",
  kettlebell: "🏋️",
  strength: "🏋️",
  jump_rope: "🪢",
  pole: "🤸",
  rollerblade: "🛼",
  basketball: "🏀",
  football: "⚽",
  volleyball: "🏐",
  tennis: "🎾",
  padel: "🏏",
  gymnastics: "🙆",
  other: "✨",
};

/** Совпадает с ms_leo trainingFeedAllowedEmojis (порядок отображения). */
export const TRAINING_FEED_EMOJIS = [
  ...TRAINING_FEED_APPROVING_EMOJIS,
  ...WORKOUT_CATEGORY_OPTIONS.map((o) => TRAINING_CATEGORY_REACTION_EMOJI[o.id]).filter(
    (e, i, arr) => arr.indexOf(e) === i && !TRAINING_FEED_APPROVING_EMOJIS.includes(e as (typeof TRAINING_FEED_APPROVING_EMOJIS)[number]),
  ),
] as const;

/** Совпадает с ms_leo sickLeaveAllowedEmojis. */
export const SICK_LEAVE_FEED_EMOJIS = ["😢", "😔", "🥺", "🤒", "🫂", "🙏", "❤️", "💙", "🌧️", "💤"] as const;

/** Совпадает с ms_leo healthyAllowedEmojis. */
export const HEALTHY_FEED_EMOJIS = ["🎉", "🥳", "😄", "💚", "❤️", "👏", "🙌", "✨", "🌟", "💪"] as const;

/** Совпадает с ms_leo packJoinAllowedEmojis — только посты вступления/возвращения. */
export const PACK_JOIN_FEED_EMOJIS = ["👋", "🎉", "❤️", "👏", "🙌"] as const;

/** Кто отреагировал/лайкнул: имя + аватар (для поповера «кто лайкнул»). */
export type VoterDTO = { name: string; photo_url?: string };

/** Элемент списка «кто отреагировал» с отрезолвленным URL аватара. */
export type VoterLiker = { name: string; photoUrl?: string };

/** URL аватара голосующего: Лео всегда leo-avatar.png (статик мини-аппа, не API-прокси). */
export function resolveVoterPhotoUrl(v: Pick<VoterDTO, "name" | "photo_url">): string | undefined {
  const name = (v.name || "").trim();
  const raw = (v.photo_url ?? "").trim();
  if (name === "Лео" || raw.includes("leo-avatar")) {
    return LEO_AVATAR_URL;
  }
  if (!raw) return undefined;
  return resolveFeedAvatarUrl(raw);
}

/** Голоса с бэкенда → список для LikersPopover (имя + photoUrl). */
export function votersToLikers(voters?: VoterDTO[] | string[]): VoterLiker[] {
  if (!voters) return [];
  return voters.map((v) => {
    if (typeof v === "string") {
      const name = v.trim();
      return { name: v, photoUrl: name === "Лео" ? LEO_AVATAR_URL : undefined };
    }
    return {
      name: v.name,
      photoUrl: resolveVoterPhotoUrl(v),
    };
  });
}

export type PackFeedReactionDTO = { emoji: string; count: number; me: boolean; voters?: VoterDTO[] | string[] };

export type PackFeedPollOptionDTO = {
  label: string;
  votes: number;
};

export type PackFeedPollDTO = {
  question: string;
  options: PackFeedPollOptionDTO[];
  total_votes: number;
  my_vote_index?: number;
};

export type PackFeedThreadReplyDTO = {
  id: number;
  user_id: number;
  username: string;
  text: string;
  created_at: string;
  is_you: boolean;
  is_leo?: boolean;
  /** Комментарий опубликован «от имени админов». */
  is_admin?: boolean;
  /** Реальное имя автора-админа за голосом Лео/Админ — приходит ТОЛЬКО админам. */
  admin_name?: string;
  /** URL из miniapp_user_profile (Telegram WebApp user.photo_url при онбординге). */
  author_photo_url?: string;
  /** Фото, приложенное к комментарию (опционально). */
  photo_url?: string;
  /** Ответ на сообщение внутри треда (как Reply в Telegram). */
  reply_to_id?: number;
  reply_to_username?: string;
  reply_to_text?: string;
  reply_to_is_leo?: boolean;
  reply_to_is_admin?: boolean;
  like_count?: number;
  like_me?: boolean;
  /** Имена лайкнувших комментарий (для поповера «кто лайкнул»). */
  like_voters?: VoterDTO[] | string[];
  edited_at?: string;
};

export type PackFeedItemDTO = {
  id: number;
  user_id: number;
  username: string;
  /** Настоящий TG-ник автора без «@» (username может быть display_name из анкеты). */
  author_tg_username?: string;
  type: string;
  /** Источник записи: "feed" (тренировки/системные, user_messages) или
   *  "message" (сообщение общего чата, miniapp_pack_group_chat). id уникален только
   *  внутри источника — ключевать/дедупить записи нужно по `feedItemKey`. */
  source?: "feed" | "message";
  text: string;
  created_at: string;
  streak_days: number;
  is_you: boolean;
  /** Viewer подписан на автора этой карточки (для фильтра «Друзья»). */
  is_friend?: boolean;
  pack_chat_id?: number;
  pack_title?: string;
  author_photo_url?: string;
  reactions?: PackFeedReactionDTO[];
  thread?: PackFeedThreadReplyDTO[];
  poll?: PackFeedPollDTO;
  /** Публичный URL фото из мини‑аппа после POST /api/miniapp/workout */
  training_photo_url?: string;
  /** Объявление закреплено админом (висит сверху ленты). */
  is_pinned?: boolean;
  pinned_at?: string;
  edited_at?: string;
};

/** Текст поста для поля редактирования (без служебных префиксов ленты). */
export function extractEditableFeedPostText(raw: string, type: string): string {
  const t = raw.trim();
  if (type === "healthy") {
    return stripLeadingFeedHashtag(t, "#healthy");
  }
  if (type === "training_done") {
    return stripLeadingCategoryFromTrainingReport(stripFirstLineTrainingDoneTag(t));
  }
  return t;
}

/** Собирает message_text для API из отредактированного текста пользователя. */
export function buildFeedPostTextForSave(originalRaw: string, type: string, edited: string): string {
  const body = edited.trim();
  if (!body) return body;
  if (type === "healthy") {
    const head = originalRaw.trim().match(/^(#healthy\b[^\n]*)/i)?.[1];
    if (head) return `${head}${body ? `\n${body}` : ""}`;
    return `#healthy\n${body}`;
  }
  if (type === "training_done") {
    const trimmed = originalRaw.trim();
    const prefix = trimmed.match(/^(#training_done\s*[—–-]\s*)/i)?.[1] ?? "#training_done — ";
    const withoutTag = trimmed.replace(/^#training_done\s*[—–-]\s*/i, "").trim();
    const nl = withoutTag.indexOf("\n");
    const firstLine = nl >= 0 ? withoutTag.slice(0, nl) : withoutTag;
    const kindMatch = firstLine.match(/^([^,]+),\s*(.+)$/);
    if (!kindMatch || !/\d+\s*мин/i.test(kindMatch[2])) {
      return `${prefix}${body}`;
    }
    const kind = kindMatch[1];
    const editedNl = body.indexOf("\n");
    const editedFirst = editedNl >= 0 ? body.slice(0, editedNl).trim() : body.trim();
    const editedRest = editedNl >= 0 ? body.slice(editedNl + 1).trim() : "";
    const newFirstLine = /^[^,]+,\s*.+\d+\s*мин/i.test(editedFirst) ? editedFirst : `${kind}, ${editedFirst}`;
    if (editedRest) return `${prefix}${newFirstLine}\n${editedRest}`;
    return `${prefix}${newFirstLine}`;
  }
  return body;
}

export function feedPostEditable(type: string, isYou: boolean): boolean {
  return isYou && (type === "training_done" || type === "healthy" || type === "pack_message");
}

/** Типы карточек ленты с тредом комментариев (совпадает с packFeedSupportsThread на бэкенде). */
export function feedSupportsThread(type: string): boolean {
  return (
    type === "training_done" ||
    type === "sick_leave" ||
    type === "healthy" ||
    type === "admin_post" ||
    type === "admin_poll" ||
    type === "daily_wisdom"
  );
}

/** Стабильный ключ записи единой ленты: id уникален только внутри источника,
 *  поэтому ключуем/дедупим как `${source}:${id}` ("feed" по умолчанию). */
export function feedItemKey(d: Pick<PackFeedItemDTO, "id" | "source">): string {
  return `${d.source ?? "feed"}:${d.id}`;
}

/** true — запись является сообщением общего чата (карточка pack_message). */
export function isFeedMessage(d: Pick<PackFeedItemDTO, "type" | "source">): boolean {
  return d.type === "pack_message" || d.source === "message";
}

/** reply_to_id комментария в треде сообщения: ответ участнику/Лео или сама карточка. */
export function packMessageCommentReplyToId(cardId: number, replyToCommentId?: number): number {
  if (typeof replyToCommentId === "number" && replyToCommentId > 0) {
    return replyToCommentId;
  }
  return cardId;
}

export type FeedThreadQuote = {
  author: string;
  text: string;
  isLeo?: boolean;
  isAdmin?: boolean;
};

/** Цитата «ответ на…» в треде: Лео/Админ показываем даже без текста (фото-only). */
export function mapFeedThreadQuote(
  tr: Pick<
    PackFeedThreadReplyDTO,
    | "user_id"
    | "reply_to_id"
    | "reply_to_username"
    | "reply_to_text"
    | "reply_to_is_leo"
    | "reply_to_is_admin"
  >,
): FeedThreadQuote | undefined {
  const replyToId = typeof tr.reply_to_id === "number" ? tr.reply_to_id : 0;
  if (replyToId <= 0) return undefined;
  const isLeo = Boolean(tr.reply_to_is_leo);
  const isAdmin = Boolean(tr.reply_to_is_admin);
  const username = (tr.reply_to_username || "").trim();
  const text = (tr.reply_to_text || "").trim();
  if (!isLeo && !isAdmin && text === "" && username === "") return undefined;
  const author = isLeo ? "Лео" : isAdmin ? "Админ" : username || `Участник ${tr.user_id}`;
  return { author, text, isLeo, isAdmin };
}

/** Сводит авторитетный набор закреплённых (с бэка) в текущую ленту:
 *  обновляет/добавляет закреплённые, снимает флаг с откреплённых. */
export function reconcilePinnedFeed(
  prev: PackFeedItemDTO[],
  pinned: PackFeedItemDTO[],
): PackFeedItemDTO[] {
  const pinnedByKey = new Map(pinned.map((p) => [feedItemKey(p), p]));
  let changed = false;
  const next = prev.map((it) => {
    const p = pinnedByKey.get(feedItemKey(it));
    if (p) {
      changed = true;
      return { ...it, ...p, is_pinned: true };
    }
    if (it.is_pinned) {
      changed = true;
      return { ...it, is_pinned: false, pinned_at: undefined };
    }
    return it;
  });
  for (const p of pinned) {
    if (!next.some((it) => feedItemKey(it) === feedItemKey(p))) {
      changed = true;
      next.push({ ...p, is_pinned: true });
    }
  }
  if (!changed) return prev;
  return sortPackFeedItemsDesc(next);
}

/** Допустимые реакции на отчёт: одобряющие + виды из текста тренировки. */
export function trainingFeedAllowedEmojisForReport(reportText?: string): readonly string[] {
  const cats = reportText?.trim() ? parseTrainingDoneCategories(reportText) : (["other"] as WorkoutCategoryId[]);
  const seen = new Set<string>();
  const out: string[] = [];
  const add = (e: string) => {
    if (!e || seen.has(e)) return;
    seen.add(e);
    out.push(e);
  };
  for (const e of TRAINING_FEED_APPROVING_EMOJIS) add(e);
  for (const id of cats.length > 0 ? cats : (["other"] as WorkoutCategoryId[])) {
    add(TRAINING_CATEGORY_REACTION_EMOJI[id] ?? TRAINING_CATEGORY_REACTION_EMOJI.other);
  }
  return out;
}

/** Полная строка эмодзи для кнопок реакций (с нулевыми счётчиками). Собственная реакция (`me`) показывается первой — в т.ч. если её выбрали в меню «⋯». */
export function mergeTrainingFeedReactions(
  fromServer?: PackFeedReactionDTO[],
  reportText?: string,
): { emoji: string; count: number; me: boolean; voters?: VoterDTO[] | string[] }[] {
  const allowed = reportText != null ? trainingFeedAllowedEmojisForReport(reportText) : TRAINING_FEED_EMOJIS;
  return mergePackFeedReactions(allowed, fromServer);
}

/** Реакции по типу карточки ленты (совпадает с allowedEmojiForType на бэкенде). */
export function mergeFeedReactionsForType(
  type: string,
  fromServer?: PackFeedReactionDTO[],
  reportText?: string,
): { emoji: string; count: number; me: boolean; voters?: VoterDTO[] | string[] }[] {
  if (type === "training_done") {
    return mergeTrainingFeedReactions(fromServer, reportText);
  }
  if (type === "daily_wisdom" || type === "pack_message") {
    return mergePackFeedReactions(TRAINING_FEED_APPROVING_EMOJIS, fromServer);
  }
  if (type === "pack_join" || type === "pack_rejoin") {
    return mergePackFeedReactions(PACK_JOIN_FEED_EMOJIS, fromServer);
  }
  if (type === "sick_leave") return mergePackFeedReactions(SICK_LEAVE_FEED_EMOJIS, fromServer);
  if (type === "healthy" || type === "admin_post" || type === "admin_poll") {
    return mergePackFeedReactions(HEALTHY_FEED_EMOJIS, fromServer);
  }
  return [];
}

/** Локальный toggle реакции (одна на пользователя; повтор той же эмодзи снимает). */
export function optimisticTogglePackFeedReaction(
  fromServer: PackFeedReactionDTO[] | undefined,
  emoji: string,
): PackFeedReactionDTO[] {
  const list = [...(fromServer ?? [])];
  const mineIdx = list.findIndex((r) => r.me);

  if (mineIdx >= 0 && list[mineIdx]!.emoji === emoji) {
    const r = list[mineIdx]!;
    if (r.count <= 1) list.splice(mineIdx, 1);
    else list[mineIdx] = { ...r, count: r.count - 1, me: false, voters: undefined };
    return list;
  }

  if (mineIdx >= 0) {
    const old = list[mineIdx]!;
    if (old.count <= 1) list.splice(mineIdx, 1);
    else list[mineIdx] = { ...old, count: old.count - 1, me: false, voters: undefined };
  }

  const idx = list.findIndex((r) => r.emoji === emoji);
  if (idx >= 0) {
    const r = list[idx]!;
    list[idx] = { ...r, count: r.count + 1, me: true, voters: undefined };
  } else {
    list.push({ emoji, count: 1, me: true });
  }
  return list;
}

/** Локальный toggle ❤️ в треде под отчётом. */
export function optimisticToggleThreadReplyLike(
  thread: PackFeedThreadReplyDTO[],
  threadReplyId: number,
): PackFeedThreadReplyDTO[] {
  return thread.map((tr) => {
    if (tr.id !== threadReplyId) return tr;
    const was = Boolean(tr.like_me);
    return {
      ...tr,
      like_me: !was,
      like_count: Math.max(0, (tr.like_count ?? 0) + (was ? -1 : 1)),
    };
  });
}

export function mergePackFeedReactions(
  allowedEmojis: readonly string[],
  fromServer?: PackFeedReactionDTO[],
): { emoji: string; count: number; me: boolean; voters?: VoterDTO[] | string[] }[] {
  const byEmoji = new Map<string, PackFeedReactionDTO>();
  for (const r of fromServer ?? []) {
    byEmoji.set(r.emoji, r);
  }
  const ordered = allowedEmojis.map((emoji) => {
    const r = byEmoji.get(emoji);
    return { emoji, count: r?.count ?? 0, me: r?.me ?? false, voters: r?.voters };
  });
  const myIdx = ordered.findIndex((x) => x.me);
  if (myIdx <= 0) return ordered;
  const mine = ordered[myIdx]!;
  const rest = [...ordered.slice(0, myIdx), ...ordered.slice(myIdx + 1)];
  return [mine, ...rest];
}

function typeMeta(t: string): { emoji: string; activity: string; details: string } {
  switch (t) {
    case "training_done":
      return { emoji: "💪", activity: "", details: "" };
    case "sick_leave":
      return { emoji: "🏥", activity: "Больничный", details: "" };
    case "healthy":
      return { emoji: "💚", activity: "Выздоровление", details: "" };
    case "pack_join":
      return { emoji: "🐆", activity: "Лео · приветствие", details: "" };
    case "pack_rejoin":
      return { emoji: "🐆", activity: "Лео · приветствие", details: "" };
    case "daily_wisdom":
      return { emoji: "🌅", activity: "Мудрость дня", details: "" };
    case "pack_removed":
      return { emoji: "🐆", activity: "Лео · стая", details: "Выбыл за неактивность" };
    case "admin_post":
      return { emoji: "📢", activity: "Админ · объявление", details: "Кастомный пост" };
    case "admin_poll":
      return { emoji: "🗳️", activity: "Админ · опрос", details: "Голосование в miniapp" };
    case "inactive_notice":
      return { emoji: "⏳", activity: "Лео · напоминание стае", details: "Таймер неактивности (дубль контекста)" };
    case "pack_message":
      return { emoji: "💬", activity: "", details: "" };
    default:
      return { emoji: "📝", activity: t, details: "Сообщение" };
  }
}

/** С первой строки убираем устаревший префикс #training_done, если есть. */
function stripFirstLineTrainingDoneTag(text: string): string {
  const t = text.trim();
  if (t === "") return t;
  const nl = t.indexOf("\n");
  const first = nl >= 0 ? t.slice(0, nl) : t;
  const rest = nl >= 0 ? t.slice(nl + 1) : "";
  const stripped = first.replace(/^#training_done\s*[—–-]\s*/i, "").trim();
  const combined = rest ? `${stripped}\n${rest}` : stripped;
  return combined.trim();
}

/** Убирает ведущие строки с тегом — заголовок карточки уже передаёт смысл (#healthy / #sick_leave не дублируем мелким текстом). */
function stripLeadingFeedHashtag(text: string, tag: "#healthy" | "#sick_leave"): string {
  const esc = tag.replace("#", "\\#");
  return text.replace(new RegExp(`^(?:\\s*${esc}\\b[^\\n]*(?:\\n|$))+`, "gi"), "").trim();
}

function avatarFor(name: string) {
  const t = (name || "?").trim();
  const first = t[0] ?? "🐆";
  if (first && !/[a-zA-Zа-яА-Я0-9@#]/.test(first)) {
    return first; // already emoji
  }
  return "🐾";
}

export function dtoToCard(d: PackFeedItemDTO): ActivityCardProps {
  const m = typeMeta(d.type);
  const trainingEmoji = d.type === "training_done" ? trainingDoneCategoryEmoji(d.text) : m.emoji;
  const isLeoNotice =
    d.type === "pack_join" ||
    d.type === "pack_rejoin" ||
    d.type === "daily_wisdom" ||
    d.type === "pack_removed";
  const isAdminPost = d.type === "admin_post";
  const isAdminPoll = d.type === "admin_poll";
  const newcomer = (d.username || "").trim() || `Участник ${d.user_id}`;
  const pic = resolveFeedAvatarUrl(d.author_photo_url);
  let commentRaw = d.text.trim();
  if (d.type === "healthy") {
    commentRaw = stripLeadingFeedHashtag(commentRaw, "#healthy");
  } else if (d.type === "sick_leave") {
    commentRaw = stripLeadingFeedHashtag(commentRaw, "#sick_leave");
  } else if (d.type === "training_done") {
    commentRaw = stripLeadingCategoryFromTrainingReport(stripFirstLineTrainingDoneTag(commentRaw));
  }
  const maxComment =
    d.type === "training_done" ||
    d.type === "daily_wisdom" ||
    d.type === "pack_join" ||
    d.type === "pack_rejoin"
      ? 2000
      : 280;
  const comment =
    commentRaw.length > maxComment ? commentRaw.slice(0, maxComment - 1) + "…" : commentRaw;
  if (isLeoNotice) {
    // Подзаголовок только если не дублирует строку типа (🌅 Мудрость дня / 🐆 приветствие) и текст карточки.
    let leoDetails = "";
    if (d.type === "pack_removed") leoDetails = newcomer;
    return {
      avatar: LEO_AVATAR_URL,
      name: "Лео",
      streak: 0,
      hideStreak: true,
      timeAgo: formatLocalDateTime(d.created_at),
      emoji: m.emoji,
      activity: m.activity,
      details: leoDetails,
      comment,
    };
  }
  if (isAdminPost) {
    // Пост может публиковаться от имени Лео (username "Лео") или от имени Админа.
    const fromLeo = (d.username || "").trim() === "Лео";
    return {
      avatar: fromLeo ? LEO_AVATAR_URL : "📢",
      name: fromLeo ? "Лео" : "Админ",
      streak: 0,
      hideStreak: true,
      lightTone: true,
      timeAgo: formatLocalDateTime(d.created_at),
      emoji: fromLeo ? "🐆" : m.emoji,
      activity: fromLeo ? "Объявление" : m.activity,
      details: "",
      comment,
    };
  }
  if (isAdminPoll) {
    return {
      avatar: "🗳️",
      name: "Админ",
      streak: 0,
      hideStreak: true,
      lightTone: true,
      timeAgo: formatLocalDateTime(d.created_at),
      emoji: m.emoji,
      activity: m.activity,
      details: d.poll?.total_votes ? `${d.poll.total_votes} голосов` : "",
      comment: d.poll?.question?.trim() || comment,
    };
  }
  if (d.type === "pack_message") {
    // Сообщение общего чата как карточка ленты: автор + текст (+ фото), без стрика/активности.
    return {
      avatar: (pic || "").trim() || avatarFor(d.username),
      name: d.is_you ? "Ты" : d.username || `Участник ${d.user_id}`,
      streak: 0,
      hideStreak: true,
      timeAgo: formatLocalDateTime(d.created_at),
      emoji: "",
      activity: "",
      details: "",
      comment,
      trainingPhotoUrl: resolveTrainingPhotoUrl(d.training_photo_url),
    };
  }
  return {
    avatar: (pic || "").trim() || avatarFor(d.username),
    name: d.is_you ? "Ты" : d.username || `Участник ${d.user_id}`,
    streak: d.streak_days,
    timeAgo: formatLocalDateTime(d.created_at),
    emoji: trainingEmoji,
    activity: d.type === "training_done" ? trainingDoneCategoryDisplayLabel(d.text) : m.activity,
    details: d.type === "training_done" ? "" : m.details,
    comment,
    trainingPhotoUrl: resolveTrainingPhotoUrl(d.training_photo_url),
  };
}

/** Сортировка ленты: новые сверху. */
export function sortPackFeedItemsDesc(items: PackFeedItemDTO[]): PackFeedItemDTO[] {
  return [...items].sort((a, b) => {
    const tb = Date.parse(b.created_at) || 0;
    const ta = Date.parse(a.created_at) || 0;
    if (tb !== ta) return tb - ta;
    return b.id - a.id;
  });
}

function trainingReportFirstLineKey(text: string): string {
  const line = text.trim().replace(/^#training_done\s*[—–-]\s*/i, "").split("\n")[0] ?? "";
  const m = line.match(/^([^,]+),\s*\d+\s*мин/i);
  return (m?.[1] ?? line).trim().toLowerCase();
}

/** Добавляет только новые id; снимает временную optimistic-карточку при совпадении отчёта. */
export function mergePackFeedIncremental(prev: PackFeedItemDTO[], incoming: PackFeedItemDTO[]): PackFeedItemDTO[] {
  if (incoming.length === 0) return prev;
  const known = new Set(prev.map((i) => feedItemKey(i)));
  const fresh = incoming.filter((i) => !known.has(feedItemKey(i)));
  if (fresh.length === 0) return prev;
  const withoutOptimistic = prev.filter((p) => {
    if (p.id >= 0) return true;
    return !fresh.some(
      (f) =>
        f.type === "training_done" &&
        f.is_you &&
        p.is_you &&
        f.user_id === p.user_id &&
        trainingReportFirstLineKey(f.text) === trainingReportFirstLineKey(p.text),
    );
  });
  return sortPackFeedItemsDesc([...fresh, ...withoutOptimistic]);
}

/** Карточка сразу после отчёта, пока polling не подтянул строку из БД. */
export function buildOptimisticTrainingFeedItem(params: {
  userId: number;
  username: string;
  text: string;
  streakDays: number;
  trainingPhotoUrl?: string;
  authorPhotoUrl?: string;
}): PackFeedItemDTO {
  const un = params.username.trim();
  return {
    id: -Date.now(),
    user_id: params.userId,
    username: un.startsWith("@") ? un : `@${un}`,
    type: "training_done",
    text: params.text.trim(),
    created_at: new Date().toISOString(),
    streak_days: Math.max(0, params.streakDays),
    is_you: true,
    training_photo_url: params.trainingPhotoUrl,
    author_photo_url: params.authorPhotoUrl,
  };
}

export function feedHasMatchingTrainingReport(items: PackFeedItemDTO[], text: string, userId: number): boolean {
  const key = trainingReportFirstLineKey(text);
  return items.some(
    (i) => i.id > 0 && i.type === "training_done" && i.user_id === userId && trainingReportFirstLineKey(i.text) === key,
  );
}
