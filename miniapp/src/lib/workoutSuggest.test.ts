import { describe, expect, it } from "vitest";
import { WORKOUT_CATEGORY_OPTIONS } from "./workoutCategories";
import { orderWorkoutTypes, parseSuggestedWorkoutTypes } from "./workoutSuggest";

describe("orderWorkoutTypes", () => {
  it("ставит suggested в начало, остальные сохраняют порядок", () => {
    const ordered = orderWorkoutTypes(WORKOUT_CATEGORY_OPTIONS, ["yoga", "swim", "run"]);
    expect(ordered.slice(0, 3).map((o) => o.id)).toEqual(["yoga", "swim", "run"]);
    const tail = ordered.slice(3).map((o) => o.id);
    const expectedTail = WORKOUT_CATEGORY_OPTIONS.filter((o) => !["yoga", "swim", "run"].includes(o.id)).map(
      (o) => o.id,
    );
    expect(tail).toEqual(expectedTail);
  });

  it("без подсказок возвращает исходный список", () => {
    expect(orderWorkoutTypes(WORKOUT_CATEGORY_OPTIONS, null)).toEqual([...WORKOUT_CATEGORY_OPTIONS]);
  });
});

describe("parseSuggestedWorkoutTypes", () => {
  it("фильтрует неизвестные id", () => {
    expect(parseSuggestedWorkoutTypes(["run", "unknown", "yoga"])).toEqual(["run", "yoga"]);
  });

  it("пустой или невалидный ввод → null", () => {
    expect(parseSuggestedWorkoutTypes([])).toBeNull();
    expect(parseSuggestedWorkoutTypes("run")).toBeNull();
  });
});
