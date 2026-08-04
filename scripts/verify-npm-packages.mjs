#!/usr/bin/env node
// Check the output of build-npm-packages.mjs before it can reach the registry.
//
//   node scripts/verify-npm-packages.mjs <version> [--dist dist]
//
// The load-bearing assertion is the first one: the published main package must contain
// no install script and no JS-extension file. That is what keeps the Socket supply-chain
// alerts (install scripts / network access / shell access) off the package.

import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import process from 'node:process';

const PLATFORMS = [
  'darwin-arm64',
  'darwin-x64',
  'linux-arm64',
  'linux-x64',
  'win32-arm64',
  'win32-x64',
];

const args = process.argv.slice(2);
const version = args[0];
const distIdx = args.indexOf('--dist');
const dist = distIdx === -1 ? 'dist' : args[distIdx + 1];

if (!version) {
  console.error('usage: verify-npm-packages.mjs <version> [--dist dist]');
  process.exit(1);
}

const root = path.resolve(import.meta.dirname, '..');
const outRoot = path.join(root, dist, 'npm');
const mainDir = path.join(outRoot, '@qualflare/cli');

// Resolve npm to an absolute path once, rather than letting execFileSync re-search
// $PATH at every call site. Node has no exec.LookPath equivalent, so walk PATH here and
// fail loudly if it isn't found — a build script must still use the developer's npm.
function resolveBin(name) {
  const exts = process.platform === 'win32' ? (process.env.PATHEXT || '.EXE;.CMD;.BAT').split(';') : [''];
  for (const dir of (process.env.PATH || '').split(path.delimiter)) {
    if (!dir) continue;
    for (const ext of exts) {
      const candidate = path.join(dir, name + ext);
      try {
        fs.accessSync(candidate, fs.constants.X_OK);
        return candidate;
      } catch {
        // keep looking
      }
    }
  }
  throw new Error(`could not find "${name}" on PATH`);
}

const NPM = resolveBin('npm');

let failures = 0;
const check = (ok, label, detail) => {
  console.log(`${ok ? 'ok  ' : 'FAIL'}  ${label}`);
  if (!ok) {
    if (detail) console.log(`        ${detail}`);
    failures++;
  }
};

const readJson = (f) => JSON.parse(fs.readFileSync(f, 'utf8'));

// Run the launcher expecting it to exit non-zero, and hand back what it printed.
const runFailing = (bin) => {
  try {
    execFileSync(bin, ['version'], { stdio: 'pipe' });
    return '';
  } catch (e) {
    return String(e.stderr);
  }
};

// --- main package -------------------------------------------------------------------

const main = readJson(path.join(mainDir, 'package.json'));

check(main.scripts === undefined, 'main package declares no scripts', `got ${JSON.stringify(main.scripts)}`);
check(main.version === version, `main package version is ${version}`, `got ${main.version}`);
check(main.os === undefined && main.cpu === undefined, 'main package has no os/cpu gate');
check(main.bin?.qf === 'bin/qf', 'main package bin is bin/qf (extensionless)', `got ${main.bin?.qf}`);

// `npm pack --dry-run --json` reports exactly what would be published.
const packed = JSON.parse(
  execFileSync(NPM, ['pack', mainDir, '--dry-run', '--json'], { encoding: 'utf8', cwd: root }),
);
const files = packed[0].files.map((f) => f.path);
const jsFiles = files.filter((f) => /\.(js|cjs|mjs)$/.test(f));

check(jsFiles.length === 0, 'main tarball ships no JS-extension file', `found ${jsFiles.join(', ')}`);
check(files.includes('LICENSE'), 'main tarball ships LICENSE');
check(files.includes('bin/qf'), 'main tarball ships bin/qf');

const optDeps = main.optionalDependencies ?? {};
check(
  Object.keys(optDeps).length === PLATFORMS.length,
  `main package lists ${PLATFORMS.length} optionalDependencies`,
  `got ${Object.keys(optDeps).length}`,
);
const badPins = Object.entries(optDeps).filter(([, v]) => v !== version);
check(badPins.length === 0, 'optionalDependencies are pinned to exact versions', JSON.stringify(badPins));

// --- platform packages --------------------------------------------------------------

for (const p of PLATFORMS) {
  const dir = path.join(outRoot, `@qualflare/cli-${p}`);
  const [pOs, pCpu] = [p.slice(0, p.lastIndexOf('-')), p.slice(p.lastIndexOf('-') + 1)];
  if (!fs.existsSync(dir)) {
    check(false, `@qualflare/cli-${p} exists`);
    continue;
  }
  const pkg = readJson(path.join(dir, 'package.json'));
  const binName = pOs === 'win32' ? 'qf.exe' : 'qf';
  const binPath = path.join(dir, 'bin', binName);
  const okBin = fs.existsSync(binPath) && fs.statSync(binPath).size > 0;

  check(
    pkg.os?.[0] === pOs && pkg.cpu?.[0] === pCpu && pkg.version === version && okBin && !pkg.scripts,
    `@qualflare/cli-${p}: os/cpu/version/binary, no scripts`,
    `os=${pkg.os} cpu=${pkg.cpu} version=${pkg.version} binary=${okBin}`,
  );
}

// --- end-to-end: does the shim actually find and run the binary? ---------------------

const nativeOs = process.platform;
const nativeCpu = process.arch;
const nativeDir = path.join(outRoot, `@qualflare/cli-${nativeOs}-${nativeCpu}`);

if (!fs.existsSync(nativeDir)) {
  console.log(`skip  end-to-end run (no package built for ${nativeOs}-${nativeCpu})`);
} else {
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'qf-npm-verify-'));
  try {
    fs.writeFileSync(path.join(tmp, 'package.json'), '{"name":"verify","private":true}\n');
    const npm = (a) => execFileSync(NPM, a, { cwd: tmp, encoding: 'utf8', stdio: 'pipe' });

    const tgz = (dir) => {
      const out = JSON.parse(npm(['pack', dir, '--json', '--pack-destination', tmp]));
      return path.join(tmp, out[0].filename);
    };
    // --omit=optional so npm doesn't try to resolve the (not yet published) platform
    // packages from the registry; both tarballs are installed explicitly instead.
    npm(['install', '--no-package-lock', '--omit=optional', tgz(nativeDir), tgz(mainDir)]);

    const qf = path.join(tmp, 'node_modules', '.bin', 'qf');
    const stdout = execFileSync(qf, ['version'], { encoding: 'utf8' });
    check(stdout.trim().length > 0, `shim runs the binary (\`qf version\` -> ${stdout.trim()})`);

    // The shim must forward a non-zero exit code, not swallow it.
    let code = 0;
    try {
      execFileSync(qf, ['--definitely-not-a-flag'], { stdio: 'pipe' });
    } catch (e) {
      code = e.status;
    }
    check(code !== 0, 'shim forwards a non-zero exit code', `got ${code}`);

    // A foreign platform package present instead of the host's one means these
    // node_modules came from another machine — that must be named explicitly, not
    // folded into the generic lockfile message.
    const scope = path.join(tmp, 'node_modules', '@qualflare');
    const foreign = nativeOs === 'linux' ? 'cli-darwin-arm64' : 'cli-linux-x64';
    fs.renameSync(path.join(scope, `cli-${nativeOs}-${nativeCpu}`), path.join(scope, foreign));
    const foreignPkg = path.join(scope, foreign, 'package.json');
    fs.writeFileSync(
      foreignPkg,
      JSON.stringify({ ...readJson(foreignPkg), name: `@qualflare/${foreign}` }, null, 2),
    );
    let stderr = runFailing(qf);
    check(
      stderr.includes('different platform') && stderr.includes(foreign),
      'foreign platform package produces the cross-platform message',
      stderr.split('\n')[0],
    );

    // With nothing installed at all, fall back to the lockfile/--omit=optional guidance.
    fs.rmSync(path.join(scope, foreign), { recursive: true, force: true });
    stderr = runFailing(qf);
    check(
      stderr.includes('could not be found') && stderr.includes('4828'),
      'missing platform package produces the npm#4828 guidance',
      stderr.split('\n')[0],
    );
  } finally {
    fs.rmSync(tmp, { recursive: true, force: true });
  }
}

console.log(failures === 0 ? '\nall checks passed' : `\n${failures} check(s) failed`);
process.exit(failures === 0 ? 0 : 1);
