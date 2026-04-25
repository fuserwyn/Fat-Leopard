import { useEffect, useState } from "react";

/**
 * Подключает Telegram WebApp: разворот на весь экран, тёмный header под макет.
 * Вне Telegram (браузер) — no-op, имя из query ?name=.
 */
export function useTelegramWebApp() {
  const [name, setName] = useState("Стас");
  const [streak, setStreak] = useState(0);

  const [initData, setInitData] = useState("");
  const [userId, setUserId] = useState(0);

  useEffect(() => {
    const w = window.Telegram?.WebApp;
    if (!w) {
      const q = new URLSearchParams(window.location.search);
      const n = q.get("name");
      if (n) setName(n);
      const s = q.get("streak");
      if (s) setStreak(Number(s) || 0);
      return;
    }
    w.ready();
    w.expand();
    w.setHeaderColor?.("#0d0d12");
    w.setBackgroundColor?.("#0d0d12");
    setInitData(w.initData ?? "");
    const u = w.initDataUnsafe?.user;
    if (u) {
      setName(u.first_name || u.username || "друг");
      setUserId(u.id);
    }
  }, []);

  return {
    name,
    streak,
    setStreak,
    initData,
    userId,
    inTelegram: Boolean(window.Telegram?.WebApp),
    tg: window.Telegram?.WebApp,
  };
}
