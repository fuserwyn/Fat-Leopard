/* Статика на $PORT (Railway / production). */
const { spawn } = require("child_process");
const path = require("path");

const port = String(process.env.PORT || 3000);
const serveMain = path.join(__dirname, "..", "node_modules", "serve", "build", "main.js");
const child = spawn(process.execPath, [serveMain, "-s", "dist", "-l", `tcp://0.0.0.0:${port}`], {
  stdio: "inherit",
  cwd: path.join(__dirname, ".."),
  env: process.env,
});
child.on("exit", (code) => process.exit(code ?? 1));
