import { LEO_AVATAR_URL } from "../lib/leoAvatar";
import "./MiniappRemovedScreen.css";

/** No payment / no pack access — miniapp closed, enter via bot. */
export function MiniappLockedScreen() {
  return (
    <div className="miniapp-removed" role="status" aria-live="polite">
      <div className="miniapp-removed__inner">
        <img className="miniapp-removed__avatar" src={LEO_AVATAR_URL} width={104} height={104} alt="Leo" loading="eager" />
        <p className="miniapp-removed__eyebrow">Join the pack</p>
        <h1 className="miniapp-removed__title">Pay in the bot first</h1>
        <p className="miniapp-removed__text">
          Miniapp access opens after payment. Close the miniapp, message the bot in DM{" "}
          <strong>/start</strong>, and choose a payment option.
        </p>
      </div>
    </div>
  );
}
