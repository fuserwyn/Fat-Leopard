import { useEffect } from "react";
import "./ShareCardSheet.css";

type Props = {
  /** Итог от сервера: стрик, кубки, ачивки. */
  message: string;
  /** Открыть карточку тренировки для сторис и чатов. */
  onShare: () => void;
  onClose: () => void;
};

/**
 * Модалка «Тренировка засчитана»: итог начисления и кнопка «Поделиться» —
 * карточка тренировки для сторис Telegram, Instagram, TikTok, ВКонтакте и чатов.
 */
export function WorkoutDoneModal({ message, onShare, onClose }: Props) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    <div className="share-card-overlay workout-done-overlay" onClick={onClose}>
      <div
        className="workout-done"
        role="dialog"
        aria-modal="true"
        aria-label="Тренировка засчитана"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="workout-done__emoji" aria-hidden>
          🎉
        </div>
        <div className="workout-done__title">Тренировка засчитана!</div>
        <div className="workout-done__message">{message}</div>
        <button type="button" className="share-brag-btn" onClick={onShare}>
          Поделиться в сторис
        </button>
        <button type="button" className="share-card-sheet__close" onClick={onClose}>
          Готово
        </button>
      </div>
    </div>
  );
}
