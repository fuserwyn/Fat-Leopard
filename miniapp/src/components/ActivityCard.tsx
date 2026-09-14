import { useRef, useState, useEffect, useLayoutEffect, useCallback, useMemo, type MouseEvent as ReactMouseEvent, type TouchEvent as ReactTouchEvent } from "react";
import { createPortal } from "react-dom";
import { LikersPopover, ReactionPickerPopover, useChipPress, useLikersPopover, type LikerGroup } from "./Likers";
import { PhotoCropper } from "./PhotoCropper";
import { PhotoLightbox } from "./PhotoLightbox";
import { CameraButton } from "./CameraButton";
import { LEO_AVATAR_URL } from "../lib/leoAvatar";
import { votersToLikers, type VoterDTO } from "../lib/packFeed";
import { clipboardImageFile } from "../lib/clipboardImage";
import { streakStreakAriaLabel } from "../lib/streakLabel";
import { hapticImpact } from "../lib/haptics";
import "./ActivityCard.css";

function avatarLooksLikeImageSrc(avatar: string): boolean {
  const t = avatar.trim();
  if (t.startsWith("/") || t.startsWith("http://") || t.startsWith("https://")) return true;
  return /\.[a-z0-9]{2,4}(\?|$)/i.test(t);
}

/** Обрезка по код-поинтам (не по UTF-16): не режет эмодзи пополам — иначе остаётся
 *  «битый» суррогат, который рендерится как квадрат-«бургер». */
function truncateByCodePoints(text: string, maxChars: number): string {
  const chars = Array.from(text);
  if (chars.length <= maxChars) return text.trim();
  return `${chars.slice(0, maxChars).join("").trim()}…`;
}

/** Запасной глиф, когда фото профиля не загрузилось (нет аватара в Telegram / приватность).
 *  У @username / #tag берём первую значащую букву — иначе ВСЕ такие ники показывали бы
 *  одинаковую лапку (как было с @arturio222). Разбор по код-поинтам не режет эмодзи. */
export function avatarFallbackGlyph(name: string): string {
  const t = (name || "").trim().replace(/^[@#]+/, "").trim();
  const first = Array.from(t)[0] ?? "";
  if (!first) return "🐾";
  if (/[a-zA-Zа-яА-Я0-9]/.test(first)) return first.toUpperCase();
  return first; // уже эмодзи/иной символ — оставляем как есть
}

/** Аватар участника в треде: при ошибке загрузки фото показывает инициал, а не «битую картинку». */
function ThreadMemberAvatar({ src, name }: { src: string; name: string }) {
  const [failed, setFailed] = useState(false);
  useEffect(() => setFailed(false), [src]);
  if (failed) {
    return (
      <span className="act-card__thread-avatar act-card__thread-avatar--member act-card__thread-avatar--fallback" aria-hidden>
        {avatarFallbackGlyph(name)}
      </span>
    );
  }
  return (
    <img
      className="act-card__thread-avatar act-card__thread-avatar--member"
      src={src}
      alt=""
      loading="lazy"
      referrerPolicy="no-referrer"
      onError={() => setFailed(true)}
    />
  );
}

/** Голос комментария админа: от себя, от имени Лео или от имени админов. */
export type ThreadPostAs = "self" | "leo" | "admin";

export type ActivityCardThreadReply = {
  id: number;
  author: string;
  text: string;
  timeAgo: string;
  isYou: boolean;
  /** Ответ Лео из треда ленты (тот же текст, что персонально в ЛС). */
  isLeo?: boolean;
  /** Комментарий опубликован «от имени админов». */
  isAdmin?: boolean;
  /** Реальное имя автора-админа за голосом Лео/Админ — приходит ТОЛЬКО админам. */
  adminName?: string;
  /** Аватар участника (TG), если уже синхронился с сервера. */
  authorPhotoUrl?: string;
  /** Фото, приложенное к комментарию (опционально). */
  photoUrl?: string;
  /** Цитата родителя (reply). */
  replyTo?: { author: string; text: string; isLeo?: boolean; isAdmin?: boolean };
  likeCount?: number;
  likeMe?: boolean;
  /** Имена лайкнувших комментарий (для поповера «кто лайкнул»). */
  likeVoters?: VoterDTO[] | string[];
  /** Комментарий редактировался после публикации. */
  edited?: boolean;
};

export type ActivityCardThreadComposer = {
  draft: string;
  onDraftChange: (v: string) => void;
  /** Текст берём из поля ввода в момент отправки (надёжнее в Telegram WebView, чем только React state).
   *  photo — опциональное фото к комментарию. postAs — голос: себя/Лео/админ (для админов). */
  onSubmit: (text: string, photo?: File | null, postAs?: ThreadPostAs) => void;
  posting: boolean;
  /** Режим правки своего комментария (id строки треда). */
  editReplyId?: number;
  onCancelEdit?: () => void;
};

export type ActivityCardPostEdit = {
  draft: string;
  onDraftChange: (v: string) => void;
  onSave: () => void;
  onCancel: () => void;
  posting: boolean;
  /** Текущее фото поста (готовый URL), если прикреплено. */
  photoUrl?: string;
  /** Прикрепить/заменить фото поста. */
  onAttachPhoto?: (file: File) => void;
  /** Удалить фото поста. */
  onRemovePhoto?: () => void;
  /** Идёт загрузка/удаление фото. */
  photoBusy?: boolean;
};

/** До трёх типов реакций всегда показываем все чипы — без «⋯». */
const TRAINING_REACTIONS_ALWAYS_INLINE = 3;

/** Минимум реакций в строке до «⋯», если типов больше TRAINING_REACTIONS_ALWAYS_INLINE. */
const TRAINING_REACTIONS_MIN_INLINE = 3;

/** Ширина строки + кнопка «ещё»: оценка px на кнопку (эмодзи + отступы + счётчик). */
export function trainingReactionVisibleCount(rowWidth: number, total: number): number {
  if (total <= 0) return 0;
  if (total <= TRAINING_REACTIONS_ALWAYS_INLINE) return total;
  if (rowWidth <= 0) return total;
  const chip = 48;
  const gap = 6;
  const moreBtn = 44;
  const widthAll = total * chip + Math.max(0, total - 1) * gap;
  if (widthAll <= rowWidth) return total;

  let best = 1;
  for (let k = total - 1; k >= 1; k--) {
    const used = k * chip + Math.max(0, k - 1) * gap + gap + moreBtn;
    if (used <= rowWidth) {
      best = k;
      break;
    }
  }
  return Math.min(total, Math.max(TRAINING_REACTIONS_MIN_INLINE, best));
}

function ReactionChip({
  r,
  disabled,
  onPick,
  onLongPress,
}: {
  r: { emoji: string; count: number; me?: boolean; voters?: VoterDTO[] | string[] };
  disabled?: boolean;
  onPick: (emoji: string) => void;
  /** Зажатие чипа (или hover на десктопе) — показать список всех отреагировавших. */
  onLongPress?: () => void;
}) {
  const press = useChipPress(() => onPick(r.emoji), onLongPress, disabled);
  return (
    <button
      type="button"
      className={`act-card__react-btn${r.me ? " act-card__react-btn--mine" : ""}`}
      disabled={disabled}
      {...press}
    >
      {r.emoji}
      {r.count > 0 && <span className="act-card__react-cnt">{r.count}</span>}
    </button>
  );
}

/** Лайк комментария: тап = поставить/снять, зажатие = показать, кто лайкнул. */
function ThreadLikeButton({
  count,
  mine,
  voters,
  onToggle,
}: {
  count: number;
  mine?: boolean;
  voters?: VoterDTO[] | string[];
  onToggle: () => void;
}) {
  const anchorRef = useRef<HTMLSpanElement>(null);
  const { open, setOpen, style, popRef } = useLikersPopover(anchorRef);
  const hasVoters = Array.isArray(voters) && voters.length > 0;
  const groups: LikerGroup[] = hasVoters ? [{ emoji: "❤️", voters: votersToLikers(voters) }] : [];
  const press = useChipPress(onToggle, hasVoters ? () => setOpen(true) : undefined);
  return (
    <span className="act-card__thread-like-wrap" ref={anchorRef}>
      <button type="button" className={`act-card__thread-like${mine ? " act-card__thread-like--mine" : ""}`} {...press}>
        ❤️ {count}
      </button>
      {open && hasVoters && <LikersPopover groups={groups} popRef={popRef} style={style} label="Кто лайкнул" />}
    </span>
  );
}

export function TrainingReactionsBar({
  reactions,
  onReactionClick,
}: {
  reactions: { emoji: string; count: number; me: boolean; voters?: VoterDTO[] | string[] }[];
  onReactionClick?: (emoji: string) => void;
}) {
  const rowRef = useRef<HTMLDivElement>(null);
  const moreRef = useRef<HTMLDivElement>(null);
  const [visibleCount, setVisibleCount] = useState(reactions.length);
  const { open: likersOpen, setOpen: setLikersOpen, style: likersStyle, popRef } = useLikersPopover(rowRef);
  // Меню доп. реакций «⋯» — портал с тем же позиционированием (не обрезается экраном).
  const { open: menuOpen, setOpen: setMenuOpen, style: menuStyle, popRef: menuPopRef } = useLikersPopover(moreRef);

  // Группы «кто отреагировал»: только эмодзи с реальными голосами.
  const likerGroups: LikerGroup[] = reactions
    .filter((r) => r.count > 0 && Array.isArray(r.voters) && r.voters.length > 0)
    .map((r) => ({ emoji: r.emoji, voters: votersToLikers(r.voters) }));
  const hasLikers = likerGroups.length > 0;

  useLayoutEffect(() => {
    const el = rowRef.current;
    if (!el) return;
    const measure = () => {
      const w = el.clientWidth;
      if (w <= 0) return;
      setVisibleCount(trainingReactionVisibleCount(w, reactions.length));
    };
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, [reactions.length]);

  const n = reactions.length;
  const showAll = visibleCount >= n;
  const visible = showAll ? reactions : reactions.slice(0, visibleCount);
  const hidden = showAll ? [] : reactions.slice(visibleCount);

  const pick = (emoji: string) => {
    onReactionClick?.(emoji);
    setMenuOpen(false);
  };

  const openLikers = () => {
    if (hasLikers) setLikersOpen(true);
  };

  return (
    <div className="act-card__react-inner" ref={rowRef}>
      <div className="act-card__react-inline">
        {visible.map((r) => (
          <ReactionChip
            key={r.emoji}
            r={r}
            disabled={onReactionClick == null}
            onPick={pick}
            onLongPress={hasLikers ? openLikers : undefined}
          />
        ))}
        {hidden.length > 0 && (
          <div className="act-card__react-more" ref={moreRef}>
            <button
              type="button"
              className={`act-card__react-toggle${menuOpen ? " act-card__react-toggle--open" : ""}`}
              aria-expanded={menuOpen}
              aria-haspopup="menu"
              aria-label="Ещё реакции"
              onClick={() => setMenuOpen(!menuOpen)}
            >
              ⋯
            </button>
            {menuOpen &&
              createPortal(
                <div className="act-card__react-popover" role="menu" ref={menuPopRef} style={menuStyle}>
                  {hidden.map((r) => (
                    <ReactionChip
                      key={r.emoji}
                      r={r}
                      disabled={onReactionClick == null}
                      onPick={pick}
                      onLongPress={hasLikers ? openLikers : undefined}
                    />
                  ))}
                </div>,
                document.body,
              )}
          </div>
        )}
      </div>
      {likersOpen && hasLikers && <LikersPopover groups={likerGroups} popRef={popRef} style={likersStyle} />}
    </div>
  );
}

export function ReportActionMenu({
  items,
  posting = false,
  className = "",
}: {
  items: { label: string; onClick: () => void; danger?: boolean }[];
  posting?: boolean;
  className?: string;
}) {
  const toggleRef = useRef<HTMLButtonElement>(null);
  const { open, setOpen, style, popRef } = useLikersPopover(toggleRef);

  if (items.length === 0) return null;

  return (
    <div className={`act-card__menu${className ? ` ${className}` : ""}`}>
      <button
        ref={toggleRef}
        type="button"
        className={`act-card__menu-toggle${open ? " act-card__menu-toggle--open" : ""}`}
        aria-expanded={open}
        aria-haspopup="menu"
        aria-label="Ещё"
        disabled={posting}
        onClick={() => setOpen(!open)}
      >
        {posting ? "…" : "⋯"}
      </button>
      {open &&
        !posting &&
        createPortal(
          <div
            className="act-card__menu-popover act-card__menu-popover--portal"
            role="menu"
            ref={popRef}
            style={style}
          >
            {items.map((item) => (
              <button
                key={item.label}
                type="button"
                className={`act-card__menu-item${item.danger ? " act-card__menu-item--danger" : ""}`}
                role="menuitem"
                onClick={() => {
                  setOpen(false);
                  item.onClick();
                }}
              >
                {item.label}
              </button>
            ))}
          </div>,
          document.body,
        )}
    </div>
  );
}

export type ActivityCardProps = {
  avatar: string;
  name: string;
  streak: number;
  /** Для системных карточек (приветствие Лео) — не показывать стрик. */
  hideStreak?: boolean;
  /** Текст никогда не сворачивать: «Мудрость дня» читают целиком, без «Показать полностью». */
  commentAlwaysFull?: boolean;
  /** Светлая подсветка для специальных карточек админа/системы. */
  lightTone?: boolean;
  timeAgo: string;
  emoji: string;
  activity: string;
  details: string;
  comment?: string;
  poll?: {
    totalVotes: number;
    options: { label: string; votes: number; selected?: boolean }[];
    onVote?: (optionIndex: number) => void;
  };
  aiText?: string;
  reactions?: { emoji: string; count: number; me: boolean; voters?: VoterDTO[] | string[] }[];
  /** Клик по эмодзи (лента training_done). */
  onReactionClick?: (emoji: string) => void;
  /** Тред под отчётом о тренировке. */
  threadReplies?: ActivityCardThreadReply[];
  threadComposer?: ActivityCardThreadComposer;
  /** Удалить свою реплику в треде (id строки miniapp_training_feed_thread). */
  onThreadReplyDelete?: (threadReplyId: number) => void;
  /** Выбрать сообщение для ответа (как Reply в Telegram). */
  onThreadReplyIntent?: (payload: { replyToThreadId: number; authorLabel: string; excerpt: string }) => void;
  onThreadReplyLike?: (threadReplyId: number) => void;
  /** Режим ответа на сообщение в треде (до отправки). */
  threadReplyIntent?: { replyToThreadId: number; authorLabel: string; excerpt: string } | null;
  onCancelThreadReplyIntent?: () => void;
  /** Фото к отчёту #training_done (URL с бэкенда). */
  trainingPhotoUrl?: string;
  /** Состояние удаления строк треда (id → отправка). */
  threadReplyDeleting?: Record<number, boolean>;
  /** Подписаться/отписаться от автора карточки (только чужие тренировки). */
  onToggleFollow?: () => void;
  /** Viewer уже подписан на автора (для текста пункта меню). */
  isFriend?: boolean;
  followPosting?: boolean;
  /** Пожаловаться на пост (не свой). */
  onReport?: () => void;
  reportPosting?: boolean;
  /** Скрыть пост (админ; можно вернуть в «Скрытое»). */
  onAdminDelete?: () => void;
  adminDeletePosting?: boolean;
  /** Объявление закреплено сверху ленты (пометка «закреплено» + сворачиваемый текст). */
  pinned?: boolean;
  /** Текст-объявление сворачивается (показать полностью / свернуть). */
  commentCollapsible?: boolean;
  /** Закрепить/открепить объявление (админ). Текст пункта зависит от pinned. */
  onTogglePin?: () => void;
  pinPosting?: boolean;
  /** Зритель — админ: может удалять чужие комментарии в треде (модерация). */
  isAdmin?: boolean;
  /** Админ может оставить комментарий от имени Лео (доступно в админских постах). */
  adminVoiceAvailable?: boolean;
  /** Пожаловаться на комментарий в треде. */
  onThreadReplyReport?: (threadReplyId: number) => void;
  threadReplyReporting?: Record<number, boolean>;
  /** Начать правку своего поста (training_done / healthy). */
  onStartEditPost?: () => void;
  /** Режим правки текста поста. */
  postEdit?: ActivityCardPostEdit;
  /** Пост редактировался после публикации. */
  commentEdited?: boolean;
  /** Начать правку своего комментария (только голос «от себя»). */
  onThreadReplyEdit?: (threadReplyId: number, text: string) => void;
  /** Есть непрочитанные ответы в треде (участник или Лео). */
  hasUnreadThread?: boolean;
  /** Пользователь открыл тред — сбросить локальный/серверный unread. */
  onThreadOpened?: () => void;
  /** Лео-реплай показан пользователю (тред раскрыт) — для аналитики leo_comment_displayed. */
  onLeoReplyDisplayed?: (threadReplyId: number) => void;
};

export function ActivityCard({
  avatar,
  name,
  streak,
  hideStreak = false,
  commentAlwaysFull = false,
  lightTone = false,
  timeAgo,
  emoji,
  activity,
  details,
  comment,
  poll,
  aiText,
  reactions = [],
  onReactionClick,
  threadReplies = [],
  threadComposer,
  onThreadReplyDelete,
  onThreadReplyIntent,
  onThreadReplyLike,
  threadReplyIntent,
  onCancelThreadReplyIntent,
  threadReplyDeleting = {},
  isAdmin = false,
  adminVoiceAvailable = false,
  onLeoReplyDisplayed,
  trainingPhotoUrl,
  onToggleFollow,
  isFriend = false,
  followPosting = false,
  onReport,
  reportPosting = false,
  onAdminDelete,
  adminDeletePosting = false,
  pinned = false,
  commentCollapsible = false,
  onTogglePin,
  pinPosting = false,
  onThreadReplyReport,
  threadReplyReporting = {},
  onStartEditPost,
  postEdit,
  commentEdited = false,
  onThreadReplyEdit,
  hasUnreadThread = false,
  onThreadOpened,
}: ActivityCardProps) {
  const threadBodyRef = useRef<HTMLDivElement>(null);
  const threadComposeRef = useRef<HTMLDivElement>(null);
  const threadInputRef = useRef<HTMLTextAreaElement>(null);
  const [threadOpen, setThreadOpen] = useState(false);
  const [threadInputFocused, setThreadInputFocused] = useState(false);
  // Закреплённое объявление: длинный текст свёрнут, разворачивается по кнопке.
  const [commentExpanded, setCommentExpanded] = useState(false);
  // Объявление: одна строка. Обычный длинный комментарий: 5 строк, чтобы карточка
  // не занимала весь экран на телефоне.
  const COMMENT_COLLAPSE_THRESHOLD = 80;
  const LONG_COMMENT_THRESHOLD = 180;
  const commentText = typeof comment === "string" ? comment : "";
  const isAdminCollapse =
    !commentAlwaysFull &&
    commentCollapsible &&
    (commentText.includes("\n") || commentText.trim().length > COMMENT_COLLAPSE_THRESHOLD);
  // Карточка Лео («Мудрость дня» и его реплики) должна занимать в ленте столько
  // же, сколько отчёт участника: там одна строка описания. Поэтому у Лео порог
  // сворачивания ниже, а свёрнутый текст — две строки вместо пяти.
  const isLeoCard = hideStreak;
  const isLongComment =
    !commentCollapsible &&
    !commentAlwaysFull &&
    (commentText.split("\n").length > (isLeoCard ? 2 : 5) ||
      commentText.trim().length > (isLeoCard ? 110 : LONG_COMMENT_THRESHOLD));
  const canCollapseComment = isAdminCollapse || isLongComment;
  const commentClampClass = isAdminCollapse ? "act-card__comment--clamped" : "act-card__comment--clamped-soft";
  const [lightboxOpen, setLightboxOpen] = useState(false);
  // Источник для лайтбокса: фото отчёта (по умолчанию) или фото из комментария.
  const [lightboxSrc, setLightboxSrc] = useState<string | null>(null);
  const [photoFailed, setPhotoFailed] = useState(false);
  // Счётчик перезагрузки фото: тап по заглушке ремонтирует <img> с cache-bust,
  // чтобы 404 из-за эфемерного/другого инстанса можно было пере-запросить, а не терять фото молча.
  const [photoReloadKey, setPhotoReloadKey] = useState(0);
  const [avatarFailed, setAvatarFailed] = useState(false);
  // Прикреплённое к комментарию фото (локально в композере до отправки).
  const threadPhotoInputRef = useRef<HTMLInputElement>(null);
  const [threadPhoto, setThreadPhoto] = useState<File | null>(null);
  const [threadPhotoPreview, setThreadPhotoPreview] = useState<string | null>(null);
  // Фото для комментария кропаем перед прикреплением (как в отчёте о тренировке).
  const [pendingThreadCrop, setPendingThreadCrop] = useState<File | null>(null);
  // Фото поста при добавлении/замене тоже кропаем — иначе можно залить любой размер.
  const [pendingPostEditCrop, setPendingPostEditCrop] = useState<File | null>(null);
  // Голос комментария для админа: от себя / от имени Лео / от имени админов.
  const [threadPostAs, setThreadPostAs] = useState<ThreadPostAs>("self");
  const prevThreadLen = useRef(threadReplies.length);
  const prevThreadOpenRef = useRef(false);

  // Превью прикреплённого фото: создаём/освобождаем object URL.
  useEffect(() => {
    if (!threadPhoto) {
      setThreadPhotoPreview(null);
      return;
    }
    const url = URL.createObjectURL(threadPhoto);
    setThreadPhotoPreview(url);
    return () => URL.revokeObjectURL(url);
  }, [threadPhoto]);

  const openLightbox = (src?: string) => {
    if (!src) return;
    setLightboxSrc(src);
    setLightboxOpen(true);
  };

  const submitThreadComment = () => {
    if (!threadComposer || threadComposer.posting) return;
    const raw = threadInputRef.current?.value ?? threadComposer.draft;
    const editing = threadComposer.editReplyId != null && threadComposer.editReplyId > 0;
    if (raw.trim() === "" && !threadPhoto && !editing) return;
    if (editing && raw.trim() === "") return;
    threadComposer.onSubmit(raw, editing ? null : threadPhoto, adminVoiceAvailable ? threadPostAs : "self");
    if (!editing) {
      setThreadPhoto(null);
      if (threadPhotoInputRef.current) threadPhotoInputRef.current.value = "";
    }
  };

  useEffect(() => {
    if (threadOpen && !prevThreadOpenRef.current) {
      onThreadOpened?.();
      // Тред раскрыт — Лео-реплаи теперь видны. Дедуп по id на стороне родителя.
      if (onLeoReplyDisplayed) {
        for (const tr of threadReplies) {
          if (tr.isLeo && tr.id > 0) onLeoReplyDisplayed(tr.id);
        }
      }
    }
    prevThreadOpenRef.current = threadOpen;
  }, [threadOpen, onThreadOpened, onLeoReplyDisplayed, threadReplies]);

  const scrollThreadComposeIntoView = useCallback(() => {
    const target = threadComposeRef.current ?? threadInputRef.current;
    if (!target) return;
    const scroll = () => {
      target.scrollIntoView({ block: "end", inline: "nearest" });
    };
    scroll();
    window.setTimeout(scroll, 80);
    window.setTimeout(scroll, 280);
  }, []);

  const onThreadComposeFocus = useCallback(() => {
    if (!threadOpen) setThreadOpen(true);
    setThreadInputFocused(true);
    scrollThreadComposeIntoView();
  }, [scrollThreadComposeIntoView, threadOpen]);

  const onThreadComposeBlur = useCallback(() => {
    window.setTimeout(() => {
      const active = document.activeElement;
      if (active === threadInputRef.current || threadComposeRef.current?.contains(active)) return;
      setThreadInputFocused(false);
    }, 120);
  }, []);

  useEffect(() => {
    if (!threadInputFocused) return;
    const vv = window.visualViewport;
    if (!vv) return;
    const onViewportChange = () => scrollThreadComposeIntoView();
    vv.addEventListener("resize", onViewportChange);
    vv.addEventListener("scroll", onViewportChange);
    return () => {
      vv.removeEventListener("resize", onViewportChange);
      vv.removeEventListener("scroll", onViewportChange);
    };
  }, [threadInputFocused, scrollThreadComposeIntoView]);

  useEffect(() => {
    setPhotoFailed(false);
    setPhotoReloadKey(0);
  }, [trainingPhotoUrl]);

  useEffect(() => {
    setAvatarFailed(false);
  }, [avatar]);

  useEffect(() => {
    if (threadOpen && threadReplies.length > prevThreadLen.current) {
      requestAnimationFrame(() => {
        const body = threadBodyRef.current;
        if (body) body.scrollTop = body.scrollHeight;
      });
    }
    prevThreadLen.current = threadReplies.length;
  }, [threadOpen, threadReplies.length]);

  // Escape/скролл-лок для лайтбокса живут внутри PhotoLightbox.

  // Реакции с реальными голосами показываем строкой под постом (как «уже поставленные»
  // реакции в Telegram). Пустой набор ничего не рисует.
  const activeReactions = reactions.filter((r) => r.count > 0);
  const canReact = onReactionClick != null;
  const hasThread = threadReplies.length > 0 || threadComposer != null;
  const threadCount = threadReplies.length;
  // Реакции — в одной строке с типом тренировки (если тип есть). У сообщений типа
  // нет, поэтому там реакции остаются отдельной строкой под текстом.
  const hasTypeLine = (emoji ?? "").trim() !== "" || (activity ?? "").trim() !== "";
  // У сообщений (нет типа) реакции ставим в одну строку с текстом — если это
  // обычный короткий текст (не сворачиваемый, не режим правки).
  const reactionInComment =
    !hasTypeLine && activeReactions.length > 0 && !postEdit && Boolean(comment) && !canCollapseComment;
  // Компактный вид (в строке с типом/текстом) vs полоса на всю ширину (отдельной строкой).
  const reactionInline = hasTypeLine || reactionInComment;
  const reactionsNode =
    activeReactions.length > 0 ? (
      <div
        className={`act-card__react${reactionInline ? " act-card__react--inline" : ""}`}
        role="group"
        aria-label="Реакции"
      >
        <TrainingReactionsBar reactions={activeReactions} onReactionClick={onReactionClick} />
      </div>
    ) : null;
  const showStreak = !hideStreak && name.trim() !== "Админ";
  const cardHeadMenuItems = useMemo(() => {
    const items: { label: string; onClick: () => void; danger?: boolean }[] = [];
    if (onToggleFollow) {
      items.push({ label: isFriend ? "Не следить за тренировками" : "Следить за тренировками", onClick: onToggleFollow });
    }
    if (onTogglePin) {
      items.push({ label: pinned ? "Открепить" : "Закрепить", onClick: onTogglePin });
    }
    if (onAdminDelete) {
      items.push({ label: "Скрыть пост", onClick: onAdminDelete, danger: true });
    }
    if (onStartEditPost) {
      items.push({ label: "Изменить", onClick: onStartEditPost });
    }
    if (onReport) {
      items.push({ label: "Пожаловаться на публикацию", onClick: onReport });
    }
    return items;
  }, [onToggleFollow, isFriend, onTogglePin, pinned, onAdminDelete, onStartEditPost, onReport]);
  const cardHeadMenuPosting = reportPosting || adminDeletePosting || pinPosting || followPosting;

  // Как в Telegram: реакций на посте не видно, ряд эмодзи для выбора всплывает
  // при тапе/зажатии по самому посту. Тапы по кнопкам/ссылкам/фото/треду/меню — не считаются.
  const cardRef = useRef<HTMLElement>(null);
  const picker = useLikersPopover(cardRef);

  // Тап по посту не должен считаться, если пришёлся на интерактивный элемент.
  const isInteractiveTarget = (target: EventTarget | null): boolean => {
    const el = target as HTMLElement | null;
    return !!el?.closest(
      'button, a, input, textarea, label, select, [role="button"], [role="menuitem"], [role="dialog"], [contenteditable="true"], .act-card__thread, .act-card__photo-wrap, .act-card__poll, .act-card__lightbox, .act-card__likers, .act-card__react-picker',
    );
  };
  const openPicker = (target: EventTarget | null) => {
    if (!canReact) return;
    if (isInteractiveTarget(target)) return;
    if (!picker.open) hapticImpact("light"); // тактильный отклик на появление ряда эмодзи
    picker.setOpen(true);
  };

  // Тач: только long-press открывает пикер — короткий тап и свайп остаются скроллу ленты.
  // Мышь/десктоп: обычный клик по посту (как в Telegram Desktop).
  const pressStart = useRef<{ x: number; y: number } | null>(null);
  const pressMoved = useRef(false);
  const pressFired = useRef(false);
  const recentTouch = useRef(0);
  const lpTimer = useRef<number | null>(null);
  const clearLpTimer = () => {
    if (lpTimer.current != null) {
      window.clearTimeout(lpTimer.current);
      lpTimer.current = null;
    }
  };

  const onCardTouchStart = (e: ReactTouchEvent) => {
    pressFired.current = false;
    pressMoved.current = false;
    const t = e.touches[0];
    pressStart.current = t ? { x: t.clientX, y: t.clientY } : null;
    const target = e.target;
    clearLpTimer();
    if (!canReact || isInteractiveTarget(target)) return;
    lpTimer.current = window.setTimeout(() => {
      if (pressMoved.current) return;
      pressFired.current = true;
      openPicker(target);
    }, 420);
  };
  const onCardTouchMove = (e: ReactTouchEvent) => {
    const t = e.touches[0];
    const s = pressStart.current;
    if (t && s && (Math.abs(t.clientX - s.x) > 14 || Math.abs(t.clientY - s.y) > 14)) {
      pressMoved.current = true;
      clearLpTimer();
    }
  };
  const onCardTouchEnd = (e: ReactTouchEvent) => {
    clearLpTimer();
    recentTouch.current = Date.now();
    if (pressFired.current) {
      e.preventDefault();
    }
  };
  const onCardClick = (e: ReactMouseEvent) => {
    if (Date.now() - recentTouch.current < 600) return; // click после тача — не открываем повторно
    openPicker(e.target);
  };

  return (
    <article
      ref={cardRef}
      onClick={onCardClick}
      onTouchStart={onCardTouchStart}
      onTouchMove={onCardTouchMove}
      onTouchEnd={onCardTouchEnd}
      onTouchCancel={clearLpTimer}
      className={`act-card${hideStreak ? " act-card--leo" : ""}${lightTone ? " act-card--light" : ""}${threadOpen && hasThread ? " act-card--thread-open" : ""}${trainingPhotoUrl ? " act-card--has-photo" : ""}${canCollapseComment && commentExpanded ? " act-card--comment-expanded" : ""}${canReact ? " act-card--reactable" : ""}`}
    >
      {picker.open && canReact && (
        <ReactionPickerPopover
          reactions={reactions}
          popRef={picker.popRef}
          style={picker.style}
          onPick={(emoji) => {
            onReactionClick?.(emoji);
            picker.setOpen(false);
          }}
        />
      )}
      <header className="act-card__head">
        <div className="act-card__avatar" aria-hidden>
          {avatarLooksLikeImageSrc(avatar) && !avatarFailed ? (
            <img
              className="act-card__avatar-img"
              src={avatar.trim()}
              alt=""
              loading="lazy"
              referrerPolicy="no-referrer"
              onError={() => setAvatarFailed(true)}
            />
          ) : avatarLooksLikeImageSrc(avatar) ? (
            avatarFallbackGlyph(name)
          ) : (
            avatar
          )}
        </div>
        <div className="act-card__meta">
          <div className="act-card__row act-card__row--name">
            <span className="act-card__name">{name}</span>
            {pinned && (
              <span className="act-card__pinned-badge" aria-label="Закреплённое объявление">
                📌 закреплено
              </span>
            )}
          </div>
          <div className="act-card__row act-card__row--sub">
            <p className="act-card__time">
              {timeAgo}
              {commentEdited ? <span className="act-card__edited"> · изменено</span> : null}
            </p>
            {showStreak && (
              <span
                className="pill pill--streak"
                aria-label={streakStreakAriaLabel(streak)}
                title={streakStreakAriaLabel(streak)}
              >
                <span className="pill__streak-word">Стрик</span>
                <span className="pill__streak-num">{streak}</span>
              </span>
            )}
          </div>
        </div>
        {cardHeadMenuItems.length > 0 && (
          <ReportActionMenu
            className="act-card__menu--head"
            items={cardHeadMenuItems}
            posting={cardHeadMenuPosting}
          />
        )}
      </header>
      <div className="act-card__body">
        {/* У сообщений (не тренировок) нет типа/эмодзи — не рисуем пустую строку-заголовок.
            Реакции показываем справа в этой же строке (перенос из отдельного блока). */}
        {hasTypeLine && (
          <div className="act-card__type-row">
            <p className="act-card__type">
              <span className="act-card__type-ico">{emoji}</span> {activity}
            </p>
            {reactionsNode}
          </div>
        )}
        {details.trim() !== "" && <p className="act-card__details">{details}</p>}
        {postEdit ? (
          <div className="act-card__post-edit">
            <textarea
              className="act-card__post-edit-input"
              rows={4}
              value={postEdit.draft}
              onChange={(e) => postEdit.onDraftChange(e.target.value)}
              maxLength={4000}
              disabled={postEdit.posting}
            />
            {(postEdit.onAttachPhoto || postEdit.photoUrl) && (
              <div className="act-card__post-edit-photo">
                {postEdit.photoUrl ? (
                  <img className="act-card__post-edit-photo-img" src={postEdit.photoUrl} alt="" referrerPolicy="no-referrer" />
                ) : null}
                <div className="act-card__post-edit-photo-actions">
                  {postEdit.onAttachPhoto && (
                    <label className={`act-card__post-edit-photo-btn${postEdit.photoBusy || postEdit.posting ? " act-card__post-edit-photo-btn--disabled" : ""}`}>
                      {postEdit.photoBusy ? "…" : postEdit.photoUrl ? "🖼 Из галереи" : "🖼 Галерея"}
                      <input
                        type="file"
                        accept="image/*"
                        hidden
                        disabled={postEdit.photoBusy || postEdit.posting}
                        onChange={(e) => {
                          const f = e.target.files?.[0];
                          e.target.value = "";
                          if (f) setPendingPostEditCrop(f);
                        }}
                      />
                    </label>
                  )}
                  {postEdit.onAttachPhoto && (
                    <CameraButton
                      className={`act-card__post-edit-photo-btn${postEdit.photoBusy || postEdit.posting ? " act-card__post-edit-photo-btn--disabled" : ""}`}
                      disabled={postEdit.photoBusy || postEdit.posting}
                      ariaLabel="Сделать фото"
                      onChange={(e) => {
                        const f = e.target.files?.[0];
                        if (f) setPendingPostEditCrop(f);
                      }}
                    >
                      📷 Камера
                    </CameraButton>
                  )}
                  {postEdit.photoUrl && postEdit.onRemovePhoto && (
                    <button
                      type="button"
                      className="act-card__post-edit-photo-remove"
                      disabled={postEdit.photoBusy || postEdit.posting}
                      onClick={() => postEdit.onRemovePhoto?.()}
                    >
                      Удалить фото
                    </button>
                  )}
                </div>
              </div>
            )}
            <div className="act-card__post-edit-actions">
              <button type="button" className="act-card__post-edit-cancel" disabled={postEdit.posting} onClick={postEdit.onCancel}>
                Отмена
              </button>
              <button type="button" className="act-card__post-edit-save" disabled={postEdit.posting} onClick={postEdit.onSave}>
                {postEdit.posting ? "…" : "Сохранить"}
              </button>
            </div>
            {pendingPostEditCrop &&
              createPortal(
                <PhotoCropper
                  file={pendingPostEditCrop}
                  onCancel={() => setPendingPostEditCrop(null)}
                  onConfirm={(cropped) => {
                    postEdit.onAttachPhoto?.(cropped);
                    setPendingPostEditCrop(null);
                  }}
                />,
                document.body,
              )}
          </div>
        ) : (
          comment &&
          (canCollapseComment ? (
            <div className="act-card__comment-collapsible">
              <p
                className={`act-card__comment${commentExpanded ? "" : ` ${commentClampClass}`}`}
              >
                {comment}
              </p>
              <button
                type="button"
                className="act-card__comment-toggle"
                aria-expanded={commentExpanded}
                onClick={() => setCommentExpanded((v) => !v)}
              >
                {commentExpanded ? "Свернуть" : "Показать полностью"}
              </button>
            </div>
          ) : reactionInComment ? (
            <div className="act-card__msg-row">
              <p className="act-card__comment">{comment}</p>
              {reactionsNode}
            </div>
          ) : (
            <p className="act-card__comment">{comment}</p>
          ))
        )}
        {poll && poll.options.length > 0 && (
          <div className="act-card__poll" role="group" aria-label="Опрос">
            {poll.options.map((option, optionIndex) => (
              <button
                key={`${optionIndex}-${option.label}`}
                type="button"
                className={`act-card__poll-option${option.selected ? " act-card__poll-option--selected" : ""}`}
                onClick={() => poll.onVote?.(optionIndex)}
              >
                <span className="act-card__poll-label">{option.label}</span>
                <span className="act-card__poll-votes">{option.votes}</span>
              </button>
            ))}
            <p className="act-card__poll-total">{poll.totalVotes} голосов</p>
          </div>
        )}
        {trainingPhotoUrl && !photoFailed ? (
          <div
            className="act-card__photo-wrap"
            role="button"
            tabIndex={0}
            aria-label="Открыть фото"
            onClick={() => openLightbox(trainingPhotoUrl)}
            onKeyDown={(e) => {
              if (e.key === "Enter" || e.key === " ") {
                e.preventDefault();
                openLightbox(trainingPhotoUrl);
              }
            }}
          >
            <img
              key={photoReloadKey}
              className="act-card__photo"
              src={photoReloadKey > 0 ? `${trainingPhotoUrl}${trainingPhotoUrl.includes("?") ? "&" : "?"}r=${photoReloadKey}` : trainingPhotoUrl}
              alt=""
              loading="lazy"
              referrerPolicy="no-referrer"
              onError={() => setPhotoFailed(true)}
            />
          </div>
        ) : trainingPhotoUrl && photoFailed ? (
          // Фото не загрузилось (частая причина — 404 с эфемерного диска/другого инстанса).
          // Показываем заглушку с ретраем вместо молчаливого исчезновения, чтобы не путать с багом темы.
          <button
            type="button"
            className="act-card__photo-fallback"
            onClick={() => {
              setPhotoFailed(false);
              setPhotoReloadKey((k) => k + 1);
            }}
          >
            <span className="act-card__photo-fallback-icon" aria-hidden>🖼️</span>
            <span className="act-card__photo-fallback-text">Фото не загрузилось — нажми, чтобы повторить</span>
          </button>
        ) : null}
        {aiText && (
          <div className="act-card__ai">
            <div className="act-card__ai-label">AI</div>
            <p className="act-card__ai-text">{aiText}</p>
          </div>
        )}
      </div>
      {!hasTypeLine && !reactionInComment && reactionsNode}
      {hasThread && (
        <div className="act-card__thread">
            <button
              type="button"
              className={`act-card__thread-toggle${hasUnreadThread && !threadOpen ? " act-card__thread-toggle--unread" : ""}`}
              aria-expanded={threadOpen}
              onClick={() => setThreadOpen((v) => !v)}
            >
              <span>
                {threadOpen
                  ? "Свернуть комментарии"
                  : threadCount > 0
                    ? `Комментарии · ${threadCount}`
                    : "Комментарии"}
                {hasUnreadThread && !threadOpen ? (
                  <span className="act-card__thread-unread-dot" aria-label="Непрочитанные ответы" />
                ) : null}
              </span>
              <span className="act-card__thread-chevron" aria-hidden>
                {threadOpen ? "▲" : "▼"}
              </span>
            </button>
            {threadOpen && (
              <>
                <div className="act-card__thread-body" ref={threadBodyRef}>
                  {threadReplies.length > 0 && (
                    <ul className="act-card__thread-list">
                      {threadReplies.map((tr) => {
                        const leo = Boolean(tr.isLeo);
                        const adminVoice = Boolean(tr.isAdmin);
                        const displayAuthor = leo ? "Лео" : adminVoice ? "Админ" : tr.isYou ? "Ты" : tr.author;
                        // Атрибуция «(имя · админ)» приходит ТОЛЬКО админам — юзеры её не видят.
                        const attribution = tr.adminName
                          ? leo
                            ? `${tr.adminName} · админ`
                            : tr.adminName
                          : "";
                        return (
                          <li
                            key={tr.id}
                            className={`act-card__thread-item${tr.isYou ? " act-card__thread-item--you" : ""}${leo ? " act-card__thread-item--leo" : ""}${adminVoice ? " act-card__thread-item--admin" : ""}`}
                          >
                            <div className={`act-card__thread-item-inner${leo || adminVoice || tr.authorPhotoUrl?.trim() ? " act-card__thread-item-inner--has-avatar" : ""}`}>
                              {leo ? (
                                <img
                                  className="act-card__thread-avatar"
                                  src={LEO_AVATAR_URL}
                                  alt=""
                                  loading="lazy"
                                />
                              ) : adminVoice ? (
                                <span className="act-card__thread-avatar act-card__thread-avatar--admin" aria-hidden>
                                  📢
                                </span>
                              ) : tr.authorPhotoUrl?.trim() ? (
                                <ThreadMemberAvatar src={tr.authorPhotoUrl.trim()} name={displayAuthor} />
                              ) : null}
                              <div className="act-card__thread-item-main">
                                <div className="act-card__thread-item-head">
                                  <div className="act-card__thread-item-meta">
                                    <div className="act-card__thread-name-row">
                                      <span className="act-card__thread-name-text">
                                        <span className="act-card__thread-author">{displayAuthor}</span>
                                        {attribution && (
                                          <span className="act-card__thread-attrib muted">({attribution})</span>
                                        )}
                                      </span>
                                      {!tr.isYou && !leo && onThreadReplyReport != null && (
                                        <ReportActionMenu
                                          className="act-card__menu--thread"
                                          items={[
                                            {
                                              label: "Пожаловаться на комментарий",
                                              onClick: () => onThreadReplyReport(tr.id),
                                            },
                                          ]}
                                          posting={Boolean(threadReplyReporting[tr.id])}
                                        />
                                      )}
                                    </div>
                                    <div className="act-card__thread-subrow">
                                      <span className="act-card__thread-time muted">
                                        {tr.timeAgo}
                                        {tr.edited ? <span className="act-card__edited"> · изменено</span> : null}
                                      </span>
                                      <div className="act-card__thread-item-actions">
                                        {tr.isYou && !leo && !adminVoice && onThreadReplyEdit != null && (
                                          <button
                                            type="button"
                                            className="act-card__thread-edit"
                                            onClick={() => {
                                              setThreadOpen(true);
                                              onThreadReplyEdit(tr.id, tr.text);
                                              window.setTimeout(() => threadInputRef.current?.focus(), 80);
                                            }}
                                          >
                                            Изменить
                                          </button>
                                        )}
                                        {onThreadReplyDelete != null && (tr.isYou || isAdmin) && (
                                          <button
                                            type="button"
                                            className="act-card__thread-del"
                                            disabled={Boolean(threadReplyDeleting[tr.id])}
                                            onClick={() => onThreadReplyDelete(tr.id)}
                                          title={!tr.isYou ? "Скрыть как админ" : undefined}
                                        >
                                          {threadReplyDeleting[tr.id] ? "…" : tr.isYou ? "Удалить" : "Скрыть"}
                                          </button>
                                        )}
                                      </div>
                                    </div>
                                  </div>
                                </div>
                                {tr.replyTo != null &&
                                  ((tr.replyTo.text || "").trim() !== "" || (tr.replyTo.author || "").trim() !== "") && (
                                    <div className="act-card__thread-quote" aria-label="Ответ на сообщение">
                                      <span className="act-card__thread-quote-author muted">
                                        {tr.replyTo.isLeo ? "Лео" : tr.replyTo.isAdmin ? "Админ" : tr.replyTo.author}
                                      </span>
                                      {(tr.replyTo.text || "").trim() !== "" && (
                                        <p className="act-card__thread-quote-text">{(tr.replyTo.text || "").trim()}</p>
                                      )}
                                    </div>
                                  )}
                                {(tr.text || "").trim() !== "" && (
                                  <p className="act-card__thread-text">{tr.text}</p>
                                )}
                                {tr.photoUrl ? (
                                  <button
                                    type="button"
                                    className="act-card__thread-photo-wrap"
                                    aria-label="Открыть фото"
                                    onClick={() => openLightbox(tr.photoUrl)}
                                  >
                                    <img
                                      className="act-card__thread-photo"
                                      src={tr.photoUrl}
                                      alt=""
                                      loading="lazy"
                                      referrerPolicy="no-referrer"
                                    />
                                  </button>
                                ) : null}
                                {(onThreadReplyLike != null || onThreadReplyIntent != null) && (
                                  <div className="act-card__thread-actions">
                                    {onThreadReplyLike != null && (
                                      <ThreadLikeButton
                                        count={tr.likeCount ?? 0}
                                        mine={tr.likeMe}
                                        voters={tr.likeVoters}
                                        onToggle={() => onThreadReplyLike(tr.id)}
                                      />
                                    )}
                                    {onThreadReplyIntent != null && (
                                      <button
                                        type="button"
                                        className="act-card__thread-answer"
                                        onClick={() => {
                                          onThreadReplyIntent({
                                            replyToThreadId: tr.id,
                                            authorLabel: displayAuthor,
                                            excerpt: truncateByCodePoints(tr.text, 99),
                                          });
                                          window.setTimeout(() => {
                                            threadInputRef.current?.focus();
                                          }, 80);
                                        }}
                                      >
                                        Ответить
                                      </button>
                                    )}
                                  </div>
                                )}
                              </div>
                            </div>
                          </li>
                        );
                      })}
                    </ul>
                  )}
                </div>
                {threadComposer && (
                    <div className="act-card__thread-compose" ref={threadComposeRef}>
                    {threadComposer.editReplyId != null && threadComposer.editReplyId > 0 && (
                      <div className="act-card__reply-intent act-card__reply-intent--edit">
                        <div className="act-card__reply-intent-row">
                          <span className="act-card__reply-intent-label">Редактирование комментария</span>
                          {threadComposer.onCancelEdit != null && (
                            <button
                              type="button"
                              className="act-card__reply-intent-cancel"
                              aria-label="Отменить редактирование"
                              onClick={() => threadComposer.onCancelEdit?.()}
                            >
                              ✕
                            </button>
                          )}
                        </div>
                      </div>
                    )}
                    {threadReplyIntent != null && threadComposer.editReplyId == null && (
                      <div className="act-card__reply-intent">
                        <div className="act-card__reply-intent-row">
                          <span className="act-card__reply-intent-label">
                            Ответ <strong>{threadReplyIntent.authorLabel}</strong>
                          </span>
                          {onCancelThreadReplyIntent != null && (
                            <button
                              type="button"
                              className="act-card__reply-intent-cancel"
                              aria-label="Отменить ответ"
                              onClick={() => onCancelThreadReplyIntent()}
                            >
                              ✕
                            </button>
                          )}
                        </div>
                        {(threadReplyIntent.excerpt || "").trim() !== "" && (
                          <p className="act-card__reply-intent-snippet muted">{threadReplyIntent.excerpt}</p>
                        )}
                      </div>
                    )}
                    {threadPhotoPreview && threadComposer.editReplyId == null && (
                      <div className="act-card__thread-photo-preview">
                        <img
                          className="act-card__thread-photo-preview-img"
                          src={threadPhotoPreview}
                          alt=""
                        />
                        <button
                          type="button"
                          className="act-card__thread-photo-recrop"
                          disabled={threadComposer.posting}
                          onClick={() => {
                            if (threadPhoto) setPendingThreadCrop(threadPhoto);
                          }}
                        >
                          Обрезать
                        </button>
                        <button
                          type="button"
                          className="act-card__thread-photo-remove"
                          aria-label="Убрать фото"
                          disabled={threadComposer.posting}
                          onClick={() => {
                            setThreadPhoto(null);
                            if (threadPhotoInputRef.current) threadPhotoInputRef.current.value = "";
                          }}
                        >
                          ✕
                        </button>
                      </div>
                    )}
                    <textarea
                      ref={threadInputRef}
                      className="act-card__thread-input"
                      rows={2}
                      placeholder={
                        threadComposer.editReplyId != null
                          ? "Изменить комментарий…"
                          : threadReplyIntent
                            ? `Сообщение для ${threadReplyIntent.authorLabel}…`
                            : "Написать комментарий…"
                      }
                      value={threadComposer.draft}
                      onChange={(e) => threadComposer.onDraftChange(e.target.value)}
                      maxLength={500}
                      onFocus={onThreadComposeFocus}
                      onBlur={onThreadComposeBlur}
                      onPaste={(e) => {
                        // Картинка из буфера — как фото из галереи: сразу в кроп.
                        if (threadComposer.editReplyId != null) return;
                        const pasted = clipboardImageFile(e.clipboardData);
                        if (!pasted) return;
                        e.preventDefault();
                        setPendingThreadCrop(pasted);
                      }}
                      onKeyDown={(e) => {
                        if (e.key !== "Enter" || (!e.ctrlKey && !e.metaKey)) return;
                        e.preventDefault();
                        submitThreadComment();
                      }}
                    />
                    <input
                      ref={threadPhotoInputRef}
                      type="file"
                      accept="image/*"
                      hidden
                      onChange={(e) => {
                        const f = e.target.files?.[0] ?? null;
                        if (f) setPendingThreadCrop(f);
                        e.target.value = "";
                      }}
                    />
                    <div className="act-card__thread-compose-actions">
                      {threadComposer.editReplyId == null && (
                        <button
                          type="button"
                          className="act-card__thread-attach"
                          aria-label="Прикрепить фото из галереи"
                          disabled={threadComposer.posting}
                          onClick={() => threadPhotoInputRef.current?.click()}
                        >
                          📎
                        </button>
                      )}
                      {threadComposer.editReplyId == null && (
                        <CameraButton
                          className="act-card__thread-attach"
                          disabled={threadComposer.posting}
                          onChange={(e) => {
                            const f = e.target.files?.[0] ?? null;
                            if (f) setPendingThreadCrop(f);
                          }}
                        />
                      )}
                      {adminVoiceAvailable && threadComposer.editReplyId == null && (
                        <div className="act-card__thread-voice" role="group" aria-label="От чьего имени комментировать">
                          {(
                            [
                              { v: "self", label: "Я" },
                              { v: "leo", label: "🐆 Лео" },
                              { v: "admin", label: "Админ" },
                            ] as { v: ThreadPostAs; label: string }[]
                          ).map((opt) => (
                            <button
                              key={opt.v}
                              type="button"
                              className={`act-card__thread-voice-btn${
                                threadPostAs === opt.v ? " act-card__thread-voice-btn--active" : ""
                              }`}
                              disabled={threadComposer.posting}
                              aria-pressed={threadPostAs === opt.v}
                              onClick={() => setThreadPostAs(opt.v)}
                            >
                              {opt.label}
                            </button>
                          ))}
                        </div>
                      )}
                      <button
                        type="button"
                        className="act-card__thread-send"
                        disabled={threadComposer.posting}
                        onClick={() => submitThreadComment()}
                      >
                        {threadComposer.posting
                          ? "…"
                          : threadComposer.editReplyId != null
                            ? "Сохранить"
                            : "Отправить"}
                      </button>
                    </div>
                    {pendingThreadCrop &&
                      createPortal(
                        <PhotoCropper
                          file={pendingThreadCrop}
                          onCancel={() => setPendingThreadCrop(null)}
                          onConfirm={(cropped) => {
                            setThreadPhoto(cropped);
                            setPendingThreadCrop(null);
                          }}
                        />,
                        document.body,
                      )}
                  </div>
                )}
              </>
            )}
          </div>
        )}
      {lightboxOpen && (lightboxSrc || trainingPhotoUrl)
        ? createPortal(
            <PhotoLightbox
              src={(lightboxSrc || trainingPhotoUrl) as string}
              onClose={() => setLightboxOpen(false)}
            />,
            document.body,
          )
        : null}
    </article>
  );
}
