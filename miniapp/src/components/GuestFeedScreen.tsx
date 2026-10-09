import { useEffect, useState } from "react";
import { ActivityCard } from "./ActivityCard";
import { dtoToCard, feedItemKey } from "../lib/packFeed";
import { fetchGuestFeed, openGuestJoin, type GuestFeed } from "../lib/guestFeed";
import "./GuestFeedScreen.css";

type Props = {
  initData: string;
  /** Сервер ответил, что смотрящий уже в стае: открыть обычное приложение. */
  onInPack: () => void;
  /** Для тестов: готовый ответ вместо запроса (null — сервер не ответил). */
  initialFeed?: GuestFeed | null;
};

/**
 * Гостевой просмотр: несколько свежих тренировок стаи и кнопка «Вступить» для
 * того, кто открыл мини-апп по чужой ссылке, ещё не вступив. Карточки только
 * для чтения: реакции, комментарии и всё остальное — после вступления.
 */
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
        <p className="guest-feed__eyebrow">Лента стаи</p>
        <h1 className="guest-feed__title">Так стая тренируется каждый день</h1>
        <p className="guest-feed__text">
          Каждый отмечает тренировку, стая поддерживает. Вступи, чтобы публиковать свои, ставить реакции и
          держать стрик вместе со всеми.
        </p>
      </header>

      <div className="guest-feed__list" aria-busy={loading}>
        {loading ? <p className="guest-feed__empty">Загружаем ленту…</p> : null}
        {!loading && posts.length === 0 ? (
          <p className="guest-feed__empty">Свежих тренировок пока не видно — загляни в стаю изнутри.</p>
        ) : null}
        {posts.map((p) => (
          <ActivityCard key={feedItemKey(p)} {...dtoToCard(p)} />
        ))}
      </div>

      <div className="guest-feed__cta-bar">
        <button type="button" className="guest-feed__cta" onClick={() => openGuestJoin(feed?.joinUrl ?? "")}>
          Вступить
        </button>
      </div>
    </div>
  );
}
