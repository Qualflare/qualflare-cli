#!/usr/bin/env node
'use strict';

// Postinstall: download the platform-native `qf` binary from the matching GitHub
// release, verify its sha256 against the release checksums.txt, and extract it into
// ./bin. Dependency-free (uses Node core + the system `tar`).

const fs = require('fs');
const path = require('path');
const os = require('os');
const https = require('https');
const crypto = require('crypto');
const { spawnSync } = require('child_process');

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
    const expected = line && line.trim().split(/\s+/)[0];
    if (!expected) throw new Error(`no checksum entry for ${archive}`);
    const actual = crypto.createHash('sha256').update(data).digest('hex');
    if (expected !== actual) {
      throw new Error(`checksum mismatch for ${archive} (expected ${expected}, got ${actual})`);
    }

    // Extract just the binary. GNU tar handles tar.gz; bsdtar (macOS + Windows 10+)
    // handles both tar.gz and zip — so a single `tar -xf` covers every platform.
    const tmp = path.join(os.tmpdir(), archive);
    fs.writeFileSync(tmp, data);
    const r = spawnSync('tar', ['-xf', tmp, '-C', binDir, binName], { stdio: 'inherit' });
    fs.unlinkSync(tmp);
    if (r.status !== 0) throw new Error(`extract failed (tar exit ${r.status})`);

    if (!isWin) fs.chmodSync(binPath, 0o755);
    console.log(`[qualflare] installed qf v${version} (${goOS}/${goArch}).`);
  } catch (err) {
    console.error(`[qualflare] failed to install the qf binary: ${err.message}`);
    console.error(
      `[qualflare] install manually from https://github.com/${REPO}/releases/tag/v${version}`,
    );
    process.exit(1);
  }
})();
