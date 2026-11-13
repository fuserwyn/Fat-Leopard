import { LEO_AVATAR_URL } from "../lib/leoAvatar";
import { useI18n } from "../lib/I18nContext";
import "./MiniappRemovedScreen.css";

/** No payment / no pack access — miniapp closed, enter via bot. */
export function MiniappLockedScreen() {
  const { t } = useI18n();
  return (
    <div className="miniapp-removed" role="status" aria-live="polite">
      <div className="miniapp-removed__inner">
        <img className="miniapp-removed__avatar" src={LEO_AVATAR_URL} width={104} height={104} alt={t("gate.leoAlt")} loading="eager" />
        <p className="miniapp-removed__eyebrow">{t("gate.locked.eyebrow")}</p>
        <h1 className="miniapp-removed__title">{t("gate.locked.title")}</h1>
        <p className="miniapp-removed__text">
          {t("gate.locked.text1")} <strong>/start</strong>
          {t("gate.locked.text2")}
        </p>
      </div>
    </div>
  );
}
