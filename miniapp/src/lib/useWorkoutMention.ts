import { useCallback, useEffect, useState, type RefObject } from "react";
import { activeMentionQuery, applyMention, searchMentionCandidates, type MentionCandidate } from "./workoutNote";

/**
 * Подсказка «@» в описании тренировки: пока у курсора набирается отметка,
 * запрашивает участников стаи; pick вставляет выбранного и возвращает курсор в поле.
 * Отмеченный участник получит уведомление о совместной тренировке.
 */
export function useWorkoutMention(
  taRef: RefObject<HTMLTextAreaElement | null>,
  note: string,
  setNote: (v: string) => void,
  initData: string | undefined,
  max: number,
) {
  const [mention, setMention] = useState<{ start: number; query: string } | null>(null);
  const [hits, setHits] = useState<MentionCandidate[]>([]);

  const track = useCallback((el: HTMLTextAreaElement) => {
    const m = activeMentionQuery(el.value, el.selectionStart ?? el.value.length);
    setMention((prev) => (prev?.start === m?.start && prev?.query === m?.query ? prev : m));
  }, []);

  const close = useCallback(() => setMention(null), []);

  useEffect(() => {
    if (!mention || !initData?.trim()) {
      setHits([]);
      return;
    }
    let alive = true;
    const t = window.setTimeout(() => {
      void searchMentionCandidates(initData, mention.query).then((found) => {
        if (alive) setHits(found);
      });
    }, 200);
    return () => {
      alive = false;
      window.clearTimeout(t);
    };
  }, [mention, initData]);

  const pick = useCallback(
    (c: MentionCandidate) => {
      const ta = taRef.current;
      if (!mention || !ta) return;
      const next = applyMention(note, mention.start, ta.selectionStart ?? note.length, c.mention, max);
      setNote(next.text);
      setMention(null);
      setHits([]);
      window.requestAnimationFrame(() => {
        ta.focus();
        ta.setSelectionRange(next.caret, next.caret);
      });
    },
    [taRef, mention, note, setNote, max],
  );

  return { hits: mention ? hits : [], track, close, pick };
}
