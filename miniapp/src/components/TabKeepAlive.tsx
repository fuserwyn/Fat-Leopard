import { useLayoutEffect, useRef, type ReactNode } from "react";
import { ErrorBoundary } from "./ErrorBoundary";
import { applyScrollY, captureScrollY } from "../lib/tabScrollRestore";
import "./TabKeepAlive.css";

type Props = {
  active: boolean;
  /** Скрыть вкладку (оверлей тренировки / поддержки поверх). */
  hidden?: boolean;
  className?: string;
  /** Название раздела для сообщения об ошибке: сбой в одной вкладке не роняет остальные. */
  label?: string;
  children: ReactNode;
};

/** Держит дочерний экран смонтированным; показывает только при active. */
export function TabKeepAlive({ active, hidden = false, className, label, children }: Props) {
  const show = active && !hidden;
  const scrollYRef = useRef(0);
  const prevShowRef = useRef(show);

  // Читаем scrollY в render, пока DOM ещё старый: после display:none документ
  // схлопывается и браузер уже обрезал window.scrollY.
  if (prevShowRef.current && !show) {
    scrollYRef.current = captureScrollY();
  }

  useLayoutEffect(() => {
    const was = prevShowRef.current;
    prevShowRef.current = show;
    if (!was && show) {
      const y = scrollYRef.current;
      applyScrollY(y);
      // Chat/Support снимают body--lock позже в этом же commit (#root снова
      // с padding-bottom). Повторяем restore в том же кадре, иначе список прыгает.
      queueMicrotask(() => applyScrollY(y));
    }
  }, [show]);

  return (
    <div
      className={["tab-pane", show ? "tab-pane--active" : "", hidden ? "tab-pane--overlay-hidden" : "", className]
        .filter(Boolean)
        .join(" ")}
      aria-hidden={!show}
      {...(!show ? { inert: true as const } : {})}
    >
      <ErrorBoundary label={label}>{children}</ErrorBoundary>
    </div>
  );
}
