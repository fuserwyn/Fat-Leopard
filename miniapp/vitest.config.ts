import { defineConfig, mergeConfig } from "vitest/config";
import viteConfig from "./vite.config";

// Покрытие считается при каждом `npm test` — и локально, и при сборке образа
// на выкатке. Пороги только растут: новый код без тестов опускает покрытие,
// и сборка не проходит. Цель — 90%.
export default mergeConfig(
  viteConfig,
  defineConfig({
    test: {
      // В образе на выкатке задан VITE_MINIAPP_API_URL (ARG в Dockerfile.miniapp),
      // локально — нет. Без фиксации тесты шли по разным веткам кода, и покрытие
      // в образе выходило ниже порога. Тестам, которым нужен API, его подставляет
      // vi.stubEnv (src/lib/testApi.ts).
      env: { VITE_MINIAPP_API_URL: "" },
      coverage: {
        provider: "v8",
        include: ["src/**/*.{ts,tsx}"],
        exclude: ["src/**/*.test.{ts,tsx}", "src/main.tsx", "src/vite-env.d.ts", "src/lib/testApi.ts", "src/lib/backendContract.ts"],
        reporter: ["text-summary"],
        thresholds: { lines: 31, functions: 59, statements: 31 },
      },
    },
  }),
);
