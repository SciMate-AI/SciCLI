import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const currentFile = fileURLToPath(import.meta.url);
const rootDir = path.resolve(path.dirname(currentFile), "..");
const packageJsonPath = path.join(rootDir, "package.json");

const rawVersion = process.argv[2];
if (!rawVersion) {
  process.stderr.write("[scicli] Missing version argument\n");
  process.exit(1);
}

const version = rawVersion.startsWith("v") ? rawVersion.slice(1) : rawVersion;
if (!/^\d+\.\d+\.\d+([-.][0-9A-Za-z.-]+)?$/.test(version)) {
  process.stderr.write(`[scicli] Invalid version: ${rawVersion}\n`);
  process.exit(1);
}

const packageJson = JSON.parse(fs.readFileSync(packageJsonPath, "utf8"));
packageJson.version = version;
fs.writeFileSync(packageJsonPath, `${JSON.stringify(packageJson, null, 2)}\n`);
process.stdout.write(`[scicli] package.json version synced to ${version}\n`);
