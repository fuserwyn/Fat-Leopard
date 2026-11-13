import { useEffect, useState } from "react";
import { fetchReferralState, friendsWord, referralProgressLine, referralShareUrl, type ReferralState } from "../lib/referral";
import "./InviteFriendsSection.css";

type Props = {
  initData: string;
  /** For tests: prefilled state instead of fetch. */
  initialState?: ReferralState | null;
};

/** Profile block: personal invite link, counts, streak-save progress. */
export function InviteFriendsSection({ initData, initialState }: Props) {
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
    const url = referralShareUrl(state.link);
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
        Invite to the pack
      </h2>
      <p className="profile__hint muted">
        For every {state.rewardEvery} {friendsWord(state.rewardEvery)} who join via your link and log their first
        workout, you get +1 streak save.
      </p>
      <div className="profile__invite-stats">
        <div className="profile__invite-stat">
          <b>{state.joined}</b>
          <small>joined via link</small>
        </div>
        <div className="profile__invite-stat">
          <b>{state.qualified}</b>
          <small>logged a workout</small>
        </div>
        <div className="profile__invite-stat">
          <b>{state.rewards}</b>
          <small>saves earned</small>
        </div>
      </div>
      <p className="profile__invite-progress muted">{referralProgressLine(state)}</p>
      <button type="button" className="profile__save profile__invite-btn" onClick={share}>
        Invite to the pack
      </button>
      <button type="button" className="profile__invite-link" onClick={() => void copy()} title="Copy link">
        <span>{state.link}</span>
        <small>{copied ? "copied" : "copy"}</small>
      </button>
    </section>
  );
}
