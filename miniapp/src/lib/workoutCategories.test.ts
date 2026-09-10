import { describe, expect, it } from "vitest";
import {
  parseTrainingDoneCategory,
  parseTrainingDoneCategories,
  trainingDoneCategoryDisplayLabel,
  trainingDoneMatchesCategory,
  trainingDoneMatchesAnyCategory,
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
});
