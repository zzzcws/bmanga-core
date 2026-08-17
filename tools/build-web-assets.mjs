import {
  brotliCompressSync,
  brotliDecompressSync,
  constants,
  gunzipSync,
  gzipSync,
} from "node:zlib";
import { spawnSync } from "node:child_process";
import { existsSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, extname, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const webV2Dir = resolve(root, "web-v2");
const webV2BuildDir = resolve(root, "web", "v2");
const npm = process.platform === "win32" ? "npm.cmd" : "npm";
const allowedArgs = new Set(["--ci"]);

for (const arg of process.argv.slice(2)) {
  if (!allowedArgs.has(arg)) {
    throw new Error(`unknown argument: ${arg}`);
  }
}

function run(command, args) {
  const executable = process.platform === "win32" && command.endsWith(".cmd") ? "cmd.exe" : command;
  const finalArgs = executable === "cmd.exe" ? ["/d", "/s", "/c", command, ...args] : args;
  const result = spawnSync(executable, finalArgs, {
    cwd: root,
    encoding: "utf8",
    stdio: "pipe",
  });
  if (result.error) {
    throw result.error;
  }
  if (result.status !== 0) {
    const details = [result.stdout, result.stderr].filter(Boolean).join("\n");
    throw new Error(`${command} ${args.join(" ")} failed\n${details}`);
  }
  if (result.stdout.trim()) {
    console.log(result.stdout.trim());
  }
}

const COMPRESSIBLE_V2_EXTENSIONS = new Set([".css", ".html", ".js", ".json", ".svg", ".txt", ".xml"]);

function precompress(baseDir, relativePath) {
  const path = resolve(baseDir, relativePath);
  const bytes = readFileSync(path);
  const gzip = gzipSync(bytes, { level: 9 });
  const brotli = brotliCompressSync(bytes, {
    params: {
      [constants.BROTLI_PARAM_QUALITY]: 11,
      [constants.BROTLI_PARAM_SIZE_HINT]: bytes.length,
    },
  });
  if (!gunzipSync(gzip).equals(bytes) || !brotliDecompressSync(brotli).equals(bytes)) {
    throw new Error(`precompression self-check failed: ${relativePath}`);
  }
  writeFileSync(`${path}.gz`, gzip);
  writeFileSync(`${path}.br`, brotli);
  return { relative: relativePath.replaceAll("\\", "/"), raw: bytes.length, gzip: gzip.length, brotli: brotli.length };
}

function v2CompressibleAssets() {
  if (!existsSync(webV2BuildDir)) {
    return [];
  }
  const files = [];
  const visit = (directory) => {
    for (const entry of readdirSync(directory, { withFileTypes: true })) {
      const path = resolve(directory, entry.name);
      if (entry.isDirectory()) {
        visit(path);
      } else if (entry.isFile() && COMPRESSIBLE_V2_EXTENSIONS.has(extname(entry.name).toLowerCase())) {
        files.push(relative(webV2BuildDir, path));
      }
    }
  };
  visit(webV2BuildDir);
  return files.sort((left, right) => left.localeCompare(right));
}

if (!existsSync(resolve(webV2Dir, "package.json")) || !existsSync(resolve(webV2Dir, "package-lock.json"))) {
  throw new Error("web-v2 package.json and package-lock.json are required");
}

if (process.argv.includes("--ci") || !existsSync(resolve(webV2Dir, "node_modules", ".package-lock.json"))) {
  run(npm, ["--prefix", webV2Dir, "ci"]);
}
run(npm, ["--prefix", webV2Dir, "run", "build"]);

if (!existsSync(resolve(webV2BuildDir, "index.html"))) {
  throw new Error("V2 build did not produce web/v2/index.html");
}

const results = v2CompressibleAssets().map((asset) => precompress(webV2BuildDir, asset));
console.log(JSON.stringify(results, null, 2));
