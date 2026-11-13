import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { measureBottomNavOffset } from "../lib/bottomNavOffset";
import { useI18n } from "../lib/I18nContext";
import { CameraButton } from "./CameraButton";
import "./BottomNav.css";

type Tab = "chat" | "feed" | "rules" | "profile";

type Props = {
  active: Tab;
  /** Непрочитанные фрагменты из очереди лички Лео (предупреждения, ответы). */
  leoBadgeCount?: number;
  /** Непрочитанные комментарии к твоим отчётам в ленте стаи. */
  feedBadgeCount?: number;
  onChat: () => void;
  onFeed: () => void;
  onAddWorkout: () => void;
  onRules: () => void;
  onProfile: () => void;
  /** Отправка сообщения в общую ленту (компоуз-поле): текст и/или фото. Возвращает true при успехе. */
  onSendMessage?: (text: string, photo?: File | null) => Promise<boolean> | boolean;
  /** Показывать компоуз-поле. На экране тренировки/поддержки скрываем (там своё поле ввода). */
  showCompose?: boolean;
};

export function BottomNav({
  active,
  leoBadgeCount = 0,
  feedBadgeCount = 0,
  onChat,
  onFeed,
  onAddWorkout,
  onRules,
  onProfile,
  onSendMessage,
  showCompose = true,
}: Props) {
  const { t } = useI18n();
  const leoBadge = leoBadgeCount > 0 ? (leoBadgeCount > 9 ? "9+" : String(leoBadgeCount)) : null;
  const feedBadge = feedBadgeCount > 0 ? (feedBadgeCount > 9 ? "9+" : String(feedBadgeCount)) : null;
  const navRef = useRef<HTMLElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const photoInputRef = useRef<HTMLInputElement>(null);
  const [msg, setMsg] = useState("");
  const [photo, setPhoto] = useState<File | null>(null);
  const [photoPreview, setPhotoPreview] = useState<string | null>(null);
  const [sending, setSending] = useState(false);

  // Превью прикреплённого фото (object URL с чисткой при смене/снятии).
  useEffect(() => {
    if (!photo) {
      setPhotoPreview(null);
      return;
    }
    const url = URL.createObjectURL(photo);
    setPhotoPreview(url);
    return () => URL.revokeObjectURL(url);
  }, [photo]);

  // Публикуем высоту таббара в --bottom-nav-h, чтобы оверлеи (например, #training_done) могли её зарезервировать.
  useLayoutEffect(() => {
    const el = navRef.current;
    if (!el) return;
    const root = document.documentElement;
    const write = () => {
      root.style.setProperty("--bottom-nav-h", `${Math.ceil(el.getBoundingClientRect().height)}px`);
      root.style.setProperty("--bottom-nav-stack", `${measureBottomNavOffset(el)}px`);
    };
    write();
    const ro = new ResizeObserver(write);
    ro.observe(el);
    return () => {
      ro.disconnect();
      root.style.removeProperty("--bottom-nav-h");
      root.style.removeProperty("--bottom-nav-stack");
    };
  }, []);

  // Пока сфокусировано компоуз-поле — держим таббар видимым и над клавиатурой
  // (иначе html.app-keyboard-open прячет весь бар вместе с полем ввода).
  const onComposeFocus = () => {
    document.documentElement.classList.add("app-nav-compose-open");
  };
  const onComposeBlur = () => {
    document.documentElement.classList.remove("app-nav-compose-open");
  };

  const submitMessage = async () => {
    const text = msg.trim();
    if ((!text && !photo) || sending || !onSendMessage) return;
    setSending(true);
    try {
      const ok = await onSendMessage(text, photo);
      if (ok) {
        setMsg("");
        setPhoto(null);
        if (photoInputRef.current) photoInputRef.current.value = "";
      }
    } finally {
      setSending(false);
    }
  };

  return (
    <nav ref={navRef} className="bottom-nav" role="navigation" aria-label={t("nav.menu")}>
      {/* Компоуз-поле вынесено отдельной строкой НАД таббаром: текст → сообщение
          в ленту, «+» → форма тренировки. Скрыто на экране тренировки/поддержки. */}
      <div
        className={`bottom-nav__compose-row${showCompose ? "" : " bottom-nav__compose-row--collapsed"}`}
        aria-hidden={!showCompose}
        {...(!showCompose ? { inert: true as const } : {})}
      >
        <div className="bottom-nav__compose-clip">
          <form
            className="bottom-nav__compose"
            onSubmit={(e) => {
              e.preventDefault();
              void submitMessage();
            }}
          >
            <input
              ref={photoInputRef}
              type="file"
              accept="image/*"
              className="bottom-nav__compose-file"
              disabled={sending || !showCompose}
              onChange={(e) => {
                const f = e.target.files?.[0] ?? null;
                if (f) setPhoto(f);
                e.target.value = "";
              }}
            />
            <button
              type="button"
              className={`bottom-nav__compose-attach${photo ? " bottom-nav__compose-attach--on" : ""}`}
              disabled={sending || !showCompose}
              onClick={() => photoInputRef.current?.click()}
              aria-label={photo ? t("nav.replacePhoto") : t("nav.attachPhoto")}
              title={photo ? t("nav.replacePhoto") : t("nav.attachPhoto")}
            >
              📎
            </button>
            <CameraButton
              className="bottom-nav__compose-attach"
              disabled={sending || !showCompose}
              onChange={(e) => {
                const f = e.target.files?.[0] ?? null;
                if (f) setPhoto(f);
              }}
            />
            {photoPreview && (
              <span className="bottom-nav__compose-thumb">
                <img src={photoPreview} alt="" />
                <button
                  type="button"
                  className="bottom-nav__compose-thumb-x"
                  onClick={() => setPhoto(null)}
                  aria-label={t("nav.removePhoto")}
                >
                  ✕
                </button>
              </span>
            )}
            <input
              ref={inputRef}
              className="bottom-nav__compose-input"
              value={msg}
              onChange={(e) => setMsg(e.target.value)}
              onFocus={onComposeFocus}
              onBlur={onComposeBlur}
              placeholder={t("nav.composePlaceholder")}
              aria-label={t("nav.composeAria")}
              maxLength={4000}
              autoComplete="off"
              enterKeyHint="send"
              disabled={sending || !showCompose}
            />
            <button
              type="submit"
              className="bottom-nav__compose-send"
              disabled={sending || !showCompose || (!msg.trim() && !photo)}
              aria-label={t("nav.sendFeed")}
              title={t("nav.sendFeed")}
            >
              {sending ? "…" : "➤"}
            </button>
          </form>
        </div>
      </div>
      <div className="bottom-nav__tabs">
      <button
        type="button"
        className={`bottom-nav__item ${active === "feed" ? "is-active" : ""}`}
        onClick={onFeed}
        aria-current={active === "feed" ? "page" : undefined}
        aria-label={feedBadge ? t("nav.packUnread", { n: feedBadgeCount }) : t("nav.pack")}
      >
        <span className="bottom-nav__icon-wrap" aria-hidden>
          <span className="bottom-nav__icon">🐆</span>
          {feedBadge && (
            <span className="bottom-nav__badge bottom-nav__badge--feed" title={t("nav.newFeedReplies")}>
              {feedBadge}
            </span>
          )}
        </span>
        <span className="bottom-nav__label">{t("nav.pack")}</span>
      </button>
      <button
        type="button"
        className={`bottom-nav__item ${active === "chat" ? "is-active" : ""}`}
        onClick={onChat}
        aria-current={active === "chat" ? "page" : undefined}
        aria-label={leoBadge ? t("nav.leoUnread", { n: leoBadgeCount }) : t("nav.leo")}
      >
        <span className="bottom-nav__icon-wrap" aria-hidden>
          <span className="bottom-nav__icon">💬</span>
          {leoBadge && (
            <span className="bottom-nav__badge" title={t("nav.newFromLeo")}>
              {leoBadge}
            </span>
          )}
        </span>
        <span className="bottom-nav__label">{t("nav.leo")}</span>
      </button>

      {/* «+» всегда по центру таб-бара — добавить тренировку можно отовсюду. */}
      <button
        type="button"
        className="bottom-nav__add"
        onClick={onAddWorkout}
        aria-label={t("nav.addWorkout")}
        title={t("nav.addWorkout")}
      >
        <svg className="bottom-nav__add-plus" viewBox="0 0 24 24" aria-hidden focusable="false">
          <path d="M12 5v14M5 12h14" />
        </svg>
      </button>

      <button
        type="button"
        className={`bottom-nav__item ${active === "rules" ? "is-active" : ""}`}
        onClick={onRules}
        aria-current={active === "rules" ? "page" : undefined}
      >
        <span className="bottom-nav__icon" aria-hidden>
          📔
        </span>
        <span className="bottom-nav__label">{t("nav.rules")}</span>
      </button>
      <button
        type="button"
        className={`bottom-nav__item ${active === "profile" ? "is-active" : ""}`}
        onClick={onProfile}
        aria-current={active === "profile" ? "page" : undefined}
      >
        <span className="bottom-nav__icon" aria-hidden>
          👤
        </span>
        <span className="bottom-nav__label">{t("nav.profile")}</span>
      </button>
      </div>
    </nav>
  );
}
