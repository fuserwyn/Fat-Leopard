
import { useI18n } from "../lib/I18nContext";
import { pick } from "../lib/i18n";import { useRef, type ChangeEvent, type ReactNode } from "react";

/** Кнопка «Сделать фото»: открывает камеру устройства напрямую (input capture) рядом с
 *  обычным выбором из галереи. Переиспользует тот же обработчик, что и галерейный input —
 *  он читает e.target.files[0], поэтому камере отдельная логика не нужна. Значение input
 *  сбрасывается после выбора, чтобы повторный тот же кадр снова триггерил onChange. */
type CameraButtonProps = {
  onChange: (e: ChangeEvent<HTMLInputElement>) => void;
  disabled?: boolean;
  className?: string;
  ariaLabel?: string;
  title?: string;
  /** Содержимое кнопки; по умолчанию 📷. */
  children?: ReactNode;
};

/** Общий класс: hit-area и читаемость живут здесь, визуал (рамка, фон) — у className снаружи. */
const CAM_BTN_CLASS = "fl-cam-btn";

/**
 * Стили в этом файле, без отдельного CSS: родители задают вид (36–44px),
 * а на таче / узком экране зона нажатия поднимается до 44×44, не ломая десктоп.
 */
const CAM_BTN_CSS = `
.${CAM_BTN_CLASS} {
  box-sizing: border-box;
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 0.35em;
  line-height: 1.2;
  color: inherit;
  -webkit-tap-highlight-color: transparent;
  touch-action: manipulation;
  user-select: none;
  -webkit-user-select: none;
}

.${CAM_BTN_CLASS}:focus-visible {
  outline: 2px solid var(--accent, currentColor);
  outline-offset: 2px;
}

.${CAM_BTN_CLASS}:disabled {
  cursor: not-allowed;
}

/* Телефон и узкое окно: HIG / WCAG 2.5.5 — не меньше 44×44. Широкий fine-pointer не трогаем. */
@media (pointer: coarse), (max-width: 600px) {
  button.${CAM_BTN_CLASS} {
    min-width: 44px !important;
    min-height: 44px !important;
    font-size: max(1em, 16px);
  }
}
`;

export function CameraButton({
  onChange,
  disabled,
  className,
  ariaLabel,
  title,
  children,
}: CameraButtonProps) {
  const { lang } = useI18n();
  ariaLabel = ariaLabel ?? pick(lang, "Сделать фото", "Take photo");
  const inputRef = useRef<HTMLInputElement>(null);
  return (
    <>
      <style>{CAM_BTN_CSS}</style>
      <input
        ref={inputRef}
        type="file"
        accept="image/*"
        capture="environment"
        hidden
        tabIndex={-1}
        aria-hidden
        disabled={disabled}
        onChange={(e) => {
          onChange(e);
          if (inputRef.current) inputRef.current.value = "";
        }}
      />
      <button
        type="button"
        className={className ? `${CAM_BTN_CLASS} ${className}` : CAM_BTN_CLASS}
        disabled={disabled}
        aria-label={ariaLabel}
        title={title ?? ariaLabel}
        onClick={() => inputRef.current?.click()}
      >
        {children ?? "📷"}
      </button>
    </>
  );
}
