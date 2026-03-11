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

  const env = {
    ...process.env,
    GOCACHE: process.env.GOCACHE || path.join(rootDir, ".tmp-npm-go-cache"),
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
    const localBinary = process.env.SCICLI_LOCAL_BINARY;
    if (localBinary) {
      installFromLocalBinary(localBinary);
      return;
    }

    await installFromRelease();
  } catch (downloadError) {
    log(`Prebuilt install unavailable: ${downloadError.message}`);
    try {
      installFromSource();
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
