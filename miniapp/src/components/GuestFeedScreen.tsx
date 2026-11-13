import { useEffect, useState } from "react";
import { ActivityCard } from "./ActivityCard";
import { dtoToCard, feedItemKey } from "../lib/packFeed";
import { fetchGuestFeed, openGuestJoin, type GuestFeed } from "../lib/guestFeed";
import "./GuestFeedScreen.css";

type Props = {
  initData: string;
  /** Viewer already in pack — open full app. */
  onInPack: () => void;
  /** For tests: prefilled response (null — no server). */
  initialFeed?: GuestFeed | null;
};

/** Guest preview: recent pack workouts and Join for invite links. Read-only cards. */
export function GuestFeedScreen({ initData, onInPack, initialFeed }: Props) {
  const [feed, setFeed] = useState<GuestFeed | null | undefined>(initialFeed);

  useEffect(() => {
    if (initialFeed !== undefined) return;
    let alive = true;
    void fetchGuestFeed(initData).then((f) => {
      if (alive) setFeed(f);
    });
    return () => {
      alive = false;
    };
  }, [initData, initialFeed]);

  useEffect(() => {
    if (feed?.inPack) onInPack();
  }, [feed, onInPack]);

  const loading = feed === undefined;
  const posts = feed?.items ?? [];

  return (
    <div className="guest-feed">
      <header className="guest-feed__header">
        <div className="guest-feed__brand">Fat Leopard</div>
        <p className="guest-feed__eyebrow">Pack feed</p>
        <h1 className="guest-feed__title">This is how the pack trains every day</h1>
        <p className="guest-feed__text">
          Everyone logs workouts and the pack cheers you on. Join to post yours, react, and keep your streak with the
          rest of the pack.
        </p>
      </header>

      <div className="guest-feed__list" aria-busy={loading}>
        {loading ? <p className="guest-feed__empty">Loading feed…</p> : null}
        {!loading && posts.length === 0 ? (
          <p className="guest-feed__empty">No recent workouts to show — join the pack to see more.</p>
        ) : null}
        {posts.map((p) => (
          <ActivityCard key={feedItemKey(p)} {...dtoToCard(p)} />
        ))}
      </div>

      <div className="guest-feed__cta-bar">
        <button type="button" className="guest-feed__cta" onClick={() => openGuestJoin(feed?.joinUrl ?? "")}>
          Join
        </button>
      </div>
    </div>
  );
}
