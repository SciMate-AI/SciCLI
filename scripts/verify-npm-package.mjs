import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

const currentFile = fileURLToPath(import.meta.url);
const rootDir = path.resolve(path.dirname(currentFile), "..");

function resolveNpmInvocation() {
  if (process.env.npm_execpath) {
    return {
      command: process.execPath,
      argsPrefix: [process.env.npm_execpath],
    };
  }

  return {
    command: process.platform === "win32" ? "npm.cmd" : "npm",
    argsPrefix: [],
  };
}

function run(command, args, options = {}) {
  const result = spawnSync(command, args, {
    cwd: rootDir,
    encoding: "utf8",
    stdio: "pipe",
    ...options,
  });

  if (result.error) {
    throw new Error(`${command} failed to start: ${result.error.message}`);
  }

  if (result.status !== 0) {
    const output = [result.stdout, result.stderr].filter(Boolean).join("\n").trim();
    throw new Error(output || `${command} exited with code ${result.status}`);
  }

  return result.stdout;
}

function fail(message) {
  process.stderr.write(`[scicli] ${message}\n`);
  process.exit(1);
}

const expectedEntries = [
  "package/bin/scicli.js",
  "package/npm/postinstall.mjs",
  "package/package.json",
];

let tarballName = "";

try {
  const npmInvocation = resolveNpmInvocation();
  const output = run(npmInvocation.command, [
    ...npmInvocation.argsPrefix,
    "pack",
    "--json",
    "--silent",
    "--ignore-scripts",
  ]);
  const parsed = JSON.parse(output);
  tarballName = parsed.at(-1)?.filename || "";
  if (!tarballName) {
    fail("npm pack did not return a tarball filename");
  }

  const tarballPath = path.join(rootDir, tarballName);
  if (!fs.existsSync(tarballPath)) {
    fail(`Packed tarball not found: ${tarballPath}`);
  }

  const entriesOutput = run("tar", ["-tf", tarballPath]);
  const entries = new Set(entriesOutput.split(/\r?\n/).filter(Boolean));
  const missingEntries = expectedEntries.filter((entry) => !entries.has(entry));

  if (missingEntries.length > 0) {
    fail(`Packed npm tarball is missing required files: ${missingEntries.join(", ")}`);
  }

  process.stdout.write(`[scicli] Verified npm tarball: ${tarballName}\n`);
} catch (error) {
  fail(error instanceof Error ? error.message : String(error));
} finally {
  if (tarballName) {
    fs.rmSync(path.join(rootDir, tarballName), { force: true });
  }
}
