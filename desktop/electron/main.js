import { app, BrowserWindow, dialog } from "electron";
import { spawn } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import process from "node:process";
import { setTimeout as sleep } from "node:timers/promises";
import { fileURLToPath } from "node:url";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const repoRoot = path.resolve(__dirname, "..", "..");
const frontendRoot = path.join(repoRoot, "third_party", "deerflow-ui", "frontend");

const backendHost = process.env.DEERFLOW_ELECTRON_BACKEND_HOST || "127.0.0.1";
const backendPort = process.env.DEERFLOW_ELECTRON_BACKEND_PORT || "2026";
const frontendHost = process.env.DEERFLOW_ELECTRON_FRONTEND_HOST || "127.0.0.1";
const frontendPort = process.env.DEERFLOW_ELECTRON_FRONTEND_PORT || "3000";
const frontendMode = process.env.DEERFLOW_ELECTRON_FRONTEND_MODE || "dev";

const backendBaseURL = `http://${backendHost}:${backendPort}`;
const frontendURL = `http://${frontendHost}:${frontendPort}`;

const backendHealthURL = `${backendBaseURL}/health`;
const frontendHealthURL = frontendURL;

const waitTimeoutMs = parseInt(
  process.env.DEERFLOW_ELECTRON_WAIT_TIMEOUT_MS || "180000",
  10,
);
const waitIntervalMs = parseInt(
  process.env.DEERFLOW_ELECTRON_WAIT_INTERVAL_MS || "500",
  10,
);

let backendProc = null;
let frontendProc = null;
let mainWindow = null;
let shuttingDown = false;

function log(scope, message) {
  process.stdout.write(`[electron:${scope}] ${message}\n`);
}

function parseDotEnv(content) {
  const out = {};
  const lines = content.split(/\r?\n/);
  for (const line of lines) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith("#")) {
      continue;
    }
    const idx = trimmed.indexOf("=");
    if (idx <= 0) {
      continue;
    }
    const key = trimmed.slice(0, idx).trim();
    if (!key) {
      continue;
    }
    let value = trimmed.slice(idx + 1).trim();
    if (
      (value.startsWith('"') && value.endsWith('"')) ||
      (value.startsWith("'") && value.endsWith("'"))
    ) {
      value = value.slice(1, -1);
    }
    out[key] = value;
  }
  return out;
}

function loadRootEnv() {
  const envPath = path.join(repoRoot, ".env");
  if (!fs.existsSync(envPath)) {
    return;
  }
  const parsed = parseDotEnv(fs.readFileSync(envPath, "utf8"));
  for (const [key, value] of Object.entries(parsed)) {
    if (process.env[key] === undefined || process.env[key] === "") {
      process.env[key] = value;
    }
  }
}

function resolveBackendStartConfig() {
  const explicit = process.env.DEERFLOW_ELECTRON_BACKEND_BIN;
  if (explicit && explicit.trim()) {
    return {
      command: explicit.trim(),
      args: ["--addr", `${backendHost}:${backendPort}`],
      cwd: repoRoot,
    };
  }

  const exe = process.platform === "win32" ? "langgraph.exe" : "langgraph";
  const localBinary = path.join(repoRoot, "bin", exe);
  if (fs.existsSync(localBinary)) {
    return {
      command: localBinary,
      args: ["--addr", `${backendHost}:${backendPort}`],
      cwd: repoRoot,
    };
  }

  const goCmd = process.platform === "win32" ? "go.exe" : "go";
  return {
    command: goCmd,
    args: ["run", "./cmd/langgraph", "--addr", `${backendHost}:${backendPort}`],
    cwd: repoRoot,
  };
}

function resolveFrontendStartConfig() {
  const pnpmCmd = process.platform === "win32" ? "pnpm.cmd" : "pnpm";
  if (frontendMode === "start") {
    return {
      command: pnpmCmd,
      args: ["start", "--", "--hostname", frontendHost, "--port", frontendPort],
      cwd: frontendRoot,
    };
  }
  return {
    command: pnpmCmd,
    args: ["dev", "--", "--hostname", frontendHost, "--port", frontendPort],
    cwd: frontendRoot,
  };
}

function attachProcLogs(proc, scope) {
  if (!proc) {
    return;
  }
  if (proc.stdout) {
    proc.stdout.on("data", (chunk) => {
      process.stdout.write(`[${scope}] ${chunk}`);
    });
  }
  if (proc.stderr) {
    proc.stderr.on("data", (chunk) => {
      process.stderr.write(`[${scope}] ${chunk}`);
    });
  }
}

function startProcess(scope, config, extraEnv) {
  log(scope, `start: ${config.command} ${config.args.join(" ")}`);
  const proc = spawn(config.command, config.args, {
    cwd: config.cwd,
    env: { ...process.env, ...extraEnv },
    stdio: ["ignore", "pipe", "pipe"],
    windowsHide: true,
  });
  attachProcLogs(proc, scope);
  proc.on("error", (err) => {
    log(scope, `error: ${err.message}`);
    if (!shuttingDown) {
      void failAndQuit(`${scope} failed to start: ${err.message}`);
    }
  });
  proc.on("exit", (code, signal) => {
    log(scope, `exit: code=${code ?? "null"} signal=${signal ?? "null"}`);
    if (!shuttingDown) {
      void failAndQuit(`${scope} exited unexpectedly (code=${code}, signal=${signal})`);
    }
  });
  return proc;
}

async function waitForURL(url, timeoutMs, intervalMs, scope) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const controller = new AbortController();
      const timeout = setTimeout(() => controller.abort(), 2000);
      const resp = await fetch(url, { signal: controller.signal });
      clearTimeout(timeout);
      if (resp.status >= 200 && resp.status < 500) {
        log(scope, `ready: ${url} (${resp.status})`);
        return;
      }
    } catch {
      // keep polling
    }
    await sleep(intervalMs);
  }
  throw new Error(`timeout waiting for ${scope}: ${url}`);
}

async function stopProcess(proc, scope) {
  if (!proc || proc.killed || proc.exitCode !== null) {
    return;
  }
  log(scope, "stopping");
  try {
    proc.kill("SIGINT");
  } catch {
    proc.kill();
  }
  await sleep(1200);
  if (proc.exitCode === null) {
    proc.kill("SIGKILL");
  }
}

async function shutdownAll() {
  if (shuttingDown) {
    return;
  }
  shuttingDown = true;
  await stopProcess(frontendProc, "frontend");
  await stopProcess(backendProc, "backend");
}

async function failAndQuit(message) {
  log("fatal", message);
  try {
    await dialog.showMessageBox({
      type: "error",
      title: "DeerFlow Desktop Startup Failed",
      message,
      buttons: ["OK"],
    });
  } catch {
    // ignore dialog errors
  }
  await shutdownAll();
  app.quit();
}

function createWindow() {
  mainWindow = new BrowserWindow({
    width: 1440,
    height: 960,
    minWidth: 1080,
    minHeight: 720,
    autoHideMenuBar: true,
    webPreferences: {
      preload: path.join(__dirname, "preload.js"),
      nodeIntegration: false,
      contextIsolation: true,
      sandbox: true,
      webSecurity: true,
    },
  });
  mainWindow.on("closed", () => {
    mainWindow = null;
  });
  void mainWindow.loadURL(frontendURL);
}

async function bootstrap() {
  loadRootEnv();

  if (!fs.existsSync(frontendRoot)) {
    throw new Error(`frontend directory not found: ${frontendRoot}`);
  }

  const backendStart = resolveBackendStartConfig();
  const frontendStart = resolveFrontendStartConfig();

  const backendEnv = {
    ADDR: `${backendHost}:${backendPort}`,
    PORT: `${backendPort}`,
  };
  backendProc = startProcess("backend", backendStart, backendEnv);
  await waitForURL(backendHealthURL, waitTimeoutMs, waitIntervalMs, "backend");

  const frontendEnv = {
    SKIP_ENV_VALIDATION: "1",
    NEXT_PUBLIC_BACKEND_BASE_URL: backendBaseURL,
    NEXT_PUBLIC_LANGGRAPH_BASE_URL: `${backendBaseURL}/api/langgraph`,
    DEER_FLOW_INTERNAL_LANGGRAPH_BASE_URL: backendBaseURL,
    DEER_FLOW_INTERNAL_GATEWAY_BASE_URL: backendBaseURL,
    NEXT_TELEMETRY_DISABLED: "1",
  };
  frontendProc = startProcess("frontend", frontendStart, frontendEnv);
  await waitForURL(frontendHealthURL, waitTimeoutMs, waitIntervalMs, "frontend");
}

app.on("window-all-closed", async () => {
  await shutdownAll();
  if (process.platform !== "darwin") {
    app.quit();
  }
});

app.on("before-quit", async () => {
  await shutdownAll();
});

app.whenReady().then(async () => {
  try {
    await bootstrap();
    createWindow();
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    await failAndQuit(msg);
  }
});
