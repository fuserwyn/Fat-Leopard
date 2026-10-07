/**
 * Минимальные объявления Node для тестов контракта с бэкендом (они читают
 * исходники ms_leo с диска). Полный @types/node не подключаем: он меняет типы
 * setTimeout и прочих глобальных функций во всём коде мини-аппа.
 */
declare module "node:fs" {
  export function existsSync(path: string): boolean;
  export function readdirSync(path: string): string[];
  export function readFileSync(path: string, encoding: "utf8"): string;
  export function statSync(path: string): { isDirectory(): boolean };
}

declare module "node:path" {
  export function join(...parts: string[]): string;
}

declare const __dirname: string;
