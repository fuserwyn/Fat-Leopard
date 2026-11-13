import { useEffect } from "react";
import { useI18n } from "../lib/I18nContext";
import "./ShareCardSheet.css";

type Props = {
  /** Server summary: streak, cups, achievements. */
  message: string;
  /** Open workout share card for stories and chats. */
  onShare: () => void;
  onClose: () => void;
};

/**
 * “Workout logged” modal: accrual summary and Share — workout card for Telegram,
 * Instagram, TikTok, VK stories and chats.
 */
export function WorkoutDoneModal({ message, onShare, onClose }: Props) {
  const { t } = useI18n();
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
        aria-label={t("workout.doneAria")}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="workout-done__emoji" aria-hidden>
          🎉
        </div>
        <div className="workout-done__title">{t("workout.doneTitle")}</div>
        <div className="workout-done__message">{message}</div>
        <button type="button" className="share-brag-btn" onClick={onShare}>
          {t("workout.shareStory")}
        </button>
        <button type="button" className="share-card-sheet__close" onClick={onClose}>
          {t("workout.doneClose")}
        </button>
      </div>
    </div>
  );
}
