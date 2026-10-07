import { Component, type ErrorInfo, type ReactNode } from "react";
import "./ErrorBoundary.css";

type Props = {
  /** Что сломалось — в подписи: «Лента», «Профиль». Без него — всё приложение. */
  label?: string;
  children: ReactNode;
};

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
    const { label } = this.props;
    return (
      <div className="error-boundary" role="alert">
        <p className="error-boundary__title">
          {label ? `Раздел «${label}» не открылся` : "Что-то пошло не так"}
        </p>
        <p className="error-boundary__text">
          {label
            ? "Остальные разделы работают. Попробуйте открыть этот ещё раз."
            : "Попробуйте ещё раз. Если не поможет — перезапустите мини-приложение."}
        </p>
        <div className="error-boundary__actions">
          <button type="button" className="error-boundary__btn" onClick={() => this.setState({ error: null })}>
            Попробовать снова
          </button>
          {!label ? (
            <button
              type="button"
              className="error-boundary__btn error-boundary__btn--ghost"
              onClick={() => window.location.reload()}
            >
              Перезагрузить
            </button>
          ) : null}
        </div>
      </div>
    );
  }
}
