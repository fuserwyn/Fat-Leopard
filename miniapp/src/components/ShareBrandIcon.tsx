import type { ShareTarget } from "./ShareCardSheet";

/**
 * Значки соцсетей в виде иконок приложений — скруглённый квадрат фирменного
 * цвета со знаком, как на рабочем столе телефона. Нарисованы здесь же в SVG:
 * без картинок с чужих серверов и без шрифтов, которых может не быть в WebView.
 */
export function ShareBrandIcon({ target }: { target: ShareTarget }) {
  const id = `share-ico-${target}`;
  return (
    <svg className="share-brand-icon" viewBox="0 0 48 48" width="52" height="52" aria-hidden focusable="false">
      {target === "tg-story" || target === "tg-chat" ? (
        <>
          <defs>
            <linearGradient id={id} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0" stopColor="#3fb8ee" />
              <stop offset="1" stopColor="#1c93d6" />
            </linearGradient>
          </defs>
          <rect width="48" height="48" rx="11" fill={`url(#${id})`} />
          {target === "tg-story" ? (
            // Кольцо сторис вокруг самолётика.
            <circle cx="24" cy="24" r="17.5" fill="none" stroke="#fff" strokeWidth="2" strokeDasharray="7 3.2" strokeLinecap="round" opacity="0.95" />
          ) : null}
          <path
            transform={target === "tg-story" ? "translate(7.2 7.6) scale(1.4)" : "translate(2.4 3) scale(1.8)"}
            fill="#fff"
            d="M4.6 11.6 18.4 6.3c.6-.2 1.2.2 1 .9l-2.3 11c-.2.8-.7 1-1.3.6l-3.5-2.6-1.7 1.6c-.2.2-.4.3-.7.3l.3-3.6 6.5-5.9c.3-.3-.1-.4-.4-.2l-8 5-3.5-1.1c-.8-.2-.8-.8.2-1.2z"
          />
        </>
      ) : null}

      {target === "instagram" ? (
        <>
          <defs>
            <radialGradient id={id} cx="0.3" cy="1.07" r="1.25">
              <stop offset="0" stopColor="#fed576" />
              <stop offset="0.26" stopColor="#f47133" />
              <stop offset="0.6" stopColor="#bc3081" />
              <stop offset="1" stopColor="#4c63d2" />
            </radialGradient>
          </defs>
          <rect width="48" height="48" rx="11" fill={`url(#${id})`} />
          <rect x="11" y="11" width="26" height="26" rx="7.5" fill="none" stroke="#fff" strokeWidth="2.6" />
          <circle cx="24" cy="24" r="6.2" fill="none" stroke="#fff" strokeWidth="2.6" />
          <circle cx="31.4" cy="16.6" r="1.7" fill="#fff" />
        </>
      ) : null}

      {target === "tiktok" ? (
        <>
          <rect width="48" height="48" rx="11" fill="#010101" />
          {/* Нота в три слоя: бирюзовый и красный со сдвигом, белый поверх. */}
          {[
            ["#25f4ee", "translate(9.4 7.6)"],
            ["#fe2c55", "translate(11.6 9.8)"],
            ["#fff", "translate(10.5 8.7)"],
          ].map(([fill, shift]) => (
            <path
              key={fill}
              transform={`${shift} scale(1.25)`}
              fill={fill}
              d="M12.6 2h3c.2 2.2 1.5 3.7 3.7 3.9v3.1c-1.4 0-2.6-.5-3.7-1.2v6.6a5.6 5.6 0 1 1-5.6-5.6c.3 0 .6 0 .9.1v3.2a2.5 2.5 0 1 0 1.7 2.4V2z"
            />
          ))}
        </>
      ) : null}

      {target === "vk" ? (
        <>
          <rect width="48" height="48" rx="11" fill="#0077ff" />
          <path
            fill="#fff"
            d="M25.5 33.2c-9.1 0-14.3-6.2-14.5-16.6h4.6c.1 7.6 3.5 10.8 6.1 11.5V16.600h4.300v6.600c2.600-.300 5.400-3.300 6.300-6.600h4.300c-.7 4.100-3.700 7.100-5.900 8.300 2.200 1 5.600 3.700 6.900 8.300h-4.700c-1-3.200-3.600-5.700-6.900-6v6h-.5z"
          />
        </>
      ) : null}
    </svg>
  );
}
