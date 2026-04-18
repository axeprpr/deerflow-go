import { app, BrowserWindow, dialog } from "electron";
import { spawn } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import process from "node:process";
import { setTimeout as sleep } from "node:timers/promises";
import { fileURLToPath } from "node:url";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const isPackaged = app.isPackaged;
const repoRoot = isPackaged
  ? path.resolve(process.resourcesPath, "..")
  : path.resolve(__dirname, "..", "..");
const devFrontendRoot = path.join(repoRoot, "third_party", "deerflow-ui", "frontend");

const defaultWaitTimeoutMs = 180000;
const defaultWaitIntervalMs = 500;

let backendProc = null;
let frontendProc = null;
let mainWindow = null;
let shuttingDown = false;
let startupConfig = null;

function log(scope, message) {
  process.stdout.write(`[electron:${scope}] ${message}\n`);
}

function env(name, fallback = "") {
  const value = process.env[name];
  if (value === undefined || value === null) {
    return fallback;
  }
  const trimmed = String(value).trim();
  return trimmed === "" ? fallback : trimmed;
}

function envBool(name, fallback = false) {
  const raw = env(name, fallback ? "1" : "0").toLowerCase();
  return raw === "1" || raw === "true" || raw === "yes" || raw === "on";
}

function parsePort(value, fallback) {
  const parsed = Number.parseInt(String(value), 10);
  if (Number.isFinite(parsed) && parsed > 0 && parsed <= 65535) {
    return String(parsed);
  }
  return String(fallback);
}

function executableName(base) {
  return process.platform === "win32" ? `${base}.exe` : base;
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

async function runCommandExit(scope, command, args, cwd, extraEnv = {}) {
  return new Promise((resolve, reject) => {
    log(scope, `exec: ${command} ${args.join(" ")}`);
    const proc = spawn(command, args, {
      cwd,
      env: { ...process.env, ...extraEnv },
      stdio: ["ignore", "pipe", "pipe"],
      windowsHide: true,
    });
    let stderr = "";
    if (proc.stdout) {
      proc.stdout.on("data", (chunk) => {
        process.stdout.write(`[${scope}] ${chunk}`);
      });
    }
    if (proc.stderr) {
      proc.stderr.on("data", (chunk) => {
        const text = String(chunk);
        stderr += text;
        process.stderr.write(`[${scope}] ${text}`);
      });
    }
    proc.on("error", (err) => {
      reject(new Error(`${scope} failed: ${err.message}`));
    });
    proc.on("exit", (code) => {
      if (code === 0) {
        resolve();
        return;
      }
      reject(
        new Error(
          `${scope} exited with code ${code}. ${stderr.trim() || "no stderr output"}`,
        ),
      );
    });
  });
}

async function waitForURL(url, timeoutMs, intervalMs, scope, acceptStatus) {
  const deadline = Date.now() + timeoutMs;
  const statusOK = acceptStatus || ((status) => status >= 200 && status < 500);
  while (Date.now() < deadline) {
    try {
      const controller = new AbortController();
      const timeout = setTimeout(() => controller.abort(), 2000);
      const resp = await fetch(url, { signal: controller.signal });
      clearTimeout(timeout);
      if (statusOK(resp.status)) {
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
  void mainWindow.loadURL(startupConfig.frontendURL);
}

function runtimeBinarySearchDirs() {
  const out = [];
  const push = (value) => {
    const trimmed = String(value || "").trim();
    if (!trimmed) {
      return;
    }
    const normalized = path.resolve(trimmed);
    if (!out.includes(normalized)) {
      out.push(normalized);
    }
  };
  push(env("DEERFLOW_ELECTRON_RUNTIME_BINARY_DIR", ""));
  push(path.join(process.resourcesPath, "runtime", "bin"));
  push(path.join(repoRoot, "desktop", "electron", "runtime", "bin"));
  push(path.join(repoRoot, "bin"));
  return out;
}

function resolveBinaryFromDirs(name, dirs) {
  const withExt = executableName(name);
  for (const dir of dirs) {
    const direct = path.join(dir, withExt);
    if (fs.existsSync(direct)) {
      return direct;
    }
    const plain = path.join(dir, name);
    if (fs.existsSync(plain)) {
      return plain;
    }
  }
  return null;
}

function readStartupConfig() {
  const backendHost = env("DEERFLOW_ELECTRON_BACKEND_HOST", "127.0.0.1");
  const backendPort = parsePort(env("DEERFLOW_ELECTRON_BACKEND_PORT", "2026"), 2026);
  const frontendHost = env("DEERFLOW_ELECTRON_FRONTEND_HOST", "127.0.0.1");
  const frontendPort = parsePort(env("DEERFLOW_ELECTRON_FRONTEND_PORT", "3000"), 3000);

  const runtimeModeRaw = env("DEERFLOW_ELECTRON_RUNTIME_MODE", "single").toLowerCase();
  const runtimeMode = runtimeModeRaw === "bundle" ? "bundle" : "single";

  const frontendModeDefault = isPackaged ? "external" : "dev";
  const frontendModeRaw = env("DEERFLOW_ELECTRON_FRONTEND_MODE", frontendModeDefault).toLowerCase();
  const frontendMode =
    frontendModeRaw === "start" || frontendModeRaw === "external" ? frontendModeRaw : "dev";

  const backendBaseURL = `http://${backendHost}:${backendPort}`;
  const frontendURL =
    frontendMode === "external"
      ? env("DEERFLOW_ELECTRON_FRONTEND_EXTERNAL_URL", backendBaseURL)
      : `http://${frontendHost}:${frontendPort}`;

  const workerPort = parsePort(
    env("DEERFLOW_ELECTRON_WORKER_PORT", String(Number.parseInt(backendPort, 10) + 55)),
    Number.parseInt(backendPort, 10) + 55,
  );

  const runtimeDataRoot = path.resolve(
    env("DEERFLOW_ELECTRON_RUNTIME_DATA_ROOT", path.join(app.getPath("userData"), "runtime-data")),
  );
  const runtimeBundleDir = path.resolve(
    env(
      "DEERFLOW_ELECTRON_RUNTIME_BUNDLE_DIR",
      path.join(app.getPath("userData"), "runtime-bundle"),
    ),
  );

  return {
    backendHost,
    backendPort,
    backendBaseURL,
    backendHealthURL: `${backendBaseURL}/health`,
    frontendHost,
    frontendPort,
    frontendMode,
    frontendURL,
    runtimeMode,
    runtimeBundleDir,
    runtimeBundlePreset: env("DEERFLOW_ELECTRON_RUNTIME_BUNDLE_PRESET", "shared-remote"),
    runtimeBundleRefresh: envBool("DEERFLOW_ELECTRON_RUNTIME_BUNDLE_REFRESH", false),
    runtimeDataRoot,
    workerAddr: `${backendHost}:${workerPort}`,
    waitTimeoutMs: Number.parseInt(
      env("DEERFLOW_ELECTRON_WAIT_TIMEOUT_MS", String(defaultWaitTimeoutMs)),
      10,
    ),
    waitIntervalMs: Number.parseInt(
      env("DEERFLOW_ELECTRON_WAIT_INTERVAL_MS", String(defaultWaitIntervalMs)),
      10,
    ),
  };
}

function resolveRuntimeStackCommand(config) {
  const dirs = runtimeBinarySearchDirs();
  const stackBin = resolveBinaryFromDirs("runtime-stack", dirs);
  if (stackBin) {
    return {
      command: stackBin,
      prefixArgs: [],
      cwd: repoRoot,
      binaryDir: path.dirname(stackBin),
    };
  }
  if (!isPackaged) {
    return {
      command: process.platform === "win32" ? "go.exe" : "go",
      prefixArgs: ["run", "./cmd/runtime-stack"],
      cwd: repoRoot,
      binaryDir: resolveBinaryDir(config),
    };
  }
  throw new Error(
    "runtime-stack binary is required for bundle mode. Set DEERFLOW_ELECTRON_RUNTIME_BINARY_DIR or include runtime/bin in packaged resources.",
  );
}

function resolveBinaryDir(config) {
  const explicit = env("DEERFLOW_ELECTRON_RUNTIME_BINARY_DIR", "");
  if (explicit) {
    return path.resolve(explicit);
  }
  const dirs = runtimeBinarySearchDirs();
  for (const dir of dirs) {
    const required = [
      "langgraph",
      "runtime-node",
      "runtime-state",
      "runtime-sandbox",
      "runtime-stack",
    ];
    const allPresent = required.every((name) => resolveBinaryFromDirs(name, [dir]));
    if (allPresent) {
      return dir;
    }
  }
  if (config.runtimeMode === "bundle" && isPackaged) {
    throw new Error(
      "bundle mode requires runtime binaries (langgraph/runtime-node/runtime-state/runtime-sandbox/runtime-stack).",
    );
  }
  return "";
}

async function ensureRuntimeBundle(config) {
  const stack = resolveRuntimeStackCommand(config);
  const ensureDir = path.dirname(config.runtimeBundleDir);
  fs.mkdirSync(ensureDir, { recursive: true });
  fs.mkdirSync(config.runtimeDataRoot, { recursive: true });

  const validateArgs = [
    ...stack.prefixArgs,
    `-validate-bundle=${config.runtimeBundleDir}`,
  ];
  const writeArgs = [
    ...stack.prefixArgs,
    `-write-bundle=${config.runtimeBundleDir}`,
    `-preset=${config.runtimeBundlePreset}`,
    "-addr",
    `${config.backendHost}:${config.backendPort}`,
    "-worker-addr",
    config.workerAddr,
    "-data-root",
    config.runtimeDataRoot,
  ];

  const needWrite = config.runtimeBundleRefresh || !fs.existsSync(config.runtimeBundleDir);
  if (needWrite) {
    await runCommandExit("runtime-bundle-write", stack.command, writeArgs, stack.cwd);
    await runCommandExit("runtime-bundle-validate", stack.command, validateArgs, stack.cwd);
    return stack;
  }

  try {
    await runCommandExit("runtime-bundle-validate", stack.command, validateArgs, stack.cwd);
  } catch {
    await runCommandExit("runtime-bundle-write", stack.command, writeArgs, stack.cwd);
    await runCommandExit("runtime-bundle-validate", stack.command, validateArgs, stack.cwd);
  }
  return stack;
}

async function resolveBackendStartConfig(config) {
  if (config.runtimeMode === "bundle") {
    const stack = await ensureRuntimeBundle(config);
    const args = [
      ...stack.prefixArgs,
      `-spawn-bundle=${config.runtimeBundleDir}`,
      "-spawn-bundle-use-host-plan",
    ];
    const binaryDir = resolveBinaryDir(config) || stack.binaryDir;
    if (binaryDir) {
      args.push(`-process-binary-dir=${binaryDir}`);
    }
    return {
      command: stack.command,
      args,
      cwd: stack.cwd,
    };
  }

  const explicit = env("DEERFLOW_ELECTRON_BACKEND_BIN", "");
  if (explicit) {
    return {
      command: explicit,
      args: ["--addr", `${config.backendHost}:${config.backendPort}`],
      cwd: repoRoot,
    };
  }

  const localBinary = resolveBinaryFromDirs("langgraph", runtimeBinarySearchDirs());
  if (localBinary) {
    return {
      command: localBinary,
      args: ["--addr", `${config.backendHost}:${config.backendPort}`],
      cwd: repoRoot,
    };
  }

  if (!isPackaged) {
    return {
      command: process.platform === "win32" ? "go.exe" : "go",
      args: ["run", "./cmd/langgraph", "--addr", `${config.backendHost}:${config.backendPort}`],
      cwd: repoRoot,
    };
  }

  throw new Error(
    "langgraph binary not found for packaged app. Set DEERFLOW_ELECTRON_BACKEND_BIN or bundle runtime/bin/langgraph.",
  );
}

function resolveFrontendStartConfig(config) {
  if (config.frontendMode === "external") {
    return null;
  }
  if (!fs.existsSync(devFrontendRoot)) {
    throw new Error(`frontend directory not found: ${devFrontendRoot}`);
  }
  const pnpmCmd = process.platform === "win32" ? "pnpm.cmd" : "pnpm";
  if (config.frontendMode === "start") {
    return {
      command: pnpmCmd,
      args: [
        "start",
        "--",
        "--hostname",
        config.frontendHost,
        "--port",
        config.frontendPort,
      ],
      cwd: devFrontendRoot,
    };
  }
  return {
    command: pnpmCmd,
    args: ["dev", "--", "--hostname", config.frontendHost, "--port", config.frontendPort],
    cwd: devFrontendRoot,
  };
}

async function bootstrap() {
  loadRootEnv();
  startupConfig = readStartupConfig();
  log(
    "config",
    `runtime_mode=${startupConfig.runtimeMode} frontend_mode=${startupConfig.frontendMode} backend=${startupConfig.backendBaseURL}`,
  );
  const backendStart = await resolveBackendStartConfig(startupConfig);
  const frontendStart = resolveFrontendStartConfig(startupConfig);

  const backendEnv = {
    ADDR: `${startupConfig.backendHost}:${startupConfig.backendPort}`,
    PORT: `${startupConfig.backendPort}`,
  };
  backendProc = startProcess("backend", backendStart, backendEnv);
  await waitForURL(
    startupConfig.backendHealthURL,
    startupConfig.waitTimeoutMs,
    startupConfig.waitIntervalMs,
    "backend",
  );

  if (frontendStart == null) {
    await waitForURL(
      startupConfig.frontendURL,
      startupConfig.waitTimeoutMs,
      startupConfig.waitIntervalMs,
      "frontend-external",
    );
    return;
  }

  const frontendEnv = {
    SKIP_ENV_VALIDATION: "1",
    NEXT_PUBLIC_BACKEND_BASE_URL: startupConfig.backendBaseURL,
    NEXT_PUBLIC_LANGGRAPH_BASE_URL: `${startupConfig.backendBaseURL}/api/langgraph`,
    DEER_FLOW_INTERNAL_LANGGRAPH_BASE_URL: startupConfig.backendBaseURL,
    DEER_FLOW_INTERNAL_GATEWAY_BASE_URL: startupConfig.backendBaseURL,
    NEXT_TELEMETRY_DISABLED: "1",
  };
  frontendProc = startProcess("frontend", frontendStart, frontendEnv);
  await waitForURL(
    startupConfig.frontendURL,
    startupConfig.waitTimeoutMs,
    startupConfig.waitIntervalMs,
    "frontend",
  );
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
