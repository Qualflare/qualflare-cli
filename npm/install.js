#!/usr/bin/env node
'use strict';

// Postinstall: download the platform-native `qf` binary from the matching GitHub
// release, verify its sha256 against the release checksums.txt, and extract it into
// ./bin. Dependency-free (uses Node core + the system `tar`).

const fs = require('node:fs');
const path = require('node:path');
const os = require('node:os');
const https = require('node:https');
const crypto = require('node:crypto');
const { spawnSync } = require('node:child_process');

const REPO = 'Qualflare/qualflare-cli';
const pkg = require('./package.json');
const version = pkg.version;

if (process.env.QUALFLARE_CLI_SKIP_INSTALL === '1') {
  console.log('[qualflare] QUALFLARE_CLI_SKIP_INSTALL=1 — skipping binary download.');
  process.exit(0);
}

const PLATFORMS = { darwin: 'darwin', linux: 'linux', win32: 'windows' };
const ARCHES = { x64: 'amd64', arm64: 'arm64' };
const goOS = PLATFORMS[process.platform];
const goArch = ARCHES[process.arch];
if (!goOS || !goArch) {
  console.error(
    `[qualflare] Unsupported platform: ${process.platform}/${process.arch}. ` +
      `Install manually from https://github.com/${REPO}/releases`,
  );
  process.exit(1);
}

const isWin = goOS === 'windows';
const ext = isWin ? 'zip' : 'tar.gz';
const binName = isWin ? 'qf.exe' : 'qf';
// Must match the goreleaser archives name_template: qf_<Version>_<Os>_<Arch>.
const archive = `qf_${version}_${goOS}_${goArch}.${ext}`;
const base =
  process.env.QUALFLARE_CLI_BASE_URL ||
  `https://github.com/${REPO}/releases/download/v${version}`;

const binDir = path.join(__dirname, 'bin');
const binPath = path.join(binDir, binName);

// Idempotent — a re-run (or a repaired install) shouldn't re-download.
if (fs.existsSync(binPath)) process.exit(0);
fs.mkdirSync(binDir, { recursive: true });

// Resolve `tar` to a fixed absolute path rather than searching $PATH (a writable
// $PATH entry could shadow the real binary). bsdtar (macOS, Windows 10+) reads both
// tar.gz and zip; GNU tar (Linux) reads tar.gz — so one `tar -xf` covers every OS.
function resolveTar() {
  const candidates = isWin
    ? ['C:\\Windows\\System32\\tar.exe']
    : ['/usr/bin/tar', '/bin/tar'];
  const found = candidates.find((p) => fs.existsSync(p));
  if (!found) throw new Error(`no usable tar found (looked in: ${candidates.join(', ')})`);
  return found;
}

function get(url) {
  return new Promise((resolve, reject) => {
    https
      .get(url, { headers: { 'User-Agent': '@qualflare/cli' } }, (res) => {
        // Follow the release-asset redirect to the CDN.
        if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
          res.resume();
          resolve(get(res.headers.location));
          return;
        }
        if (res.statusCode !== 200) {
          res.resume();
          reject(new Error(`GET ${url} -> HTTP ${res.statusCode}`));
          return;
        }
        const chunks = [];
        res.on('data', (c) => chunks.push(c));
        res.on('end', () => resolve(Buffer.concat(chunks)));
        res.on('error', reject);
      })
      .on('error', reject);
  });
}

(async () => {
  try {
    const [data, sumsBuf] = await Promise.all([
      get(`${base}/${archive}`),
      get(`${base}/checksums.txt`),
    ]);

    // checksums.txt lines look like: "<sha256>  qf_<v>_<os>_<arch>.<ext>"
    const line = sumsBuf
      .toString('utf8')
      .split('\n')
      .find((l) => l.trim().endsWith(archive));
    const expected = line?.trim().split(/\s+/)[0];
    if (!expected) throw new Error(`no checksum entry for ${archive}`);
    const actual = crypto.createHash('sha256').update(data).digest('hex');
    if (expected !== actual) {
      throw new Error(`checksum mismatch for ${archive} (expected ${expected}, got ${actual})`);
    }

    // Extract just the binary into ./bin. `tar` preserves the archived mode (0755),
    // so the extracted qf is already executable — no chmod needed.
    const workDir = fs.mkdtempSync(path.join(os.tmpdir(), 'qualflare-cli-'));
    const tmp = path.join(workDir, archive);
    fs.writeFileSync(tmp, data);
    const r = spawnSync(resolveTar(), ['-xf', tmp, '-C', binDir, binName], { stdio: 'inherit' });
    fs.rmSync(workDir, { recursive: true, force: true });
    if (r.status !== 0) throw new Error(`extract failed (tar exit ${r.status})`);

    console.log(`[qualflare] installed qf v${version} (${goOS}/${goArch}).`);
  } catch (err) {
    console.error(`[qualflare] failed to install the qf binary: ${err.message}`);
    console.error(
      `[qualflare] install manually from https://github.com/${REPO}/releases/tag/v${version}`,
    );
    process.exit(1);
  }
})();
