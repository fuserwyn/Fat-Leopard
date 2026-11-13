import { LEO_AVATAR_URL } from "../lib/leoAvatar";
import { useI18n } from "../lib/I18nContext";
import "./MiniappRemovedScreen.css";

export function MiniappRemovedScreen() {
  const { t } = useI18n();
  return (
    <div className="miniapp-removed" role="status" aria-live="polite">
      <div className="miniapp-removed__inner">
        <img className="miniapp-removed__avatar" src={LEO_AVATAR_URL} width={104} height={104} alt={t("gate.leoAlt")} loading="eager" />
        <p className="miniapp-removed__eyebrow">{t("gate.removed.eyebrow")}</p>
        <h1 className="miniapp-removed__title">{t("gate.removed.title")}</h1>
        <p className="miniapp-removed__text">
          {t("gate.removed.text")}
        </p>
        <button
          type="button"
          className="miniapp-removed__cta"
          onClick={() => window.Telegram?.WebApp?.close()}
        >
          {t("gate.removed.cta")}
        </button>
      </div>
    </div>
  );
}
