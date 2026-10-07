import { defineConfig, mergeConfig } from "vitest/config";
import viteConfig from "./vite.config";

// Покрытие считается при каждом `npm test` — и локально, и при сборке образа
// на выкатке. Пороги только растут: новый код без тестов опускает покрытие,
// и сборка не проходит. Цель — 90%.
export default mergeConfig(
  viteConfig,
  defineConfig({
    test: {
      coverage: {
        provider: "v8",
        include: ["src/**/*.{ts,tsx}"],
        exclude: ["src/**/*.test.{ts,tsx}", "src/main.tsx", "src/vite-env.d.ts"],
        reporter: ["text-summary"],
        thresholds: { lines: 24, functions: 44, statements: 24 },
      },
    },
  }),
);
