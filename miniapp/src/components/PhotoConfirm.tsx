import { useEffect, useRef, useState } from "react";
import { prepareWorkoutPhoto } from "../lib/prepareWorkoutPhoto";
import "./PhotoConfirm.css";
import { pick } from "../lib/i18n";
import { useI18n } from "../lib/I18nContext";

type Props = {
  file: File;
  onCancel: () => void;
  onConfirm: (prepared: File) => void;
  /** Открыть инструмент обрезки для текущего файла. */
  onCrop: () => void;
  /** Пользователь выбрал другое фото. */
  onReplace: (newFile: File) => void;
};

export function PhotoConfirm({ file, onCancel, onConfirm, onCrop, onReplace }: Props) {
  const { lang } = useI18n();
  const [imgUrl, setImgUrl] = useState("");
  const [busy, setBusy] = useState(false);
  const replaceRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const url = URL.createObjectURL(file);
    setImgUrl(url);
    return () => URL.revokeObjectURL(url);
  }, [file]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onCancel();
    };
    document.addEventListener("keydown", onKey);
    const prev = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.removeEventListener("keydown", onKey);
      document.body.style.overflow = prev;
    };
  }, [onCancel]);

  const confirm = async () => {
    if (busy) return;
    setBusy(true);
    try {
      const prepared = await prepareWorkoutPhoto(file);
      if (!prepared) {
        onCancel();
        return;
      }
      onConfirm(prepared);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="ph-confirm" role="dialog" aria-modal="true" aria-label={pick(lang, "Фото тренировки", "Workout photo")}>
      <header className="ph-confirm__head">
        <button type="button" className="ph-confirm__btn ph-confirm__btn--ghost" onClick={onCancel} disabled={busy}>
          {pick(lang, "Отмена", "Cancel")}
        </button>
        <span className="ph-confirm__title">{pick(lang, "Фото тренировки", "Workout photo")}</span>
        <span className="ph-confirm__head-spacer" aria-hidden="true" />
      </header>
      <div className="ph-confirm__stage">
        {imgUrl ? (
          <img className="ph-confirm__img" src={imgUrl} alt="" draggable={false} />
        ) : null}
      </div>
      <div className="ph-confirm__actions">
        <button
          type="button"
          className="ph-confirm__btn ph-confirm__btn--primary"
          onClick={() => void confirm()}
          disabled={busy}
        >
          {busy ? "…" : pick(lang, "Готово", "Done")}
        </button>
        <button type="button" className="ph-confirm__btn" onClick={onCrop} disabled={busy}>
          {pick(lang, "Обрезать фото", "Crop photo")}
        </button>
        <button
          type="button"
          className="ph-confirm__btn ph-confirm__btn--ghost"
          onClick={() => replaceRef.current?.click()}
          disabled={busy}
        >
          {pick(lang, "Загрузить другое фото", "Choose another photo")}
        </button>
      </div>
      <input
        ref={replaceRef}
        type="file"
        accept="image/*"
        hidden
        tabIndex={-1}
        aria-hidden
        onChange={(e) => {
          const f = e.target.files?.[0] ?? null;
          e.target.value = "";
          if (f) onReplace(f);
        }}
      />
    </div>
  );
}
