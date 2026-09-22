import { describe, expect, it } from "vitest";
import {
  parseTrainingDoneCategory,
  parseTrainingDoneCategories,
  sortWorkoutCategoriesByCountDesc,
  stripLeadingCategoryFromTrainingReport,
  trainingDoneCategoryDisplayLabel,
  trainingDoneMatchesCategory,
  trainingDoneMatchesAnyCategory,
  WORKOUT_CATEGORY_OPTIONS,
} from "./workoutCategories";

describe("parseTrainingDoneCategories — мультивыбор", () => {
  it("разбирает один вид", () => {
    expect(parseTrainingDoneCategories("бег, 15 мин, инт. 3/5")).toEqual(["run"]);
  });

  it("разбирает несколько видов через «+»", () => {
    expect(parseTrainingDoneCategories("бег + плавание, 30 мин, инт. 3/5")).toEqual(["run", "swim"]);
  });

  it("схлопывает дубли и поддерживает «/» как разделитель", () => {
    expect(parseTrainingDoneCategories("плавание / бег / плавание, 20 мин, инт. 2/5")).toEqual([
      "swim",
      "run",
    ]);
  });

  it("неизвестный вид → other", () => {
    expect(parseTrainingDoneCategories("пилатес, 30 мин, инт. 2/5")).toEqual(["other"]);
  });

  it("разбирает теннис и падел как отдельные виды", () => {
    expect(parseTrainingDoneCategories("теннис, 60 мин, инт. 3/5")).toEqual(["tennis"]);
    expect(parseTrainingDoneCategories("падел, 45 мин, инт. 4/5")).toEqual(["padel"]);
    expect(parseTrainingDoneCategories("теннис + падел, 90 мин, инт. 3/5")).toEqual(["tennis", "padel"]);
  });

  it("разбирает футбол и волейбол как отдельные виды", () => {
    expect(parseTrainingDoneCategories("футбол, 60 мин, инт. 3/5")).toEqual(["football"]);
    expect(parseTrainingDoneCategories("волейбол, 45 мин, инт. 4/5")).toEqual(["volleyball"]);
    expect(parseTrainingDoneCategories("футбол + волейбол, 90 мин, инт. 3/5")).toEqual([
      "football",
      "volleyball",
    ]);
  });

  it("разбирает гимнастику как отдельный вид", () => {
    expect(parseTrainingDoneCategories("гимнастика, 45 мин, инт. 3/5")).toEqual(["gymnastics"]);
    expect(parseTrainingDoneCategories("гимнастика + йога, 60 мин, инт. 2/5")).toEqual([
      "gymnastics",
      "yoga",
    ]);
  });

  it("разбирает зарядку как отдельный вид", () => {
    expect(parseTrainingDoneCategories("зарядка, 15 мин, инт. 2/5")).toEqual(["morning_exercise"]);
    expect(parseTrainingDoneCategories("зарядка + йога, 30 мин, инт. 2/5")).toEqual([
      "morning_exercise",
      "yoga",
    ]);
  });

  it("нераспознанный формат → пустой массив", () => {
    expect(parseTrainingDoneCategories("просто текст")).toEqual([]);
  });

  it("первый вид — для эмодзи/заголовка", () => {
    expect(parseTrainingDoneCategory("йога + плавание, 30 мин, инт. 3/5")).toBe("yoga");
  });
});

describe("фильтр ленты по нескольким видам", () => {
  const text = "бег + плавание, 30 мин, инт. 3/5";

  it("матчится по каждому из своих видов", () => {
    expect(trainingDoneMatchesCategory(text, "run")).toBe(true);
    expect(trainingDoneMatchesCategory(text, "swim")).toBe(true);
    expect(trainingDoneMatchesCategory(text, "yoga")).toBe(false);
  });

  it("мультифильтр: совпадение хотя бы по одному виду", () => {
    expect(trainingDoneMatchesAnyCategory(text, new Set(["swim"]))).toBe(true);
    expect(trainingDoneMatchesAnyCategory(text, new Set(["yoga", "bike"]))).toBe(false);
    expect(trainingDoneMatchesAnyCategory(text, new Set())).toBe(true);
  });
});

describe("sortWorkoutCategoriesByCountDesc", () => {
  it("сортирует по убыванию частоты, «Другое» — в конце при равных счётчиках", () => {
    const sorted = sortWorkoutCategoriesByCountDesc(WORKOUT_CATEGORY_OPTIONS, {
      run: 5,
      yoga: 2,
      swim: 1,
      other: 0,
    });
    expect(sorted.slice(0, 3).map((o) => o.id)).toEqual(["run", "yoga", "swim"]);
    expect(sorted[sorted.length - 1]?.id).toBe("other");
  });
});

describe("заголовок карточки", () => {
  it("показывает оба вида как ввёл пользователь", () => {
    expect(trainingDoneCategoryDisplayLabel("бег + плавание, 30 мин, инт. 3/5")).toBe("бег + плавание");
  });

  it("показывает теннис и падел как отдельные подписи", () => {
    expect(trainingDoneCategoryDisplayLabel("теннис, 60 мин, инт. 3/5")).toBe("Теннис");
    expect(trainingDoneCategoryDisplayLabel("падел, 45 мин, инт. 4/5")).toBe("Падел");
  });

  it("показывает футбол и волейбол как отдельные подписи", () => {
    expect(trainingDoneCategoryDisplayLabel("футбол, 60 мин, инт. 3/5")).toBe("Футбол");
    expect(trainingDoneCategoryDisplayLabel("волейбол, 45 мин, инт. 4/5")).toBe("Волейбол");
  });

  it("показывает гимнастику как отдельную подпись", () => {
    expect(trainingDoneCategoryDisplayLabel("гимнастика, 45 мин, инт. 3/5")).toBe("Гимнастика");
  });

  it("показывает зарядку как отдельную подпись", () => {
    expect(trainingDoneCategoryDisplayLabel("зарядка, 15 мин, инт. 2/5")).toBe("Зарядка");
  });
});

describe("expandTrainingIntensityLabel / stripLeadingCategoryFromTrainingReport", () => {
  it("раскрывает устаревшее «инт.» в теле карточки", () => {
    expect(stripLeadingCategoryFromTrainingReport("бег, 15 мин, инт. 3/5")).toBe("15 мин, интенсивность 3/5");
  });

  it("оставляет новый формат без изменений", () => {
    expect(stripLeadingCategoryFromTrainingReport("бег, 15 мин, интенсивность 3/5")).toBe(
      "15 мин, интенсивность 3/5",
    );
  });
});
