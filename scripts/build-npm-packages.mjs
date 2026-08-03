#!/usr/bin/env node
// Assemble the npm publish set from GoReleaser's build output.
//
//   node scripts/build-npm-packages.mjs <version> [--dist dist]
//
// Produces, under dist/npm/:
//   @qualflare/cli-<os>-<arch>/  — one per platform: package.json + bin/qf[.exe]
//   @qualflare/cli/              — the main package, with optionalDependencies pinned
//                                  to <version> and the launcher from npm/bin/qf
//
// Binaries are located via GoReleaser's dist/artifacts.json so this stays in step with
// whatever .goreleaser.yml builds. Dependency-free — Node core only.

import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';

// node platform/arch (npm's `os`/`cpu` vocabulary) -> Go's GOOS/GOARCH.
const TARGETS = [
  { os: 'darwin', cpu: 'arm64', goos: 'darwin', goarch: 'arm64' },
  { os: 'darwin', cpu: 'x64', goos: 'darwin', goarch: 'amd64' },
  { os: 'linux', cpu: 'arm64', goos: 'linux', goarch: 'arm64' },
  { os: 'linux', cpu: 'x64', goos: 'linux', goarch: 'amd64' },
  { os: 'win32', cpu: 'arm64', goos: 'windows', goarch: 'arm64' },
  { os: 'win32', cpu: 'x64', goos: 'windows', goarch: 'amd64' },
];

const REPO_URL = 'git+https://github.com/Qualflare/qualflare-cli.git';

const args = process.argv.slice(2);
const version = args[0];
const distIdx = args.indexOf('--dist');
const dist = distIdx === -1 ? 'dist' : args[distIdx + 1];

if (!version || version.startsWith('-')) {
  console.error('usage: build-npm-packages.mjs <version> [--dist dist]');
  process.exit(1);
}
// Guard against a leading "v" silently producing a version npm sorts differently.
if (!/^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$/.test(version)) {
  console.error(`invalid semver: "${version}" (expected e.g. 0.1.12 or 0.1.12-rc.1)`);
  process.exit(1);
}

const root = path.resolve(import.meta.dirname, '..');
const outRoot = path.join(root, dist, 'npm');
const license = fs.readFileSync(path.join(root, 'LICENSE'), 'utf8');

// GoReleaser records every artifact it produced; find the raw binaries by goos/goarch
// rather than reconstructing archive filenames (which drift when name_template changes).
function loadBinaries() {
  const artifactsPath = path.join(root, dist, 'artifacts.json');
  if (!fs.existsSync(artifactsPath)) {
    throw new Error(
      `${artifactsPath} not found — run \`goreleaser build\` (or \`release\`) first, ` +
        'or pass --dist to point at its output directory.',
    );
  }
  const artifacts = JSON.parse(fs.readFileSync(artifactsPath, 'utf8'));
  const found = new Map();
  for (const a of artifacts) {
    if (a.type !== 'Binary') continue;
    found.set(`${a.goos}/${a.goarch}`, path.resolve(root, a.path));
  }
  return found;
}

const binaries = loadBinaries();

fs.rmSync(outRoot, { recursive: true, force: true });
fs.mkdirSync(outRoot, { recursive: true });

const missing = [];

for (const t of TARGETS) {
  const name = `@qualflare/cli-${t.os}-${t.cpu}`;
  const src = binaries.get(`${t.goos}/${t.goarch}`);
  if (!src || !fs.existsSync(src)) {
    missing.push(`${name} (needs GOOS=${t.goos} GOARCH=${t.goarch})`);
    continue;
  }

  const dir = path.join(outRoot, name);
  const binName = t.os === 'win32' ? 'qf.exe' : 'qf';
  fs.mkdirSync(path.join(dir, 'bin'), { recursive: true });
  fs.copyFileSync(src, path.join(dir, 'bin', binName));
  // npm preserves the executable bit from the mode on disk.
  fs.chmodSync(path.join(dir, 'bin', binName), 0o755);

  writeJson(path.join(dir, 'package.json'), {
    name,
    version,
    description: `The ${t.os}/${t.cpu} binary for @qualflare/cli.`,
    homepage: 'https://qualflare.com',
    repository: { type: 'git', url: REPO_URL },
    license: 'Apache-2.0',
    author: 'Qualflare',
    engines: { node: '>=16' },
    os: [t.os],
    cpu: [t.cpu],
    files: [`bin/${binName}`],
    // Yarn Berry: unpack rather than leaving the binary inside a zip.
    preferUnplugged: true,
    publishConfig: { access: 'public', provenance: true },
  });

  fs.writeFileSync(path.join(dir, 'LICENSE'), license);
  fs.writeFileSync(
    path.join(dir, 'README.md'),
    `# ${name}\n\nThe ${t.os}/${t.cpu} binary for [\`@qualflare/cli\`](https://www.npmjs.com/package/@qualflare/cli).\n\n` +
      'This package is installed automatically as an optional dependency of\n' +
      '`@qualflare/cli`. You should not need to depend on it directly.\n',
  );

  console.log(`built ${name}`);
}

if (missing.length > 0) {
  console.error(`\nmissing binaries for:\n  ${missing.join('\n  ')}`);
  process.exit(1);
}

// --- main package -----------------------------------------------------------------

const mainDir = path.join(outRoot, '@qualflare/cli');
fs.mkdirSync(path.join(mainDir, 'bin'), { recursive: true });

const pkg = JSON.parse(fs.readFileSync(path.join(root, 'npm', 'package.json'), 'utf8'));
pkg.version = version;
// Exact pins, never a range — a range would let npm resolve a mismatched binary.
pkg.optionalDependencies = Object.fromEntries(
  TARGETS.map((t) => [`@qualflare/cli-${t.os}-${t.cpu}`, version]),
);
writeJson(path.join(mainDir, 'package.json'), pkg);

fs.copyFileSync(path.join(root, 'npm', 'bin', 'qf'), path.join(mainDir, 'bin', 'qf'));
fs.chmodSync(path.join(mainDir, 'bin', 'qf'), 0o755);
fs.copyFileSync(path.join(root, 'npm', 'README.md'), path.join(mainDir, 'README.md'));
fs.writeFileSync(path.join(mainDir, 'LICENSE'), license);

console.log(`built @qualflare/cli`);
console.log(`\n${TARGETS.length + 1} packages in ${path.relative(root, outRoot)} @ ${version}`);

function writeJson(file, value) {
  fs.writeFileSync(file, `${JSON.stringify(value, null, 2)}\n`);
}
