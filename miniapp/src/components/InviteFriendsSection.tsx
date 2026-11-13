import { useEffect, useState } from "react";
import { fetchReferralState, friendsWord, referralProgressLine, referralShareUrl, type ReferralState } from "../lib/referral";
import { useI18n } from "../lib/I18nContext";
import "./InviteFriendsSection.css";

type Props = {
  initData: string;
  /** For tests: prefilled state instead of fetch. */
  initialState?: ReferralState | null;
};

/** Profile block: personal invite link, counts, streak-save progress. */
export function InviteFriendsSection({ initData, initialState }: Props) {
  const { t, lang } = useI18n();
  const [state, setState] = useState<ReferralState | null>(initialState ?? null);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (initialState !== undefined) return;
    let alive = true;
    const load = () => {
      void fetchReferralState(initData).then((s) => {
        if (alive) setState((prev) => s ?? prev);
      });
    };
    load();
    const onVisible = () => {
      if (document.visibilityState === "visible") load();
    };
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      alive = false;
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [initData, initialState]);

  if (!state) return null;

  const copy = async () => {
    try {
      await navigator.clipboard?.writeText(state.link);
      setCopied(true);
    } catch {
      setCopied(false);
    }
  };

  const share = () => {
    const url = referralShareUrl(state.link, lang);
    const wa = window.Telegram?.WebApp;
    if (wa?.openTelegramLink) {
      wa.openTelegramLink(url);
      return;
    }
    if (wa?.openLink) {
      wa.openLink(url);
      return;
    }
    void copy();
  };

  return (
    <section className="profile__invite" aria-labelledby="profile-invite-title">
      <h2 id="profile-invite-title" className="section-title">
        {t("invite.title")}
      </h2>
      <p className="profile__hint muted">
        {t("invite.hint", { n: state.rewardEvery, friends: friendsWord(state.rewardEvery, lang) })}
      </p>
      <div className="profile__invite-stats">
        <div className="profile__invite-stat">
          <b>{state.joined}</b>
          <small>{t("invite.joined")}</small>
        </div>
        <div className="profile__invite-stat">
          <b>{state.qualified}</b>
          <small>{t("invite.qualified")}</small>
        </div>
        <div className="profile__invite-stat">
          <b>{state.rewards}</b>
          <small>{t("invite.rewards")}</small>
        </div>
      </div>
      <p className="profile__invite-progress muted">{referralProgressLine(state, lang)}</p>
      <button type="button" className="profile__save profile__invite-btn" onClick={share}>
        {t("invite.cta")}
      </button>
      <button type="button" className="profile__invite-link" onClick={() => void copy()} title={t("invite.copyTitle")}>
        <span>{state.link}</span>
        <small>{copied ? t("invite.copied") : t("invite.copy")}</small>
      </button>
    </section>
  );
}
