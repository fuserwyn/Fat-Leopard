import { useCallback, useEffect, useState } from "react";
import { ActivityCard, type ActivityCardProps } from "./ActivityCard";
import { PackGroupChatPanel } from "./PackGroupChatPanel";
import { dtoToCard, type PackFeedItemDTO } from "../lib/packFeed";
import "./FeedScreen.css";

const apiBase = (import.meta.env.VITE_MINIAPP_API_URL as string | undefined)?.replace(/\/$/, "") ?? "";

type Props = {
  name: string;
  streak: number;
  userId: number;
  initData: string;
  inTelegram: boolean;
  showAlert: (m: string) => void;
};

type Sub = "activity" | "room";

function mockFallback(_name: string, streak: number): ActivityCardProps[] {
  return [
    {
      avatar: "💬",
      name: "Стая",
      streak: Math.max(streak, 0),
      timeAgo: "сейчас",
      emoji: "ℹ️",
      activity: "Нет API",
      details: "VITE_MINIAPP_API_URL",
      comment: "Включи URL бота в билде, чтобы тянуть реальные отчёты из чата стаи.",
    },
  ];
}

export function FeedScreen({ name, streak, userId, initData, inTelegram, showAlert }: Props) {
  const [sub, setSub] = useState<Sub>("activity");
  const subtitle =
    streak === 0
      ? "Две вкладки: чужие отчёты и общий чат с @leo"
      : streak === 1
        ? "Лента отчётов и комната стаи: Лео отвечает только с @leo"
        : "Стая: кто тренировался, и чат с упоминанием бота.";

  const [cards, setCards] = useState<ActivityCardProps[]>([]);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState<string | null>(null);

  const load = useCallback(async () => {
    if (!apiBase || !inTelegram || !initData) {
      setLoading(false);
      setCards(mockFallback(name, streak));
      setErr(null);
      return;
    }
    setErr(null);
    setLoading(true);
    try {
      const res = await fetch(`${apiBase}/api/miniapp/feed`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ init_data: initData }),
      });
      const j = (await res.json().catch(() => ({}))) as { ok?: boolean; items?: PackFeedItemDTO[]; error?: string };
      if (!res.ok) {
        if (res.status === 403) {
          setErr("Нет доступа к ленте стаи: нужна подписка/участие в группе, как в боте.");
          setCards([]);
          return;
        }
        setErr(j.error ?? `Ошибка ${res.status}`);
        setCards([]);
        return;
      }
      const items = j.items ?? [];
      setCards(items.map((it) => dtoToCard(it)));
    } catch (e) {
      setErr(e instanceof Error ? e.message : "Сеть");
      setCards([]);
    } finally {
      setLoading(false);
    }
  }, [inTelegram, initData, name, streak]);

  useEffect(() => {
    void load();
  }, [load]);

  return (
    <div className="feed">
      <header className="feed__header">
        <div>
          <h1 className="feed__greet">Стая, {name}</h1>
          <p className="feed__sub muted">{subtitle}</p>
        </div>
        <div className="feed__streak" aria-label={`Серия ${streak} дней`}>
          <span>🔥</span> {streak}
        </div>
      </header>
      <div className="feed__subtabs" role="tablist" aria-label="Стая">
        <button
          type="button"
          className={`feed__subtab ${sub === "activity" ? "is-active" : ""}`}
          onClick={() => setSub("activity")}
          role="tab"
          aria-selected={sub === "activity"}
        >
          Лента
        </button>
        <button
          type="button"
          className={`feed__subtab ${sub === "room" ? "is-active" : ""}`}
          onClick={() => setSub("room")}
          role="tab"
          aria-selected={sub === "room"}
        >
          Чат
        </button>
      </div>
      {sub === "room" && (
        <PackGroupChatPanel
          initData={initData}
          inTelegram={inTelegram}
          meId={userId}
          showAlert={showAlert}
          onHaptic={() => {
            const w = window.Telegram?.WebApp;
            w?.HapticFeedback?.impactOccurred?.("light");
          }}
        />
      )}
      {sub === "activity" && (
        <>
          <h2 className="section-title">Кто что постит</h2>
          {err && <p className="feed__err">{err}</p>}
          {loading && <p className="feed__load muted">Загрузка…</p>}
          <div className="feed__list">
            {!loading && cards.length === 0 && !err && (
              <p className="feed__empty muted">Пока нет отчётов в базе (или нет MONETIZED_CHAT_ID).</p>
            )}
            {cards.map((c, i) => (
              <ActivityCard key={i} {...c} />
            ))}
          </div>
          {!loading && !err && apiBase && inTelegram && initData && (
            <div className="feed__actions">
              <button type="button" className="feed__btn" onClick={() => void load()}>
                Обновить
              </button>
            </div>
          )}
        </>
      )}
    </div>
  );
}
