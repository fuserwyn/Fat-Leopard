import { useEffect, useState } from "react";
import { fetchReferralState, friendsWord, referralProgressLine, referralShareUrl, type ReferralState } from "../lib/referral";
import "./InviteFriendsSection.css";

type Props = {
  initData: string;
  /** Для тестов: готовое состояние вместо запроса. */
  initialState?: ReferralState | null;
};

/** Блок профиля «Позвать в стаю»: личная ссылка, счётчик пришедших и прогресс к попытке спасти стрик. */
export function InviteFriendsSection({ initData, initialState }: Props) {
  const [state, setState] = useState<ReferralState | null>(initialState ?? null);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (initialState !== undefined) return;
    let alive = true;
    const load = () => {
      void fetchReferralState(initData).then((s) => {
        // Сбой сети при обновлении не прячет уже показанный блок.
        if (alive) setState((prev) => s ?? prev);
      });
    };
    load();
    // Друг переходит по ссылке, пока мини-апп свёрнут: при возврате обновляем счётчик.
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
        Позвать в стаю
      </h2>
      <p className="profile__hint muted">
        Каждые {state.rewardEvery} {friendsWord(state.rewardEvery)}, которые придут по твоей ссылке и запишут первую
        тренировку, — +1 попытка спасти стрик.
      </p>
      <div className="profile__invite-stats">
        <div className="profile__invite-stat">
          <b>{state.joined}</b>
          <small>пришли по ссылке</small>
        </div>
        <div className="profile__invite-stat">
          <b>{state.qualified}</b>
          <small>записали тренировку</small>
        </div>
        <div className="profile__invite-stat">
          <b>{state.rewards}</b>
          <small>попыток получено</small>
        </div>
      </div>
      <p className="profile__invite-progress muted">{referralProgressLine(state)}</p>
      <button type="button" className="profile__save profile__invite-btn" onClick={share}>
        Позвать в стаю
      </button>
      <button type="button" className="profile__invite-link" onClick={() => void copy()} title="Скопировать ссылку">
        <span>{state.link}</span>
        <small>{copied ? "скопировано" : "копировать"}</small>
      </button>
    </section>
  );
}
