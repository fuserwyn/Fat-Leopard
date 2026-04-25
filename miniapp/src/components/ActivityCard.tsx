import "./ActivityCard.css";

export type ActivityCardProps = {
  avatar: string;
  name: string;
  streak: number;
  timeAgo: string;
  emoji: string;
  activity: string;
  details: string;
  comment?: string;
  aiText?: string;
  reactions: { emoji: string; count?: number }[];
};

export function ActivityCard({
  avatar,
  name,
  streak,
  timeAgo,
  emoji,
  activity,
  details,
  comment,
  aiText,
  reactions,
}: ActivityCardProps) {
  return (
    <article className="act-card">
      <header className="act-card__head">
        <div className="act-card__avatar" aria-hidden>
          {avatar}
        </div>
        <div className="act-card__meta">
          <div className="act-card__row">
            <span className="act-card__name">{name}</span>
            <span className="pill" aria-label={`Серия ${streak} дней`}>
              <span>🔥</span> {streak}
            </span>
          </div>
          <p className="act-card__time">{timeAgo}</p>
        </div>
      </header>
      <div className="act-card__body">
        <p className="act-card__type">
          <span className="act-card__type-ico">{emoji}</span> {activity}
        </p>
        <p className="act-card__details muted">{details}</p>
        {comment && <p className="act-card__comment">{comment}</p>}
        {aiText && (
          <div className="act-card__ai">
            <div className="act-card__ai-label">AI</div>
            <p className="act-card__ai-text">{aiText}</p>
          </div>
        )}
        <div className="act-card__react" role="group" aria-label="Реакции">
          {reactions.map((r) => (
            <button type="button" className="act-card__react-btn" key={r.emoji + (r.count ?? 0)}>
              {r.emoji}
              {r.count != null && r.count > 0 && <span className="act-card__react-cnt">{r.count}</span>}
            </button>
          ))}
          <span className="act-card__sync" aria-hidden title="Поделиться">
            🔄
          </span>
        </div>
      </div>
    </article>
  );
}
