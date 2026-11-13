import { useState } from "react";
import { leoTextNeedsEnglishTranslate, translateLeoTextToEnglish } from "../lib/leoTranslate";
import { useI18n } from "../lib/I18nContext";
import "./LeoTranslateButton.css";

type Props = {
  text: string;
  initData?: string;
  /** Показанный текст (оригинал или перевод). */
  onShowText: (text: string) => void;
  className?: string;
};

export function LeoTranslateButton({ text, initData, onShowText, className }: Props) {
  const { t } = useI18n();
  const original = text.trim();
  const [translated, setTranslated] = useState<string | null>(null);
  const [showingEn, setShowingEn] = useState(false);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState(false);

  if (!leoTextNeedsEnglishTranslate(original)) {
    return null;
  }

  const toggle = async () => {
    setErr(false);
    if (showingEn) {
      setShowingEn(false);
      onShowText(original);
      return;
    }
    if (translated) {
      setShowingEn(true);
      onShowText(translated);
      return;
    }
    if (!initData?.trim()) {
      setErr(true);
      return;
    }
    setBusy(true);
    try {
      const en = await translateLeoTextToEnglish(initData, original);
      setTranslated(en);
      setShowingEn(true);
      onShowText(en);
    } catch {
      setErr(true);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className={`leo-translate${className ? ` ${className}` : ""}`}>
      <button
        type="button"
        className="leo-translate__btn"
        onClick={() => void toggle()}
        disabled={busy}
        aria-pressed={showingEn}
      >
        {busy ? "…" : showingEn ? t("leo.showOriginal") : t("leo.translateEn")}
      </button>
      {err ? <span className="leo-translate__err">{t("leo.translateFail")}</span> : null}
    </div>
  );
}
