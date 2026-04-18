import { spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const electronRoot = path.resolve(__dirname, "..");
const repoRoot = path.resolve(electronRoot, "..", "..");
const outputDir = path.join(electronRoot, "runtime", "bin");
const isWin = process.platform === "win32";

const binaries = [
  { name: "langgraph", pkg: "./cmd/langgraph" },
  { name: "runtime-stack", pkg: "./cmd/runtime-stack" },
  { name: "runtime-node", pkg: "./cmd/runtime-node" },
  { name: "runtime-state", pkg: "./cmd/runtime-state" },
  { name: "runtime-sandbox", pkg: "./cmd/runtime-sandbox" },
];

function log(message) {
  process.stdout.write(`[build-runtime] ${message}\n`);
}

function run(cmd, args, cwd) {
  const result = spawnSync(cmd, args, {
    cwd,
    stdio: "inherit",
    env: process.env,
  });
  if (result.error) {
    throw result.error;
  }
  if (result.status !== 0) {
    throw new Error(`${cmd} ${args.join(" ")} exited with code ${result.status}`);
  }
}

function buildAll() {
  fs.mkdirSync(outputDir, { recursive: true });
  const go = isWin ? "go.exe" : "go";
  for (const item of binaries) {
    const filename = isWin ? `${item.name}.exe` : item.name;
    const target = path.join(outputDir, filename);
    log(`building ${item.name} -> ${target}`);
    run(go, ["build", "-o", target, item.pkg], repoRoot);
    if (!isWin) {
      fs.chmodSync(target, 0o755);
    }
  }
}

try {
  buildAll();
  log(`done: ${outputDir}`);
} catch (err) {
  const message = err instanceof Error ? err.message : String(err);
  process.stderr.write(`[build-runtime] failed: ${message}\n`);
  process.exit(1);
}
