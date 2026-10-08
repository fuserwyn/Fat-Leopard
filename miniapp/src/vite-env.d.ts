/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_MINIAPP_API_URL?: string;
}

interface TelegramUser {
  id: number;
  first_name: string;
  last_name?: string;
  username?: string;
  language_code?: string;
  is_premium?: boolean;
}

interface TelegramWebApp {
  initData: string;
  initDataUnsafe: { user?: TelegramUser; query_id?: string };
  HapticFeedback?: {
    impactOccurred: (style: "light" | "medium" | "heavy" | "rigid" | "soft") => void;
    notificationOccurred?: (type: "error" | "success" | "warning") => void;
    selectionChanged?: () => void;
  };
  colorScheme: "light" | "dark";
  themeParams: Record<string, string | undefined>;
  isExpanded: boolean;
  /** Платформа клиента: ios | android | android_x | macos | tdesktop | weba | webk | unigram | unknown. */
  platform?: string;
  /** Высота видимой области Mini App (в пикселях), см. WebApp.viewportHeight в Telegram. */
  viewportHeight?: number;
  /** Высота без оверлеев (стабильнее при появлении клавиатуры). */
  viewportStableHeight?: number;
  ready: () => void;
  expand: () => void;
  /** Полноэкранный режим (Bot API ≥ 8.0). На старых клиентах метод отсутствует. */
  requestFullscreen?: () => void;
  exitFullscreen?: () => void;
  isFullscreen?: boolean;
  /** Запрещает свайп-вниз для сворачивания. Доступно с Bot API 7.7. */
  disableVerticalSwipes?: () => void;
  enableVerticalSwipes?: () => void;
  /** Сравнивает версию Bot API; вернёт false если метод не поддержан. */
  isVersionAtLeast?: (version: string) => boolean;
  setHeaderColor: (color: string) => void;
  setBackgroundColor: (color: string) => void;
  onEvent: (event: string, handler: () => void) => void;
  offEvent: (event: string, handler: () => void) => void;
  showAlert: (message: string) => void;
  /** Счёт Telegram (в т.ч. Stars) прямо в мини-аппе. Bot API ≥ 6.1. */
  openInvoice?: (
    url: string,
    callback?: (status: "paid" | "cancelled" | "failed" | "pending") => void,
  ) => void;
  /** Внешняя ссылка в браузере — для страницы оплаты ЮKassa. */
  openLink?: (url: string, options?: { try_instant_view?: boolean }) => void;
  /** Ссылка t.me внутри Telegram — например окно «Поделиться» t.me/share/url. */
  openTelegramLink?: (url: string) => void;
  close: () => void;
  /** Bot API ≥ 6.9 — ключи переживают закрытие Mini App, в отличие от localStorage на iOS. */
  CloudStorage?: {
    setItem: (key: string, value: string, callback?: (error: unknown, stored?: boolean) => void) => void;
    getItem: (key: string, callback: (error: unknown, value?: string) => void) => void;
  };
}

interface Window {
  Telegram?: { WebApp: TelegramWebApp };
}
