import { useEffect, useRef, useState } from "react";
import { prepareWorkoutPhoto } from "../lib/prepareWorkoutPhoto";
import "./PhotoConfirm.css";

type Props = {
  file: File;
  onCancel: () => void;
  onConfirm: (prepared: File) => void;
  /** Открыть инструмент обрезки для текущего файла. */
  onCrop: () => void;
  /** Пользователь выбрал другое фото. */
  onReplace: (newFile: File) => void;
};

function CropIcon() {
  return (
    <svg className="ph-confirm__crop-icon" viewBox="0 0 24 24" aria-hidden focusable="false">
      <path d="M17 15h2V7c0-1.1-.9-2-2-2H9v2h8v8zM7 17V1H5v4H1v2h4v10c0 1.1.9 2 2 2h10v4h2v-6H7z" />
    </svg>
  );
}

export function PhotoConfirm({ file, onCancel, onConfirm, onCrop, onReplace }: Props) {
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
    <div className="ph-confirm" role="dialog" aria-modal="true" aria-label="Подтверждение фото">
      <header className="ph-confirm__head">
        <button type="button" className="ph-confirm__btn ph-confirm__btn--ghost" onClick={onCancel} disabled={busy}>
          Отмена
        </button>
        <span className="ph-confirm__title">Фото тренировки</span>
        <button
          type="button"
          className="ph-confirm__btn ph-confirm__btn--icon"
          onClick={onCrop}
          disabled={busy}
          aria-label="Обрезать фото"
          title="Обрезать фото"
        >
          <CropIcon />
        </button>
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
          {busy ? "…" : "Готово"}
        </button>
        <button
          type="button"
          className="ph-confirm__btn ph-confirm__btn--ghost"
          onClick={() => replaceRef.current?.click()}
          disabled={busy}
        >
          Загрузить другое фото
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
