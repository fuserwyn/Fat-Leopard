import { Component, type ErrorInfo, type ReactNode } from "react";
import "./ErrorBoundary.css";

type Props = {
  /** Что сломалось — в подписи: «Лента», «Профиль». Без него — всё приложение. */
  label?: string;
  /** Язык подписей заглушки; админка всегда передаёт «ru». */
  lang?: "en" | "ru";
  children: ReactNode;
};

const COPY = {
  en: {
    labelTitle: (label: string) => `“${label}” didn't load`,
    title: "Something went wrong",
    labelText: "Other tabs still work. Try opening this one again.",
    text: "Try again. If it keeps happening, restart the mini app.",
    retry: "Try again",
    reload: "Reload",
  },
  ru: {
    labelTitle: (label: string) => `«${label}» не загрузилась`,
    title: "Что-то пошло не так",
    labelText: "Остальные вкладки работают. Попробуй открыть эту ещё раз.",
    text: "Попробуй ещё раз. Если повторяется — перезапусти мини-апп.",
    retry: "Попробовать снова",
    reload: "Перезагрузить",
  },
} as const;

type State = { error: Error | null };

/**
 * Ловит ошибку отрисовки в своей части дерева. Без него одно исключение
 * в любой вкладке размонтирует всё приложение — пользователь видит белый экран.
 */
export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("[ErrorBoundary]", this.props.label ?? "app", error, info.componentStack);
  }

  render() {
    if (!this.state.error) return this.props.children;
    const { label, lang = "en" } = this.props;
    const tx = COPY[lang];
    return (
      <div className="error-boundary" role="alert">
        <p className="error-boundary__title">
          {label ? tx.labelTitle(label) : tx.title}
        </p>
        <p className="error-boundary__text">
          {label ? tx.labelText : tx.text}
        </p>
        <div className="error-boundary__actions">
          <button type="button" className="error-boundary__btn" onClick={() => this.setState({ error: null })}>
            {tx.retry}
          </button>
          {!label ? (
            <button
              type="button"
              className="error-boundary__btn error-boundary__btn--ghost"
              onClick={() => window.location.reload()}
            >
              {tx.reload}
            </button>
          ) : null}
        </div>
      </div>
    );
  }
}
