import { LEO_AVATAR_URL } from "../lib/leoAvatar";
import "./MiniappRemovedScreen.css";

export function MiniappRemovedScreen() {
  return (
    <div className="miniapp-removed" role="status" aria-live="polite">
      <div className="miniapp-removed__inner">
        <img className="miniapp-removed__avatar" src={LEO_AVATAR_URL} width={104} height={104} alt="Leo" loading="eager" />
        <p className="miniapp-removed__eyebrow">Leo already messaged you in the bot.</p>
        <h1 className="miniapp-removed__title">Glad you stayed on the couch.</h1>
        <p className="miniapp-removed__text">
          You were removed from the pack for inactivity. New members join free, but coming back is paid — payment
          buttons are waiting in your chat with the bot.
        </p>
        <button
          type="button"
          className="miniapp-removed__cta"
          onClick={() => window.Telegram?.WebApp?.close()}
        >
          Back to Leo in chat
        </button>
      </div>
    </div>
  );
}
