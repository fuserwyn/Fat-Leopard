import { useCallback, useEffect, useRef, useState } from "react";
import { fetchReferralState } from "../lib/referral";
import {
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
  const [busy, setBusy] = useState<ShareTarget | null>(null);
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

  const share = useCallback(
    async (target: ShareTarget) => {
      if (busy) return;
      setBusy(target);
      try {
        const up = target === "instagram" || target === "tiktok" ? null : await ensureUploaded();
        const caption = shareCardCaption(card, link || up?.link || "");
        if (target === "instagram" || target === "tiktok") {
          const app = target === "instagram" ? "Instagram" : "TikTok";
          const res = image ? await shareImageViaSystem(image, caption) : "unsupported";
          if (res !== "unsupported") return;
          const saved = await ensureUploaded();
          if (!saved) {
            showAlert(UPLOAD_FAILED);
            return;
          }
          saveShareImage(saved.url);
          showAlert(`Сохрани картинку и выложи её в сторис ${app} из галереи.`);
          return;
        }
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
        openShareLink(vkShareUrl(link || up.link, up.url, caption));
      } finally {
        setBusy(null);
      }
    },
    [busy, card, ensureUploaded, image, link, showAlert],
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
        <button type="button" className="share-card-sheet__close" onClick={onClose}>
          Закрыть
        </button>
      </div>
    </div>
  );
}
