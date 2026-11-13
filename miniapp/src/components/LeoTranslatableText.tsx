import type { ReactNode } from "react";

type Props = {
  text: string;
  initData?: string;
  enabled?: boolean;
  className?: string;
  as?: "p" | "div" | "span";
  children?: (displayText: string) => ReactNode;
};

/** Текст Лео в ленте/чате (перевод по кнопке убран — язык берётся из ui_lang). */
export function LeoTranslatableText({
  text,
  className,
  as = "p",
  children,
}: Props) {
  const Tag = as;
  if (children) return <>{children(text)}</>;
  return <Tag className={className}>{text}</Tag>;
}
