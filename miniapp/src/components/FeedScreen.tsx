import { ActivityCard, type ActivityCardProps } from "./ActivityCard";
import "./FeedScreen.css";

const MOCK: ActivityCardProps[] = [
  {
    avatar: "🐼",
    name: "Маша",
    streak: 29,
    timeAgo: "1 ч назад",
    emoji: "🏃",
    activity: "Бег",
    details: "42 мин · Тяжело (4/5)",
    comment: "Набережная, ветер в лицо",
    aiText: "29 дней подряд — личный рекорд. Восстанавливайся.",
    reactions: [
      { emoji: "🔥", count: 3 },
      { emoji: "😅" },
      { emoji: "💀" },
      { emoji: "👑", count: 1 },
    ],
  },
  {
    avatar: "🦊",
    name: "Ты",
    streak: 1,
    timeAgo: "только что",
    emoji: "🏃",
    activity: "Бег",
    details: "60 мин · Рабочий темп (3/5)",
    comment: "Старт недели",
    aiText: "Стабильный ритм — идеальная основа. Держи курс.",
    reactions: [
      { emoji: "🔥", count: 2 },
      { emoji: "😅" },
      { emoji: "👑", count: 1 },
    ],
  },
];

type Props = { name: string; streak: number };

export function FeedScreen({ name, streak }: Props) {
  const subtitle =
    streak === 0
      ? "Давай начнём стрик сегодня"
      : streak === 1
        ? "1 день подряд — продолжаем"
        : `${streak} дн. подряд — продолжаем`;

  return (
    <div className="feed">
      <header className="feed__header">
        <div>
          <h1 className="feed__greet">Привет, {name}</h1>
          <p className="feed__sub muted">{subtitle}</p>
        </div>
        <div className="feed__streak" aria-label={`Серия ${streak} дней`}>
          <span>🔥</span> {streak}
        </div>
      </header>
      <h2 className="section-title">Лента</h2>
      <div className="feed__list">
        {MOCK.map((c, i) => (
          <ActivityCard key={i} {...c} />
        ))}
      </div>
    </div>
  );
}
