import { useCallback, useEffect, useRef, useState } from "react";
import { fetchReferralState } from "../lib/referral";
import {
  openShareApp,
  openShareLink,
  renderShareCard,
  saveShareImage,
  shareCardCaption,
  shareImageViaSystem,
  shareToTelegramStory,
  telegramChatShareUrl,
  uploadShareCard,
  vkShareUrl,
  type ShareCard,
  type UploadedShareCard,
} from "../lib/shareCard";
import { ShareBrandIcon } from "./ShareBrandIcon";
import "./ShareCardSheet.css";

type Props = {
  card: ShareCard;
  initData: string;
  showAlert: (message: string) => void;
  onClose: () => void;
};

export type ShareTarget = "tg-story" | "tg-chat" | "vk" | "instagram" | "tiktok";

const TARGETS: { id: ShareTarget; label: string }[] = [
  { id: "tg-story", label: "Сторис Telegram" },
  { id: "tg-chat", label: "В чат Telegram" },
  { id: "instagram", label: "Instagram" },
  { id: "tiktok", label: "TikTok" },
  { id: "vk", label: "ВКонтакте" },
];

const UPLOAD_FAILED = "Не получилось подготовить картинку. Проверь интернет и попробуй ещё раз.";

/**
 * Шторка «Поделиться»: превью карточки (ачивка, уровень, тренировка) и кнопки —
 * сторис и чат Telegram, Instagram, TikTok, ВКонтакте. Карточка рисуется сразу при
 * открытии, на сервер грузится только когда выбран способ, которому нужна ссылка.
 */
export function ShareCardSheet({ card, initData, showAlert, onClose }: Props) {
  const [link, setLink] = useState("");
  const [image, setImage] = useState<Blob | null>(null);
  const [preview, setPreview] = useState("");
  const [ready, setReady] = useState(false);
  const [busy, setBusy] = useState<ShareTarget | "save" | null>(null);
  // Загрузку начинаем сразу после отрисовки: к нажатию кнопки ссылка на картинку
  // обычно уже готова, и Telegram открывает сторис/чат без задержки.
  const upload = useRef<Promise<UploadedShareCard | null> | null>(null);

  useEffect(() => {
    let alive = true;
    let objectUrl = "";
    void (async () => {
      const ref = await fetchReferralState(initData);
      const l = ref?.link ?? "";
      const blob = await renderShareCard(card, l);
      if (!alive) return;
      setLink(l);
      setImage(blob);
      upload.current = blob ? uploadShareCard(initData, blob) : null;
      if (blob && typeof URL.createObjectURL === "function") {
        objectUrl = URL.createObjectURL(blob);
        setPreview(objectUrl);
      }
      setReady(true);
    })();
    return () => {
      alive = false;
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [card, initData]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  const ensureUploaded = useCallback(async (): Promise<UploadedShareCard | null> => {
    if (!image) return null;
    let up = upload.current ? await upload.current : null;
    if (!up) {
      // Первая попытка не удалась (сеть) — пробуем ещё раз по нажатию.
      upload.current = uploadShareCard(initData, image);
      up = await upload.current;
    }
    if (up && !link && up.link) setLink(up.link);
    return up;
  }, [image, initData, link]);

  /**
   * Отдельная кнопка «Сохранить картинку»: в Instagram и TikTok картинку выбирают
   * из галереи, поэтому её можно сохранить заранее, не задерживая переход в приложение.
   */
  const save = useCallback(async () => {
    if (busy) return;
    setBusy("save");
    try {
      const up = await ensureUploaded();
      if (up) {
        if (!saveShareImage(up.url)) showAlert("Картинка открыта — сохрани её долгим нажатием.");
        return;
      }
      // Без ссылки на картинку сохранить её через Telegram нечем — системное «Поделиться».
      const res = image ? await shareImageViaSystem(image, shareCardCaption(card, link)) : "unsupported";
      if (res === "unsupported") showAlert(UPLOAD_FAILED);
    } finally {
      setBusy(null);
    }
  }, [busy, card, ensureUploaded, image, link, showAlert]);

  const share = useCallback(
    async (target: ShareTarget) => {
      if (busy) return;
      if (target === "instagram" || target === "tiktok") {
        // Сразу в приложение (Instagram — на камеру сторис), прямо по нажатию:
        // без ожидания загрузки и без вопроса Telegram «скачать файл?».
        openShareApp(target);
        return;
      }
      setBusy(target);
      try {
        const up = await ensureUploaded();
        const caption = shareCardCaption(card, link || up?.link || "");
        if (!up) {
          // Без картинки в чат всё равно можно позвать по ссылке на бота.
          if (target === "tg-chat" && link) {
            openShareLink(telegramChatShareUrl(link, caption));
            return;
          }
          showAlert(UPLOAD_FAILED);
          return;
        }
        if (target === "tg-story") {
          if (!shareToTelegramStory(up.url, caption)) {
            saveShareImage(up.url);
            showAlert("Сторис из мини-аппа доступны в свежей версии Telegram на телефоне. Сохрани картинку и выложи её вручную.");
          }
          return;
        }
        if (target === "tg-chat") {
          openShareLink(telegramChatShareUrl(up.url, caption));
          return;
        }
        openShareApp("vk", vkShareUrl(link || up.link, up.url, caption));
      } finally {
        setBusy(null);
      }
    },
    [busy, card, ensureUploaded, link, showAlert],
  );

  return (
    <div className="share-card-overlay" onClick={onClose}>
      <div
        className="share-card-sheet"
        role="dialog"
        aria-modal="true"
        aria-label="Поделиться"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="share-card-sheet__title">Поделиться</div>
        <div className="share-card-sheet__preview">
          {preview ? (
            <img src={preview} alt={`${card.headline}: ${card.big} ${card.bigCaption}`} />
          ) : (
            <div className="share-card-sheet__fallback" aria-busy={!ready}>
              <span className="share-card-sheet__fallback-emoji">{card.emoji}</span>
              <span className="share-card-sheet__fallback-headline">{card.headline}</span>
              <span className="share-card-sheet__fallback-big">{card.big}</span>
              <span>{card.bigCaption}</span>
              {card.badge ? <span className="share-card-sheet__fallback-badge">{card.badge}</span> : null}
              <span className="share-card-sheet__fallback-name">{card.name}</span>
            </div>
          )}
        </div>
        <div className="share-card-sheet__targets">
          {TARGETS.map((t) => (
            <button
              key={t.id}
              type="button"
              className={`share-card-sheet__target${busy === t.id ? " is-busy" : ""}`}
              disabled={!ready || busy != null}
              aria-busy={busy === t.id}
              onClick={() => void share(t.id)}
            >
              <ShareBrandIcon target={t.id} />
              <span>{t.label}</span>
            </button>
          ))}
        </div>
        <button
          type="button"
          className={`share-card-sheet__save${busy === "save" ? " is-busy" : ""}`}
          disabled={!ready || busy != null}
          aria-busy={busy === "save"}
          onClick={() => void save()}
        >
          Сохранить картинку
        </button>
        <div className="share-card-sheet__hint">Для Instagram и TikTok сохрани картинку и выбери её из галереи в сторис.</div>
        <button type="button" className="share-card-sheet__close" onClick={onClose}>
          Закрыть
        </button>
      </div>
    </div>
  );
}
