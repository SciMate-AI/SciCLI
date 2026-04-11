import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

const currentFile = fileURLToPath(import.meta.url);
const rootDir = path.resolve(path.dirname(currentFile), "..");
const packageJson = JSON.parse(
  fs.readFileSync(path.join(rootDir, "package.json"), "utf8"),
);

const binaryName = process.platform === "win32" ? "scicli-real.exe" : "scicli-real";
const extractedBinaryName = process.platform === "win32" ? "scicli.exe" : "scicli";
const binaryPath = path.join(rootDir, "bin", binaryName);
const launcherPath = path.join(rootDir, "bin", "scicli.js");
const tempDir = path.join(rootDir, ".tmp-npm-install");

function log(message) {
  process.stdout.write(`[scicli] ${message}\n`);
}

function fail(message) {
  process.stderr.write(`[scicli] ${message}\n`);
  process.exit(1);
}

function ensureDir(dirPath) {
  fs.mkdirSync(dirPath, { recursive: true });
}

function cleanTemp() {
  fs.rmSync(tempDir, { recursive: true, force: true });
}

function ensureLauncherScript() {
  if (fs.existsSync(launcherPath)) {
    if (process.platform !== "win32") {
      fs.chmodSync(launcherPath, 0o755);
    }
    return;
  }

  ensureDir(path.dirname(launcherPath));
  fs.writeFileSync(
    launcherPath,
    `#!/usr/bin/env node

const fs = require("node:fs");
const path = require("node:path");
const { spawn } = require("node:child_process");

const binaryName = process.platform === "win32" ? "scicli-real.exe" : "scicli-real";
const binaryPath = path.join(__dirname, binaryName);

if (!fs.existsSync(binaryPath)) {
  console.error(
    "SciCLI binary is missing. Reinstall the package or run \`npm rebuild @scimate/scicli\`.",
  );
  process.exit(1);
}

const child = spawn(binaryPath, process.argv.slice(2), {
  stdio: "inherit",
});

child.on("error", (error) => {
  console.error(\`Failed to start SciCLI: \${error.message}\`);
  process.exit(1);
});

child.on("exit", (code, signal) => {
  if (signal) {
    process.kill(process.pid, signal);
    return;
  }
  process.exit(code ?? 1);
});
`,
    "ascii",
  );

  if (process.platform !== "win32") {
    fs.chmodSync(launcherPath, 0o755);
  }
}

function uniquePaths(paths) {
  return [...new Set(paths.filter(Boolean).map((entry) => path.resolve(entry)))];
}

function resolveWindowsShimDirs() {
  if (process.platform !== "win32") {
    return [];
  }

  const scopeDir = path.dirname(rootDir);
  const nodeModulesDir = path.dirname(scopeDir);
  if (path.basename(nodeModulesDir).toLowerCase() !== "node_modules") {
    return [];
  }

  const prefixDir =
    process.env.npm_config_prefix ||
    (process.env.npm_config_global === "true" ? path.dirname(nodeModulesDir) : "");

  return uniquePaths([
    path.join(nodeModulesDir, ".bin"),
    process.env.npm_config_global === "true" ? prefixDir : "",
  ]);
}

function writeWindowsCmdShim(shimDir) {
  const relativeBinaryPath = path.relative(shimDir, binaryPath).replace(/\//g, "\\");
  const shimPath = path.join(shimDir, "scicli.cmd");
  const shimContents = `@ECHO OFF
"%~dp0${relativeBinaryPath}" %*
`;

  fs.writeFileSync(shimPath, shimContents, "ascii");
}

function writeWindowsPowerShellShim(shimDir) {
  const relativeBinaryPath = path.relative(shimDir, binaryPath).replace(/\//g, "\\");
  const normalizedRelativePath = relativeBinaryPath.replace(/\\/g, "\\\\");
  const shimPath = path.join(shimDir, "scicli.ps1");
  const shimContents = `$exe = Join-Path $PSScriptRoot "${normalizedRelativePath}"
& $exe @args
exit $LASTEXITCODE
`;

  fs.writeFileSync(shimPath, shimContents, "ascii");
}

function ensureWindowsShims() {
  if (process.platform !== "win32" || !fs.existsSync(binaryPath)) {
    return;
  }

  const shimDirs = resolveWindowsShimDirs();
  for (const shimDir of shimDirs) {
    ensureDir(shimDir);
    writeWindowsCmdShim(shimDir);
    writeWindowsPowerShellShim(shimDir);
  }
}

function platformSegment() {
  switch (process.platform) {
    case "darwin":
      return "mac";
    case "linux":
      return "linux";
    case "win32":
      return "windows";
    default:
      fail(`Unsupported platform: ${process.platform}`);
  }
}

function archSegment() {
  switch (process.arch) {
    case "x64":
      return "x86_64";
    case "arm64":
      return "arm64";
    default:
      fail(`Unsupported architecture: ${process.arch}`);
  }
}

function run(command, args, options = {}) {
  const result = spawnSync(command, args, {
    stdio: "pipe",
    encoding: "utf8",
    ...options,
  });
  if (result.error) {
    throw new Error(`${command} failed to start: ${result.error.message}`);
  }
  if (result.status !== 0) {
    const output = [result.stdout, result.stderr].filter(Boolean).join("\n").trim();
    throw new Error(output || `${command} exited with code ${result.status}`);
  }
  return result;
}

function resolveGoExecutable() {
  if (process.env.SCICLI_GO_EXECUTABLE) {
    return process.env.SCICLI_GO_EXECUTABLE;
  }

  if (process.platform === "win32") {
    const defaultWindowsGo = "C:\\Program Files\\Go\\bin\\go.exe";
    if (fs.existsSync(defaultWindowsGo)) {
      return defaultWindowsGo;
    }
  }

  return "go";
}

async function downloadFile(url, outputPath) {
  const response = await fetch(url);
  if (!response.ok) {
    throw new Error(`HTTP ${response.status} when downloading ${url}`);
  }
  const buffer = Buffer.from(await response.arrayBuffer());
  fs.writeFileSync(outputPath, buffer);
}

function extractArchive(archivePath, destinationDir) {
  ensureDir(destinationDir);
  if (archivePath.endsWith(".zip")) {
    if (process.platform === "win32") {
      run("powershell", [
        "-NoProfile",
        "-NonInteractive",
        "-Command",
        `Expand-Archive -Path '${archivePath}' -DestinationPath '${destinationDir}' -Force`,
      ]);
      return;
    }
    run("unzip", ["-o", archivePath, "-d", destinationDir]);
    return;
  }

  run("tar", ["-xzf", archivePath, "-C", destinationDir]);
}

function installFromLocalBinary(localBinary) {
  const source = path.resolve(localBinary);
  if (!fs.existsSync(source)) {
    throw new Error(`SCICLI_LOCAL_BINARY does not exist: ${source}`);
  }
  ensureDir(path.dirname(binaryPath));
  fs.copyFileSync(source, binaryPath);
  if (process.platform !== "win32") {
    fs.chmodSync(binaryPath, 0o755);
  }
  log(`Installed from local binary: ${source}`);
}

async function installFromRelease() {
  const releaseRepo =
    process.env.SCICLI_RELEASE_REPO || packageJson.scicli?.releaseRepo || "";
  if (!releaseRepo) {
    throw new Error("SCICLI_RELEASE_REPO is not configured");
  }

  const assetPrefix =
    process.env.SCICLI_RELEASE_ASSET_PREFIX ||
    packageJson.scicli?.releaseAssetPrefix ||
    "scicli";
  const tag = process.env.SCICLI_RELEASE_TAG || `v${packageJson.version}`;
  const extension = process.platform === "win32" ? "zip" : "tar.gz";
  const archiveName = `${assetPrefix}-${platformSegment()}-${archSegment()}.${extension}`;
  const downloadURL = `https://github.com/${releaseRepo}/releases/download/${tag}/${archiveName}`;
  const archivePath = path.join(tempDir, archiveName);
  const extractionDir = path.join(tempDir, "extract");
  const extractedBinaryPath = path.join(extractionDir, extractedBinaryName);

  cleanTemp();
  ensureDir(tempDir);
  log(`Downloading ${downloadURL}`);
  await downloadFile(downloadURL, archivePath);
  extractArchive(archivePath, extractionDir);

  if (!fs.existsSync(extractedBinaryPath)) {
    throw new Error(`Binary not found in archive: ${extractedBinaryName}`);
  }

  ensureDir(path.dirname(binaryPath));
  fs.copyFileSync(extractedBinaryPath, binaryPath);
  if (process.platform !== "win32") {
    fs.chmodSync(binaryPath, 0o755);
  }
  log(`Installed prebuilt binary for ${process.platform}/${process.arch}`);
}

function installFromSource() {
  ensureDir(path.dirname(binaryPath));

  const goCacheDir = process.env.GOCACHE || path.join(rootDir, ".tmp-npm-go-cache");
  const goModCacheDir =
    process.env.GOMODCACHE || path.join(rootDir, ".tmp-npm-go-modcache");

  ensureDir(goCacheDir);
  ensureDir(goModCacheDir);

  const env = {
    ...process.env,
    GOCACHE: goCacheDir,
    GOMODCACHE: goModCacheDir,
  };

  log("Falling back to `go build` from source");
  run(resolveGoExecutable(), ["build", "-o", binaryPath, "."], {
    cwd: rootDir,
    env,
  });
  if (process.platform !== "win32") {
    fs.chmodSync(binaryPath, 0o755);
  }
  log("Built SciCLI from source");
}

async function main() {
  if (process.env.SCICLI_SKIP_POSTINSTALL === "1") {
    log("Skipping postinstall because SCICLI_SKIP_POSTINSTALL=1");
    return;
  }

  try {
    ensureLauncherScript();

    const localBinary = process.env.SCICLI_LOCAL_BINARY;
    if (localBinary) {
      installFromLocalBinary(localBinary);
      ensureWindowsShims();
      return;
    }

    await installFromRelease();
    ensureWindowsShims();
  } catch (downloadError) {
    log(`Prebuilt install unavailable: ${downloadError.message}`);
    try {
      installFromSource();
      ensureWindowsShims();
    } catch (buildError) {
      fail(
        `Unable to install SciCLI. Download failed and source build failed.\n${buildError.message}`,
      );
    }
  } finally {
    cleanTemp();
  }
}

main().catch((error) => fail(error.message));
