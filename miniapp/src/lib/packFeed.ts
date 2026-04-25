import { timeAgoFromISO } from "./timeAgo";
import type { ActivityCardProps } from "../components/ActivityCard";

export type PackFeedItemDTO = {
  id: number;
  user_id: number;
  username: string;
  type: string;
  text: string;
  created_at: string;
  streak_days: number;
  is_you: boolean;
};

function typeMeta(t: string): { emoji: string; activity: string; details: string } {
  switch (t) {
    case "training_done":
      return { emoji: "💪", activity: "Тренировка", details: "Отчёт #training_done" };
    case "sick_leave":
      return { emoji: "🏥", activity: "Больничный", details: "Заявка #sick_leave" };
    case "healthy":
      return { emoji: "💚", activity: "Выздоровление", details: "#healthy" };
    default:
      return { emoji: "📝", activity: t, details: "Сообщение" };
  }
}

function avatarFor(name: string) {
  const t = (name || "?").trim();
  const first = t[0] ?? "🐆";
  if (first && !/[a-zA-Zа-яА-Я0-9@#]/.test(first)) {
    return first; // already emoji
  }
  return "🐾";
}

export function dtoToCard(d: PackFeedItemDTO): ActivityCardProps {
  const m = typeMeta(d.type);
  const comment = d.text.length > 280 ? d.text.slice(0, 277) + "…" : d.text;
  return {
    avatar: avatarFor(d.username),
    name: d.is_you ? "Ты" : d.username || `Участник ${d.user_id}`,
    streak: d.streak_days,
    timeAgo: timeAgoFromISO(d.created_at),
    emoji: m.emoji,
    activity: m.activity,
    details: m.details,
    comment,
  };
}
