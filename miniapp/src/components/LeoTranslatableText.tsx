import { useEffect, useState, type ReactNode } from "react";
import { LeoTranslateButton } from "./LeoTranslateButton";

type Props = {
  text: string;
  initData?: string;
  enabled?: boolean;
  className?: string;
  as?: "p" | "div" | "span";
  children?: (displayText: string) => ReactNode;
};

/** Текст Лео + кнопка «Translate to English», если есть кириллица. */
export function LeoTranslatableText({
  text,
  initData,
  enabled = true,
  className,
  as = "p",
  children,
}: Props) {
  const original = text;
  const [display, setDisplay] = useState(original);
  useEffect(() => {
    setDisplay(original);
  }, [original]);

  const Tag = as;
  const body = children ? children(display) : <Tag className={className}>{display}</Tag>;

  return (
    <>
      {body}
      {enabled ? (
        <LeoTranslateButton text={original} initData={initData} onShowText={setDisplay} />
      ) : null}
    </>
  );
}
